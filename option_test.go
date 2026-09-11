package httpclient

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
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

// TestRateLimitPerMinuteDividesByTheMinute guards the per-minute limiter against
// being configured 3600 times faster than requested.
func TestRateLimitPerMinuteDividesByTheMinute(t *testing.T) {
	client := New(RateLimitPerMinute(60))

	if client.rateLimiter == nil {
		t.Fatal("expected a rate limiter to be configured")
	}

	if got := float64(client.rateLimiter.Limit()); got != 1 {
		t.Fatalf("60 requests per minute must be 1 request per second, got %v", got)
	}
}

type errTransport struct{ err error }

func (t errTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.err }

type bufferLogger struct{ buf bytes.Buffer }

func (l *bufferLogger) Debugf(format string, args ...any) {
	l.buf.WriteString(format)
	for _, a := range args {
		if s, ok := a.(string); ok {
			l.buf.WriteString(s)
		}
	}
	l.buf.WriteByte('\n')
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

	if !strings.Contains(logger.buf.String(), "dial failed") {
		t.Fatalf("expected the transport error to be logged, got %q", logger.buf.String())
	}
}
