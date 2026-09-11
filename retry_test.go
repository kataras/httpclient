package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// flakyServer answers the first len(statuses) requests with the given statuses,
// then 200. It records the request count and every request body it saw.
type flakyServer struct {
	*httptest.Server
	statuses []int
	calls    atomic.Int32
	bodies   []string
}

func newFlakyServer(t *testing.T, statuses ...int) *flakyServer {
	t.Helper()
	fs := &flakyServer{statuses: statuses}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		fs.bodies = append(fs.bodies, string(b))
		n := int(fs.calls.Add(1))
		w.Header().Set("Content-Type", "application/json")
		if n <= len(fs.statuses) {
			w.WriteHeader(fs.statuses[n-1])
			_, _ = w.Write([]byte(`{"error":"try again"}`))
			return
		}
		_, _ = w.Write([]byte(`{"firstname":"Makis"}`))
	}))
	t.Cleanup(fs.Close)
	return fs
}

func fastRetry(maxAttempts int) RetryPolicy {
	return RetryPolicy{MaxAttempts: maxAttempts, InitialBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond}
}

func TestRetryRetriesRetryableStatusesUntilSuccess(t *testing.T) {
	srv := newFlakyServer(t, http.StatusServiceUnavailable, http.StatusServiceUnavailable)
	client := New(BaseURL(srv.URL), Retry(fastRetry(3)))

	var got testValue
	if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil); err != nil {
		t.Fatalf("expected success after two 503s, got %v", err)
	}
	if got.Firstname != "Makis" {
		t.Fatalf("expected the final successful body to be decoded, got %#+v", got)
	}
	if srv.calls.Load() != 3 {
		t.Fatalf("expected exactly 3 requests, got %d", srv.calls.Load())
	}
}

func TestRetryDoesNotRetryNonRetryableStatusByDefault(t *testing.T) {
	srv := newFlakyServer(t, http.StatusInternalServerError)
	client := New(BaseURL(srv.URL), Retry(fastRetry(3)))

	err := client.ReadJSON(defaultCtx, nil, http.MethodGet, "/", nil)
	if GetErrorCode(err) != http.StatusInternalServerError {
		t.Fatalf("expected the 500 to be returned as an APIError, got %v", err)
	}
	if srv.calls.Load() != 1 {
		t.Fatalf("500 must not be retried by default, got %d requests", srv.calls.Load())
	}
}

func TestRetryCustomStatusCodes(t *testing.T) {
	srv := newFlakyServer(t, http.StatusInternalServerError)
	p := fastRetry(2)
	p.StatusCodes = []int{http.StatusInternalServerError}
	client := New(BaseURL(srv.URL), Retry(p))

	if err := client.ReadJSON(defaultCtx, nil, http.MethodGet, "/", nil); err != nil {
		t.Fatalf("expected success on the retry, got %v", err)
	}
	if srv.calls.Load() != 2 {
		t.Fatalf("expected 2 requests, got %d", srv.calls.Load())
	}
}

func TestRetryExhaustedReturnsTheLastAPIError(t *testing.T) {
	srv := newFlakyServer(t, 503, 503, 503, 503)
	client := New(BaseURL(srv.URL), Retry(fastRetry(3)))

	err := client.ReadJSON(defaultCtx, nil, http.MethodGet, "/", nil)
	if GetErrorCode(err) != http.StatusServiceUnavailable {
		t.Fatalf("expected the last 503 as APIError, got %v", err)
	}
	if srv.calls.Load() != 3 {
		t.Fatalf("MaxAttempts=3 means 3 requests, got %d", srv.calls.Load())
	}
}

func TestRetryWaitHonoursRetryAfterSecondsAndCapsIt(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxRetryAfter: 30 * time.Minute}.normalized()

	resp := &http.Response{Header: http.Header{"Retry-After": []string{"7"}}}
	if got := p.wait(1, resp); got != 7*time.Second {
		t.Fatalf("Retry-After: 7 must wait 7s, got %v", got)
	}

	p.MaxRetryAfter = 2 * time.Second
	if got := p.wait(1, resp); got != 2*time.Second {
		t.Fatalf("Retry-After must be capped at MaxRetryAfter, got %v", got)
	}

	date := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
	p.MaxRetryAfter = time.Minute
	got := p.wait(1, &http.Response{Header: http.Header{"Retry-After": []string{date}}})
	if got < 3*time.Second || got > 5*time.Second {
		t.Fatalf("HTTP-date Retry-After must be honoured (about 5s), got %v", got)
	}

	// Without the header, exponential backoff applies and is capped.
	p = RetryPolicy{MaxAttempts: 5, InitialBackoff: 100 * time.Millisecond, MaxBackoff: 350 * time.Millisecond}.normalized()
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 350 * time.Millisecond, 350 * time.Millisecond}
	for i, w := range want {
		if got := p.wait(i+1, nil); got != w {
			t.Fatalf("attempt %d: expected backoff %v, got %v", i+1, w, got)
		}
	}
}

func TestRetryStopsWhenContextIsCancelledDuringBackoff(t *testing.T) {
	srv := newFlakyServer(t, 503, 503, 503)
	p := RetryPolicy{MaxAttempts: 3, InitialBackoff: 10 * time.Second, MaxBackoff: 10 * time.Second}
	client := New(BaseURL(srv.URL), Retry(p))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := client.Do(ctx, http.MethodGet, "/", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("cancel must interrupt the backoff promptly, took %v", time.Since(start))
	}
	if srv.calls.Load() != 1 {
		t.Fatalf("expected 1 request before the cancel, got %d", srv.calls.Load())
	}
}

func TestRetryReplaysBufferedBodies(t *testing.T) {
	srv := newFlakyServer(t, 503)
	client := New(BaseURL(srv.URL), Retry(fastRetry(2)))

	if err := client.ReadJSON(defaultCtx, nil, http.MethodPost, "/", testValue{Firstname: "Makis"}); err != nil {
		t.Fatalf("expected success on the retry, got %v", err)
	}
	if len(srv.bodies) != 2 || srv.bodies[0] != srv.bodies[1] || !strings.Contains(srv.bodies[1], "Makis") {
		t.Fatalf("expected the same JSON body on both attempts, got %q", srv.bodies)
	}
}

func TestRetryDoesNotReplayUnbufferedReaders(t *testing.T) {
	srv := newFlakyServer(t, 503)
	client := New(BaseURL(srv.URL), Retry(fastRetry(3)))

	// io.Pipe readers cannot be rewound; no GetBody is available.
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte(`{"firstname":"x"}`))
		_ = pw.Close()
	}()

	err := client.ReadJSON(defaultCtx, nil, http.MethodPost, "/", pr)
	if GetErrorCode(err) != http.StatusServiceUnavailable {
		t.Fatalf("expected the 503 to be returned since the body cannot be replayed, got %v", err)
	}
	if srv.calls.Load() != 1 {
		t.Fatalf("an unbuffered body must not be retried, got %d requests", srv.calls.Load())
	}
}

type countingHandler struct{ begin, end atomic.Int32 }

func (h *countingHandler) BeginRequest(context.Context, *http.Request) error {
	h.begin.Add(1)
	return nil
}

func (h *countingHandler) EndRequest(_ context.Context, _ *http.Response, err error) error {
	h.end.Add(1)
	return err
}

func TestRetryInvokesRequestHandlersPerAttempt(t *testing.T) {
	srv := newFlakyServer(t, 503, 503)
	var retries []int
	p := fastRetry(3)
	p.OnRetry = func(attempt int, _ *http.Request, _ *http.Response, _ error, _ time.Duration) {
		retries = append(retries, attempt)
	}
	client := New(BaseURL(srv.URL), Retry(p))
	h := new(countingHandler)
	client.RegisterRequestHandler(h)

	if err := client.ReadJSON(defaultCtx, nil, http.MethodGet, "/", nil); err != nil {
		t.Fatal(err)
	}
	if h.begin.Load() != 3 || h.end.Load() != 3 {
		t.Fatalf("handlers must observe every attempt, got begin=%d end=%d", h.begin.Load(), h.end.Load())
	}
	if len(retries) != 2 || retries[0] != 1 || retries[1] != 2 {
		t.Fatalf("OnRetry must fire after attempts 1 and 2, got %v", retries)
	}
}

// failNTransport fails the first n round trips with a transport error.
type failNTransport struct {
	n     int32
	calls atomic.Int32
	next  http.RoundTripper
}

func (t *failNTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.calls.Add(1) <= t.n {
		return nil, errors.New("connection reset")
	}
	return t.next.RoundTrip(req)
}

func TestRetryTransportErrorsOnlyForIdempotentMethods(t *testing.T) {
	srv := newFlakyServer(t)

	t.Run("GET is retried", func(t *testing.T) {
		client := New(BaseURL(srv.URL), Retry(fastRetry(3)))
		tr := &failNTransport{n: 1, next: http.DefaultTransport}
		client.HTTPClient.Transport = tr
		if err := client.ReadJSON(defaultCtx, nil, http.MethodGet, "/", nil); err != nil {
			t.Fatalf("expected success after a transport error, got %v", err)
		}
		if tr.calls.Load() != 2 {
			t.Fatalf("expected 2 round trips, got %d", tr.calls.Load())
		}
	})

	t.Run("POST is not retried by default", func(t *testing.T) {
		client := New(BaseURL(srv.URL), Retry(fastRetry(3)))
		tr := &failNTransport{n: 1, next: http.DefaultTransport}
		client.HTTPClient.Transport = tr
		err := client.ReadJSON(defaultCtx, nil, http.MethodPost, "/", bytes.NewBufferString("{}"))
		if err == nil || !strings.Contains(err.Error(), "connection reset") {
			t.Fatalf("expected the transport error, got %v", err)
		}
		if tr.calls.Load() != 1 {
			t.Fatalf("expected 1 round trip, got %d", tr.calls.Load())
		}
	})

	t.Run("POST is retried with RetryNonIdempotent", func(t *testing.T) {
		p := fastRetry(3)
		p.RetryNonIdempotent = true
		client := New(BaseURL(srv.URL), Retry(p))
		tr := &failNTransport{n: 1, next: http.DefaultTransport}
		client.HTTPClient.Transport = tr
		if err := client.ReadJSON(defaultCtx, nil, http.MethodPost, "/", bytes.NewBufferString("{}")); err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if tr.calls.Load() != 2 {
			t.Fatalf("expected 2 round trips, got %d", tr.calls.Load())
		}
	})
}

func TestRetryDisabledByZeroPolicy(t *testing.T) {
	srv := newFlakyServer(t, 503)
	client := New(BaseURL(srv.URL), Retry(RetryPolicy{}))

	err := client.ReadJSON(defaultCtx, nil, http.MethodGet, "/", nil)
	if GetErrorCode(err) != http.StatusServiceUnavailable || srv.calls.Load() != 1 {
		t.Fatalf("a zero policy must not retry, got err=%v calls=%d", err, srv.calls.Load())
	}
}
