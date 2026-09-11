package httpclient

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// TestHandlerOptionServesRequestsThroughTheGivenHandler proves the Handler option
// wires the handler into the transport instead of leaving a nil handler behind.
func TestHandlerOptionServesRequestsThroughTheGivenHandler(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/send", sendJSON(t, testValue{Firstname: "Makis"}))

	client := New(BaseURL("http://example.local"), Handler(mux))

	var got testValue
	if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/send", nil); err != nil {
		t.Fatalf("ReadJSON through Handler option: %v", err)
	}

	if got.Firstname != "Makis" {
		t.Fatalf("expected the handler's response to be decoded, got %#+v", got)
	}
}

type errTransport struct{ err error }

func (t errTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.err }

type bufferLogger struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Debugf formats for real. The previous double concatenated the format string
// with its string arguments, which is why nobody noticed that the request and
// response dumps were being passed as the format string.
func (l *bufferLogger) Debugf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	fmt.Fprintf(&l.buf, format, args...)
	l.buf.WriteByte('\n')
}

func (l *bufferLogger) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.String()
}

// TestDebugLoggerSurvivesTransportErrors: on a transport error there is no response,
// so the debug handler must not dereference it.
func TestDebugLoggerSurvivesTransportErrors(t *testing.T) {
	logger := new(bufferLogger)
	client := New(BaseURL("http://example.local"), Debug(logger))
	wantErr := errors.New("dial failed")
	client.HTTPClient.Transport = errTransport{err: wantErr}

	_, err := client.Do(defaultCtx, http.MethodGet, "/x", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the transport error to be returned, got %v", err)
	}

	if !strings.Contains(logger.String(), "dial failed") {
		t.Fatalf("expected the transport error to be logged, got %q", logger.String())
	}
}
