package httpclient

import (
	"net/http"
	"testing"
)

// TestAPIErrorWithoutResponsePrintsTheBody: server code builds APIError values
// with a body and no round trip behind them.
func TestAPIErrorWithoutResponsePrintsTheBody(t *testing.T) {
	err := APIError{Body: []byte(`{"message":"remote failure"}`)}
	if got := err.Error(); got != `{"message":"remote failure"}` {
		t.Fatalf("expected the body alone, got %q", got)
	}

	if got := (APIError{}).Error(); got != "" {
		t.Fatalf("the zero value must render empty, got %q", got)
	}
}

// TestAPIErrorWithoutRequestHasNoLeadingSeparator.
func TestAPIErrorWithoutRequestHasNoLeadingSeparator(t *testing.T) {
	err := APIError{
		Response: &http.Response{StatusCode: http.StatusBadGateway, Status: "502 Bad Gateway"},
		Body:     []byte(`upstream down`),
	}

	if got, want := err.Error(), "502 Bad Gateway: upstream down"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
