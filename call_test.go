package httpclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCallSucceedsOnANoContentResponse: Call reports through the status only.
func TestCallSucceedsOnANoContentResponse(t *testing.T) {
	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	if err := New(BaseURL(srv.URL)).Call(defaultCtx, http.MethodDelete, "/todos/1", nil); err != nil {
		t.Fatalf("expected nil for a 204, got %v", err)
	}
	if method != http.MethodDelete {
		t.Fatalf("expected DELETE, got %s", method)
	}
}

// TestCallReturnsAPIErrorOnFailureStatus.
func TestCallReturnsAPIErrorOnFailureStatus(t *testing.T) {
	c := newAPIErrorClient(t, http.StatusNotFound, `{"message":"gone"}`)

	err := c.Call(defaultCtx, http.MethodGet, "/missing", nil)
	if GetErrorCode(err) != http.StatusNotFound {
		t.Fatalf("expected a 404 APIError, got %v", err)
	}
}

// TestCallSendsAPayloadAsJSON: a non-nil payload gets the JSON content type.
func TestCallSendsAPayloadAsJSON(t *testing.T) {
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	if err := New(BaseURL(srv.URL)).Call(defaultCtx, http.MethodPost, "/", testValue{Firstname: "Makis"}); err != nil {
		t.Fatal(err)
	}
	if contentType != "application/json" {
		t.Fatalf("expected application/json, got %q", contentType)
	}
}
