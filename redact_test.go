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

// TestRedactURLMasksUserinfo: basic auth credentials in a BaseURL used to be
// printed in full by every error message that embedded the URL.
func TestRedactURLMasksUserinfo(t *testing.T) {
	u, _ := url.Parse("https://alice:hunter2@example.com/x?apiKey=" + secret)

	got := RedactURL(u, "apiKey")
	if strings.Contains(got, "hunter2") {
		t.Fatalf("the password leaked: %s", got)
	}

	withoutKeys := RedactURL(u)
	if strings.Contains(withoutKeys, "hunter2") {
		t.Fatalf("the password leaked with no keys given: %s", withoutKeys)
	}
}

// TestRedactDoesNotRewriteUnrelatedText: the old implementation replaced every
// occurrence of the raw secret value anywhere in the dump, so a short value
// corrupted headers, timestamps and the body.
func TestRedactDoesNotRewriteUnrelatedText(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"1","firstname":"Makis"}`))
	})

	logger := new(bufferLogger)
	client := New(
		BaseURL("http://example.local"),
		Handler(mux),
		// A one-character secret: the worst case for blind replacement.
		PersistentRequestOptions(RequestParam("v", "1")),
		RedactQueryParams("v"),
		Debug(logger),
	)

	var got testValue
	if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil); err != nil {
		t.Fatal(err)
	}

	out := logger.String()
	if !strings.Contains(out, "v=REDACTED") {
		t.Fatalf("the query value should be redacted, got %q", out)
	}
	if !strings.Contains(out, `"firstname":"Makis"`) {
		t.Fatalf("the response body must be left alone, got %q", out)
	}
	if strings.Contains(out, "HTTP/REDACTED.REDACTED") {
		t.Fatalf("the protocol version was corrupted by redaction: %q", out)
	}
}

// TestDebugRedactsTheAuthorizationHeader: the README sold secret redaction, but
// only query parameters were ever scrubbed.
func TestDebugRedactsTheAuthorizationHeader(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"firstname":"Makis"}`))
	})

	logger := new(bufferLogger)
	client := New(
		BaseURL("http://example.local"),
		Handler(mux),
		PersistentRequestOptions(RequestAuthorizationBearer(secret)),
		Debug(logger),
	)

	var got testValue
	if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(logger.String(), secret) {
		t.Fatalf("the bearer token leaked into the debug output: %q", logger.String())
	}
}

// TestDebugDoesNotTreatTheDumpAsAFormatString: a percent sign in a
// percent-encoded URL or in a body turned the log line into %!s(MISSING) noise.
func TestDebugDoesNotTreatTheDumpAsAFormatString(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"firstname":"100%s done"}`))
	})

	logger := new(bufferLogger)
	client := New(
		BaseURL("http://example.local"),
		Handler(mux),
		PersistentRequestOptions(RequestParam("q", "a b&c")),
		Debug(logger),
	)

	var got testValue
	if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil); err != nil {
		t.Fatal(err)
	}

	out := logger.String()
	for _, bad := range []string{"%!s(MISSING)", "%!(NOVERB)", "%!s(EXTRA"} {
		if strings.Contains(out, bad) {
			t.Fatalf("the dump was used as a format string: %q", out)
		}
	}
}
