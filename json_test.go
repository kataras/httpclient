package httpclient

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "encoding/json/v2"
)

func newBodyServer(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = io.WriteString(w, body)
	}))

	t.Cleanup(srv.Close)
	return srv
}

// TestEmptyResponseBodyStaysIoEOF pins the contract that made the move to
// encoding/json/v2 safe. Callers treat an empty body as a successful no-content
// answer and detect it with errors.Is(err, io.EOF); json/v2 reports a syntactic
// error instead, so the decoder has to keep returning io.EOF itself.
func TestEmptyResponseBodyStaysIoEOF(t *testing.T) {
	for _, body := range []string{"", "   ", "\n\t "} {
		srv := newBodyServer(t, "application/json", body)
		client := New(BaseURL(srv.URL))

		var got testValue
		err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil)

		if !errors.Is(err, io.EOF) {
			t.Fatalf("body %q: expected io.EOF, got %v", body, err)
		}
		if !IsErrEmptyJSON(err) {
			t.Fatalf("body %q: IsErrEmptyJSON must recognise it, got %v", body, err)
		}
	}
}

// TestBindJSONReportsEmptyBodyTheSameWay: the generic path must agree with ReadJSON.
func TestBindJSONReportsEmptyBodyTheSameWay(t *testing.T) {
	srv := newBodyServer(t, "application/json", "")
	client := New(BaseURL(srv.URL))

	_, err := client.BindJSON[testValue](defaultCtx, http.MethodGet, "/", nil)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

// TestDefaultJSONOptionsKeepV1Semantics: field matching stays case-insensitive
// and duplicate object names stay tolerated, which json/v2 rejects by default.
func TestDefaultJSONOptionsKeepV1Semantics(t *testing.T) {
	srv := newBodyServer(t, "application/json", `{"FirstName":"Makis"}`)
	client := New(BaseURL(srv.URL))

	var got testValue
	if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil); err != nil {
		t.Fatalf("case-insensitive matching must keep working: %v", err)
	}
	if got.Firstname != "Makis" {
		t.Fatalf("expected the value to be decoded, got %#+v", got)
	}

	dup := newBodyServer(t, "application/json", `{"firstname":"a","firstname":"Makis"}`)
	dupClient := New(BaseURL(dup.URL))

	var gotDup testValue
	if err := dupClient.ReadJSON(defaultCtx, &gotDup, http.MethodGet, "/", nil); err != nil {
		t.Fatalf("duplicate names must stay tolerated under v1 semantics: %v", err)
	}
}

// TestJSONOptionsCanTightenDecoding proves the option reaches the decoder.
func TestJSONOptionsCanTightenDecoding(t *testing.T) {
	srv := newBodyServer(t, "application/json", `{"firstname":"Makis","extra":1}`)
	client := New(BaseURL(srv.URL), JSONOptions(json.RejectUnknownMembers(true)))

	var got testValue
	err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil)
	if err == nil {
		t.Fatal("expected the unknown member to be rejected")
	}
	if IsErrEmptyJSON(err) {
		t.Fatalf("an unknown member is not an empty body: %v", err)
	}
}

// TestReadJSONRejectsANonPointerDestination guards the silent data loss that
// Decode(&dest) caused when dest was already an any holding a struct value.
func TestReadJSONRejectsANonPointerDestination(t *testing.T) {
	srv := newBodyServer(t, "application/json", `{"firstname":"Makis"}`)
	client := New(BaseURL(srv.URL))

	err := client.ReadJSON(defaultCtx, testValue{}, http.MethodGet, "/", nil)
	if err == nil {
		t.Fatal("expected an error for a non-pointer destination")
	}
	if !strings.Contains(err.Error(), "pointer") {
		t.Fatalf("the error should name the problem, got %v", err)
	}
}

// TestReadJSONAcceptsANilDestination keeps the documented "discard the body" case.
func TestReadJSONAcceptsANilDestination(t *testing.T) {
	srv := newBodyServer(t, "application/json", `{"firstname":"Makis"}`)
	client := New(BaseURL(srv.URL))

	if err := client.ReadJSON(defaultCtx, nil, http.MethodGet, "/", nil); err != nil {
		t.Fatalf("a nil destination discards the body: %v", err)
	}
}
