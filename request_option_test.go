package httpclient

import (
	"net/http"
	"net/url"
	"testing"
)

func newEchoClient(t *testing.T, seen **http.Request, opts ...Option) *Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"firstname":"Makis"}`))
	})

	client := New(append([]Option{BaseURL("http://example.local"), Handler(mux)}, opts...)...)
	client.RegisterRequestHandler(recordingHandler{onBegin: func(req *http.Request) {
		*seen = req
	}})

	return client
}

// TestRequestHeaderClonesItsValues: the variadic slice is captured by the
// closure and reused on every request, so storing it directly let one request
// see another one's appended values.
func TestRequestHeaderClonesItsValues(t *testing.T) {
	values := []string{"first"}
	option := RequestHeader(true, "X-Test", values...)

	req, _ := http.NewRequest(http.MethodGet, "http://example.local/", nil)
	if err := option(req); err != nil {
		t.Fatal(err)
	}

	req.Header["X-Test"] = append(req.Header["X-Test"], "second")

	if len(values) != 1 || values[0] != "first" {
		t.Fatalf("the caller's slice was written through: %v", values)
	}
}

// TestRequestQueryClonesTheCallersValues covers the same aliasing on url.Values.
func TestRequestQueryClonesTheCallersValues(t *testing.T) {
	query := url.Values{"q": []string{"athens"}}
	option := RequestQuery(query)

	req, _ := http.NewRequest(http.MethodGet, "http://example.local/", nil)
	if err := option(req); err != nil {
		t.Fatal(err)
	}

	if got := req.URL.Query().Get("q"); got != "athens" {
		t.Fatalf("expected the query to be applied, got %q", got)
	}

	// Mutating the request's query must not reach back into the caller's map.
	q := req.URL.Query()
	q["q"][0] = "changed"

	if query["q"][0] != "athens" {
		t.Fatalf("the caller's url.Values was mutated: %v", query)
	}
}

// TestCallerContentTypeWinsOverTheDefault: the forced JSON content type was
// appended after the caller's options, so a caller could never send anything
// else through ReadJSON or JSON.
func TestCallerContentTypeWinsOverTheDefault(t *testing.T) {
	var seen *http.Request
	client := newEchoClient(t, &seen)

	var got testValue
	err := client.ReadJSON(defaultCtx, &got, http.MethodPost, "/", "a=b",
		RequestHeader(true, contentTypeKey, contentTypeFormURLEncoded))
	if err != nil {
		t.Fatal(err)
	}

	if ct := seen.Header.Get(contentTypeKey); ct != contentTypeFormURLEncoded {
		t.Fatalf("the caller's content type must win, got %q", ct)
	}
}

// TestDefaultContentTypeStillApplies keeps the convenience when the caller
// says nothing.
func TestDefaultContentTypeStillApplies(t *testing.T) {
	var seen *http.Request
	client := newEchoClient(t, &seen)

	var got testValue
	if err := client.ReadJSON(defaultCtx, &got, http.MethodPost, "/", testValue{}); err != nil {
		t.Fatal(err)
	}

	if ct := seen.Header.Get(contentTypeKey); ct != contentTypeJSON {
		t.Fatalf("expected the JSON default, got %q", ct)
	}
}

// TestRequestOptionsSliceIsNotStomped: appending the default content type to a
// caller-supplied slice with spare capacity wrote into the caller's array.
func TestRequestOptionsSliceIsNotStomped(t *testing.T) {
	var seen *http.Request
	client := newEchoClient(t, &seen)

	// Spare capacity is what makes append reuse the backing array.
	callerOpts := make([]RequestOption, 1, 4)
	callerOpts[0] = RequestHeader(true, "X-Caller", "kept")

	var got testValue
	if err := client.ReadJSON(defaultCtx, &got, http.MethodPost, "/", testValue{}, callerOpts...); err != nil {
		t.Fatal(err)
	}

	if len(callerOpts) != 1 {
		t.Fatalf("the caller's slice grew: %d", len(callerOpts))
	}
	if callerOpts[0] == nil {
		t.Fatal("the caller's option was overwritten")
	}
	if seen.Header.Get("X-Caller") != "kept" {
		t.Fatal("the caller's option did not run")
	}
}

// TestFormDoesNotSetAContentLengthHeader: net/http derives the length from the
// body, and an outgoing Content-Length header is ignored anyway.
func TestFormDoesNotSetAContentLengthHeader(t *testing.T) {
	var seen *http.Request
	client := newEchoClient(t, &seen)

	resp, err := client.Form(defaultCtx, http.MethodPost, "/", url.Values{"a": {"b"}})
	if err != nil {
		t.Fatal(err)
	}
	defer DrainResponseBody(resp)

	if got := seen.Header.Get(contentLengthKey); got != "" {
		t.Fatalf("Form should not set a Content-Length header, got %q", got)
	}
	if seen.ContentLength != int64(len("a=b")) {
		t.Fatalf("expected ContentLength to be set from the body, got %d", seen.ContentLength)
	}
}
