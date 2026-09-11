package httpclient

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestHandlerTransportBuildsAWellFormedResponse: the response was assembled by
// hand with Status set to the status text alone, which made APIError print
// "Not Found (Not Found)" and broke the Debug dump's status line.
func TestHandlerTransportBuildsAWellFormedResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"nope"}`)
	})

	client := New(BaseURL("http://example.local"), Handler(mux))

	resp, err := client.Do(defaultCtx, http.MethodGet, "/missing", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer DrainResponseBody(resp)

	if resp.Status != "404 Not Found" {
		t.Fatalf("Status must be the full status line, got %q", resp.Status)
	}
	if resp.Proto != "HTTP/1.1" || resp.ProtoMajor != 1 {
		t.Fatalf("expected the protocol to be filled in, got %q", resp.Proto)
	}
	if resp.Body == nil {
		t.Fatal("the response body must never be nil")
	}
	if resp.ContentLength <= 0 {
		t.Fatalf("expected a content length, got %d", resp.ContentLength)
	}
}

// TestHandlerTransportAPIErrorMessageHasNoDuplicatedStatus guards the message
// format, which printed the status text and the status line side by side.
func TestHandlerTransportAPIErrorMessageHasNoDuplicatedStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	client := New(BaseURL("http://example.local"), Handler(mux))

	var got testValue
	err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/missing", nil)
	if err == nil {
		t.Fatal("expected an APIError")
	}

	msg := err.Error()
	if strings.Count(msg, "Not Found") != 1 {
		t.Fatalf("the status should appear once, got %q", msg)
	}
	if !strings.Contains(msg, "404 Not Found") {
		t.Fatalf("expected the full status line, got %q", msg)
	}
}

// TestHandlerTransportDoesNotMutateTheCallersHeaders: the transport shallow
// copied the request, so the handler and the client shared one header map.
func TestHandlerTransportDoesNotMutateTheCallersHeaders(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Server-Added", "1")
		_, _ = io.WriteString(w, "{}")
	})

	var seen *http.Request
	client := New(BaseURL("http://example.local"), Handler(mux))
	client.RegisterRequestHandler(recordingHandler{onBegin: func(req *http.Request) {
		seen = req
	}})

	resp, err := client.Do(defaultCtx, http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer DrainResponseBody(resp)

	if seen.Header.Get("X-Server-Added") != "" {
		t.Fatal("the handler wrote into the client's own request headers")
	}
}
