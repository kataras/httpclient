package httpclient

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const secret = "s3cr3t-api-key"

func TestRedactURLReplacesOnlyTheListedQueryValues(t *testing.T) {
	u, _ := url.Parse("https://example.com/rest/x?apiKey=" + secret + "&string=fracture&token=" + secret)

	got := RedactURL(u, "apiKey", "token")

	if strings.Contains(got, secret) {
		t.Fatalf("secret leaked: %s", got)
	}
	if !strings.Contains(got, "apiKey=REDACTED") || !strings.Contains(got, "token=REDACTED") {
		t.Fatalf("expected both keys redacted, got %s", got)
	}
	if !strings.Contains(got, "string=fracture") {
		t.Fatalf("unrelated params must be untouched, got %s", got)
	}
	if u.Query().Get("apiKey") != secret {
		t.Fatal("RedactURL must not mutate its input")
	}
}

func TestRedactURLWithoutKeysReturnsTheURLUnchanged(t *testing.T) {
	u, _ := url.Parse("https://example.com/x?apiKey=" + secret)
	if got := RedactURL(u); got != u.String() {
		t.Fatalf("expected %s, got %s", u.String(), got)
	}
}

func newSecretServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"name":"UnauthorizedError","status":401,"message":"Invalid API Key"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAPIErrorMessageRedactsConfiguredQueryParams(t *testing.T) {
	srv := newSecretServer(t, http.StatusUnauthorized)
	client := New(
		BaseURL(srv.URL),
		PersistentRequestOptions(RequestParam("apiKey", secret)),
		RedactQueryParams("apiKey"),
	)

	err := client.ReadJSON(defaultCtx, nil, http.MethodGet, "/rest/x", nil)
	if err == nil {
		t.Fatal("expected an APIError")
	}

	if strings.Contains(err.Error(), secret) {
		t.Fatalf("APIError.Error() leaked the secret: %s", err)
	}
	if !strings.Contains(err.Error(), "apiKey=REDACTED") {
		t.Fatalf("expected the redacted URL in the message, got %s", err)
	}

	apiErr, ok := GetError(err)
	if !ok {
		t.Fatal("expected an APIError")
	}
	if !strings.Contains(apiErr.URL, "apiKey=REDACTED") || strings.Contains(apiErr.URL, secret) {
		t.Fatalf("expected APIError.URL to be redacted, got %s", apiErr.URL)
	}
	// The raw response is still available for callers who need it.
	if apiErr.Response.Request.URL.Query().Get("apiKey") != secret {
		t.Fatal("the underlying request must stay intact")
	}
}

func TestAPIErrorMessageWithoutRedactionKeepsTheURL(t *testing.T) {
	srv := newSecretServer(t, http.StatusUnauthorized)
	client := New(BaseURL(srv.URL), PersistentRequestOptions(RequestParam("apiKey", secret)))

	err := client.ReadJSON(defaultCtx, nil, http.MethodGet, "/rest/x", nil)
	if err == nil || !strings.Contains(err.Error(), "apiKey="+secret) {
		t.Fatalf("without RedactQueryParams the URL is printed as-is, got %v", err)
	}
}

func TestDebugLoggerRedactsConfiguredQueryParams(t *testing.T) {
	srv := newSecretServer(t, http.StatusOK)
	logger := new(bufferLogger)
	client := New(
		BaseURL(srv.URL),
		PersistentRequestOptions(RequestParam("apiKey", secret)),
		RedactQueryParams("apiKey"),
		Debug(logger),
	)

	_ = client.ReadJSON(defaultCtx, nil, http.MethodGet, "/rest/x", nil)

	out := logger.buf.String()
	if out == "" {
		t.Fatal("expected debug output")
	}
	if strings.Contains(out, secret) {
		t.Fatalf("debug output leaked the secret:\n%s", out)
	}
	if !strings.Contains(out, "REDACTED") {
		t.Fatalf("expected REDACTED marker in debug output:\n%s", out)
	}
}
