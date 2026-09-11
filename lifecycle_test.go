package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// trackingBody records whether the response body was closed.
type trackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *trackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

// stubTransport answers with a fixed response.
type stubTransport struct {
	body *trackingBody
	reqs atomic.Int32
}

func (t *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.reqs.Add(1)

	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        http.Header{},
		Body:          t.body,
		Request:       req,
		ContentLength: -1,
	}, nil
}

// errorHandler fails in EndRequest.
type errorHandler struct{ err error }

func (h errorHandler) BeginRequest(context.Context, *http.Request) error { return nil }
func (h errorHandler) EndRequest(context.Context, *http.Response, error) error {
	return h.err
}

// TestEndRequestErrorReleasesTheResponse: the response never reaches the caller,
// so the client has to close it or the connection is held until GC.
func TestEndRequestErrorReleasesTheResponse(t *testing.T) {
	body := &trackingBody{Reader: strings.NewReader(`{"firstname":"Makis"}`)}
	wantErr := errors.New("handler said no")

	client := New(BaseURL("http://example.local"), Transport(&stubTransport{body: body}))
	client.RegisterRequestHandler(errorHandler{err: wantErr})

	resp, err := client.Do(defaultCtx, http.MethodGet, "/", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the handler error, got %v", err)
	}
	if resp != nil {
		t.Fatal("no response should be handed back when a handler aborts")
	}
	if !body.closed.Load() {
		t.Fatal("the response body was leaked")
	}
}

// TestDrainResponseBodySurvivesAMissingBody: the nil check used to run after
// the read, so a hand-built response panicked.
func TestDrainResponseBodySurvivesAMissingBody(t *testing.T) {
	if err := DrainResponseBody(nil); err != nil {
		t.Fatalf("a nil response is a no-op, got %v", err)
	}

	if err := DrainResponseBody(&http.Response{}); err != nil {
		t.Fatalf("a nil body is a no-op, got %v", err)
	}
}

// passThroughHandler wraps the error it was handed and returns it, which is the
// natural way to add context from an observer.
type passThroughHandler struct{ seen atomic.Int32 }

func (h *passThroughHandler) BeginRequest(context.Context, *http.Request) error { return nil }
func (h *passThroughHandler) EndRequest(_ context.Context, _ *http.Response, err error) error {
	h.seen.Add(1)
	if err != nil {
		return fmt.Errorf("observed: %w", err)
	}
	return nil
}

// TestHandlerMayReturnTheWrappedTransportError: identity comparison treated a
// wrapped pass-through as a fresh failure and killed the call.
func TestHandlerMayReturnTheWrappedTransportError(t *testing.T) {
	wantErr := errors.New("dial failed")

	client := New(BaseURL("http://example.local"), Transport(errTransport{err: wantErr}))
	handler := new(passThroughHandler)
	client.RegisterRequestHandler(handler)

	_, err := client.Do(defaultCtx, http.MethodGet, "/", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the transport error to survive, got %v", err)
	}
	if handler.seen.Load() != 1 {
		t.Fatalf("expected the handler to run once, got %d", handler.seen.Load())
	}
}

// TestUnreplayableBodyReturnsTheRealResult: a failed rewind used to fall out of
// the loop into errors.New("client.Do: unreachable"), throwing away the status
// the server actually sent.
func TestUnreplayableBodyReturnsTheRealResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	client := New(BaseURL(srv.URL), Retry(fastRetry(3)))

	resp, err := client.Do(defaultCtx, http.MethodPut, "/", "payload",
		func(req *http.Request) error {
			// A GetBody that fails on the replay attempt.
			var calls atomic.Int32
			original := req.GetBody
			req.GetBody = func() (io.ReadCloser, error) {
				if calls.Add(1) > 0 {
					return nil, errors.New("cannot replay")
				}
				return original()
			}
			return nil
		})

	if err != nil {
		t.Fatalf("expected the real response, got error %v", err)
	}
	if resp == nil {
		t.Fatal("expected the previous attempt's response")
	}
	defer DrainResponseBody(resp)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected the 503 the server sent, got %d", resp.StatusCode)
	}
}

// TestEachAttemptGetsItsOwnRequest: net/http documents that a request must not
// be reused after Do returns.
func TestEachAttemptGetsItsOwnRequest(t *testing.T) {
	srv := newFlakyServer(t, http.StatusServiceUnavailable, http.StatusServiceUnavailable)

	var (
		mu   sync.Mutex
		seen []*http.Request
	)

	client := New(BaseURL(srv.URL), Retry(fastRetry(3)))
	client.RegisterRequestHandler(recordingHandler{onBegin: func(req *http.Request) {
		mu.Lock()
		seen = append(seen, req)
		mu.Unlock()
	}})

	var got testValue
	if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil); err != nil {
		t.Fatal(err)
	}

	if len(seen) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(seen))
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] == seen[i-1] {
			t.Fatalf("attempt %d reused the request value of attempt %d", i+1, i)
		}
	}
}

type recordingHandler struct {
	onBegin func(*http.Request)
}

func (h recordingHandler) BeginRequest(_ context.Context, req *http.Request) error {
	h.onBegin(req)
	return nil
}
func (h recordingHandler) EndRequest(context.Context, *http.Response, error) error { return nil }

// TestRegisterRequestHandlerDoesNotLeakIntoOtherClients guards the shared
// backing array behind the global handler list.
func TestRegisterRequestHandlerDoesNotLeakIntoOtherClients(t *testing.T) {
	first := New()
	first.RegisterRequestHandler(recordingHandler{onBegin: func(*http.Request) {}})

	second := New()
	if len(second.requestHandlers) != 0 {
		t.Fatalf("a per-client handler leaked into a new client: %d handlers", len(second.requestHandlers))
	}
}

// keepDefaultRequestHandlers puts the package-level handler list back the way it
// was when the test ends. The exported RegisterRequestHandler appends to a global
// that nothing resets, by design, so a test calling it leaves every later New()
// carrying its handlers. Without this the suite only passes on a single run:
// under -count=2 the tests asserting that a fresh Client has no handlers see the
// leftovers from the run before.
func keepDefaultRequestHandlers(t *testing.T) {
	t.Helper()

	mu.Lock()
	saved := slices.Clone(defaultRequestHandlers)
	mu.Unlock()

	t.Cleanup(func() {
		mu.Lock()
		defaultRequestHandlers = saved
		mu.Unlock()
	})
}

// TestNewIsSafeWhileHandlersAreRegistered is the race-detector case for the
// unsynchronised read of the global handler slice. It only fails under -race.
func TestNewIsSafeWhileHandlersAreRegistered(t *testing.T) {
	keepDefaultRequestHandlers(t)

	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 50 {
			RegisterRequestHandler(recordingHandler{onBegin: func(*http.Request) {}})
		}
	}()
	go func() {
		defer wg.Done()
		for range 50 {
			_ = New()
		}
	}()
	wg.Wait()
}
