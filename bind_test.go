package httpclient

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// embeddedClient mirrors the pattern the README recommends. It exists to prove
// that a generic method is promoted through an embedded *Client, which ordinary
// methods have always been but generic ones are new in Go 1.27.
type embeddedClient struct {
	*Client
}

func TestBindJSONReturnsTheDecodedValue(t *testing.T) {
	srv := newBodyServer(t, "application/json", `{"firstname":"Makis"}`)
	client := New(BaseURL(srv.URL))

	got, err := client.BindJSON[testValue](defaultCtx, http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Firstname != "Makis" {
		t.Fatalf("expected the value to be decoded, got %#+v", got)
	}
}

func TestBindJSONIsPromotedThroughAnEmbeddedClient(t *testing.T) {
	srv := newBodyServer(t, "application/json", `{"firstname":"Makis"}`)
	client := embeddedClient{New(BaseURL(srv.URL))}

	got, err := client.BindJSON[testValue](defaultCtx, http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Firstname != "Makis" {
		t.Fatalf("expected the value to be decoded, got %#+v", got)
	}
}

func TestBindJSONReturnsAPIErrorOnFailureStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"nope"}`)
	}))
	t.Cleanup(srv.Close)

	client := New(BaseURL(srv.URL))

	got, err := client.BindJSON[testValue](defaultCtx, http.MethodGet, "/", nil)
	if err == nil {
		t.Fatal("expected an APIError")
	}
	if got != (testValue{}) {
		t.Fatalf("expected the zero value alongside the error, got %#+v", got)
	}

	apiErr, ok := GetError(err)
	if !ok {
		t.Fatalf("expected an APIError, got %T", err)
	}
	if apiErr.Response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", apiErr.Response.StatusCode)
	}
	if GetErrorCode(err) != http.StatusNotFound {
		t.Fatalf("GetErrorCode disagreed: %d", GetErrorCode(err))
	}
}

func TestBindPlainReadsEachSupportedType(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		srv := newBodyServer(t, "text/plain", "hello")
		got, err := New(BaseURL(srv.URL)).BindPlain[string](defaultCtx, http.MethodGet, "/", nil)
		if err != nil || got != "hello" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("bytes", func(t *testing.T) {
		srv := newBodyServer(t, "text/plain", "hello")
		got, err := New(BaseURL(srv.URL)).BindPlain[[]byte](defaultCtx, http.MethodGet, "/", nil)
		if err != nil || string(got) != "hello" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("int", func(t *testing.T) {
		// The trailing newline is what a shell-written endpoint sends.
		srv := newBodyServer(t, "text/plain", "42\n")
		got, err := New(BaseURL(srv.URL)).BindPlain[int](defaultCtx, http.MethodGet, "/", nil)
		if err != nil || got != 42 {
			t.Fatalf("got %d, %v", got, err)
		}
	})

	t.Run("float64", func(t *testing.T) {
		srv := newBodyServer(t, "text/plain", "3.5")
		got, err := New(BaseURL(srv.URL)).BindPlain[float64](defaultCtx, http.MethodGet, "/", nil)
		if err != nil || got != 3.5 {
			t.Fatalf("got %v, %v", got, err)
		}
	})
}

// TestBindPlainSupportsNamedTypes covers "type Token string", whose pointer does
// not match the fast type switch.
func TestBindPlainSupportsNamedTypes(t *testing.T) {
	type token string

	srv := newBodyServer(t, "text/plain", "abc123")
	got, err := New(BaseURL(srv.URL)).BindPlain[token](defaultCtx, http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != token("abc123") {
		t.Fatalf("got %q", got)
	}
}

func TestBindDispatchesOnContentType(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		srv := newBodyServer(t, "application/json; charset=utf-8", `{"firstname":"Makis"}`)
		resp, err := New(BaseURL(srv.URL)).Do(defaultCtx, http.MethodGet, "/", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer DrainResponseBody(resp)

		got, err := Bind[testValue](resp)
		if err != nil || got.Firstname != "Makis" {
			t.Fatalf("got %#+v, %v", got, err)
		}
	})

	// Guards the "plain/text" typo, which made every text/plain response fall
	// through to "unexpected mime type received".
	t.Run("plain text", func(t *testing.T) {
		srv := newBodyServer(t, "text/plain; charset=utf-8", "hello")
		resp, err := New(BaseURL(srv.URL)).Do(defaultCtx, http.MethodGet, "/", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer DrainResponseBody(resp)

		got, err := Bind[string](resp)
		if err != nil || got != "hello" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

func TestBindErrorDecodesTheErrorBody(t *testing.T) {
	type problem struct {
		Message string `json:"message"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"bad input"}`)
	}))
	t.Cleanup(srv.Close)

	_, err := New(BaseURL(srv.URL)).BindJSON[testValue](defaultCtx, http.MethodGet, "/", nil)
	if err == nil {
		t.Fatal("expected an error")
	}

	got, decodeErr := BindError[problem](err)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if got.Message != "bad input" {
		t.Fatalf("got %#+v", got)
	}
}

// TestBindErrorPassesThroughANonAPIError: the caller must be able to tell
// "this was never an API error" from "I could not decode the API error".
func TestBindErrorPassesThroughANonAPIError(t *testing.T) {
	sentinel := errors.New("dial tcp: refused")

	_, err := BindError[testValue](sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the original error back, got %v", err)
	}
}
