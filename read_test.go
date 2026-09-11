package httpclient

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReadPlainSupportedDestinations(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		srv := newBodyServer(t, "text/plain", "hello")
		var got string
		if err := New(BaseURL(srv.URL)).ReadPlain(defaultCtx, &got, http.MethodGet, "/", nil); err != nil || got != "hello" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("bytes", func(t *testing.T) {
		srv := newBodyServer(t, "text/plain", "hello")
		var got []byte
		if err := New(BaseURL(srv.URL)).ReadPlain(defaultCtx, &got, http.MethodGet, "/", nil); err != nil || string(got) != "hello" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("int", func(t *testing.T) {
		srv := newBodyServer(t, "text/plain", "7")
		var got int
		if err := New(BaseURL(srv.URL)).ReadPlain(defaultCtx, &got, http.MethodGet, "/", nil); err != nil || got != 7 {
			t.Fatalf("got %d, %v", got, err)
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		srv := newBodyServer(t, "text/plain", "hello")
		var got float32
		if err := New(BaseURL(srv.URL)).ReadPlain(defaultCtx, &got, http.MethodGet, "/", nil); err == nil {
			t.Fatal("expected an unsupported destination error")
		}
	})
}

func TestGetPlainUnquoteStripsTheQuotes(t *testing.T) {
	srv := newBodyServer(t, "text/plain", `"a-token"`)

	got, err := New(BaseURL(srv.URL)).GetPlainUnquote(defaultCtx, http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a-token" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteToCopiesTheBodyAndTheHeaders(t *testing.T) {
	srv := newBodyServer(t, "text/plain", "streamed body")

	rec := httptest.NewRecorder()
	n, err := New(BaseURL(srv.URL)).WriteTo(defaultCtx, rec, http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len("streamed body")) {
		t.Fatalf("expected %d bytes, got %d", len("streamed body"), n)
	}
	if rec.Body.String() != "streamed body" {
		t.Fatalf("got %q", rec.Body.String())
	}
	if ct := rec.Header().Get(contentTypeKey); ct != "text/plain" {
		t.Fatalf("the content type should be forwarded, got %q", ct)
	}
}

func TestWriteToAnyWriter(t *testing.T) {
	srv := newBodyServer(t, "text/plain", "plain sink")

	var sink bytes.Buffer
	if _, err := New(BaseURL(srv.URL)).WriteTo(defaultCtx, &sink, http.MethodGet, "/", nil); err != nil {
		t.Fatal(err)
	}
	if sink.String() != "plain sink" {
		t.Fatalf("got %q", sink.String())
	}
}

// TestDialTimeoutKeepsAnExistingTransport: the option used to replace the whole
// transport, throwing away anything configured before it.
func TestDialTimeoutKeepsAnExistingTransport(t *testing.T) {
	base := &http.Transport{MaxIdleConns: 42}

	client := New(Transport(base), DialTimeout(3*time.Second))

	transport, ok := client.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected an *http.Transport, got %T", client.HTTPClient.Transport)
	}
	if transport.MaxIdleConns != 42 {
		t.Fatal("DialTimeout discarded the existing transport settings")
	}
	if transport.DialContext == nil {
		t.Fatal("DialTimeout did not set the dialer")
	}
}

// TestDialTimeoutBuildsATransportWhenThereIsNone keeps the original behaviour
// for a plain client.
func TestDialTimeoutBuildsATransportWhenThereIsNone(t *testing.T) {
	client := New(DialTimeout(3 * time.Second))

	transport, ok := client.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected an *http.Transport, got %T", client.HTTPClient.Transport)
	}
	if transport.DialContext == nil {
		t.Fatal("DialTimeout did not set the dialer")
	}
}
