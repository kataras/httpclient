package httpclient

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newAPIErrorClient(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(BaseURL(srv.URL))
}

// TestGetErrorFindsWrappedAPIErrors: SDKs wrap APIError with %w; GetError must still find it.
func TestGetErrorFindsWrappedAPIErrors(t *testing.T) {
	client := newAPIErrorClient(t, http.StatusNotFound, `{"name":"NotFoundError"}`)

	err := client.ReadJSON(defaultCtx, nil, http.MethodGet, "/missing", nil)
	if err == nil {
		t.Fatal("expected an APIError for a 404 response")
	}

	wrapped := fmt.Errorf("sdk: %w", err)

	apiErr, ok := GetError(wrapped)
	if !ok {
		t.Fatalf("GetError must unwrap %%w-wrapped errors, got ok=false for %v", wrapped)
	}

	if apiErr.Response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", apiErr.Response.StatusCode)
	}

	if GetErrorCode(wrapped) != http.StatusNotFound {
		t.Fatalf("GetErrorCode must see through wrapping, got %d", GetErrorCode(wrapped))
	}

	if !errors.As(wrapped, new(APIError)) {
		t.Fatal("errors.As must find the APIError as well")
	}
}
