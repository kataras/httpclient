package httpclient

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encoding/json/jsontext"
	json "encoding/json/v2"
)

// echoServer writes the request body back as JSON.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, r.Body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestJSONMarshalOptionsAffectOnlyEncoding: the encode side is lenient about
// invalid UTF-8 (the bad byte leaves as U+FFFD) while the decode side, set to
// the strict v2 defaults, still rejects invalid UTF-8 coming from the server.
// The two directions carry their own option set.
func TestJSONMarshalOptionsAffectOnlyEncoding(t *testing.T) {
	opts := []Option{
		JSONMarshalOptions(jsontext.AllowInvalidUTF8(true)),
		JSONUnmarshalOptions(json.DefaultOptionsV2()),
	}

	t.Run("encode is lenient", func(t *testing.T) {
		srv := echoServer(t)
		c := New(append([]Option{BaseURL(srv.URL)}, opts...)...)

		got, err := c.BindJSON[testValue](defaultCtx, http.MethodPost, "/", testValue{Firstname: "bad\xff"})
		if err != nil {
			t.Fatalf("the encode side must accept the invalid UTF-8, got %v", err)
		}
		if got.Firstname != "bad�" {
			t.Fatalf("expected the bad byte replaced by U+FFFD, got %q", got.Firstname)
		}
	})

	t.Run("decode stays strict", func(t *testing.T) {
		srv := newBodyServer(t, "application/json", "{\"firstname\":\"bad\xff\"}")
		c := New(append([]Option{BaseURL(srv.URL)}, opts...)...)

		if _, err := c.BindJSON[testValue](defaultCtx, http.MethodGet, "/", nil); err == nil {
			t.Fatal("expected the strict decode side to reject the invalid UTF-8 body")
		}

		// The default decode side (v1 semantics) accepts the same body.
		if _, err := New(BaseURL(srv.URL)).BindJSON[testValue](defaultCtx, http.MethodGet, "/", nil); err != nil {
			t.Fatalf("the default decode options must accept invalid UTF-8, got %v", err)
		}
	})
}

// TestJSONUnmarshalOptionsAffectOnlyDecoding: rejecting unknown members on decode
// must not stop a payload from being encoded.
func TestJSONUnmarshalOptionsAffectOnlyDecoding(t *testing.T) {
	srv := newBodyServer(t, "application/json", `{"firstname":"Makis","extra":1}`)

	c := New(BaseURL(srv.URL), JSONUnmarshalOptions(json.RejectUnknownMembers(true)))

	_, err := c.BindJSON[testValue](defaultCtx, http.MethodPost, "/", testValue{Firstname: "Makis"})
	if err == nil {
		t.Fatal("expected the unknown member to be rejected")
	}
}

// TestJSONOptionsSetBothSides: the original option keeps its meaning.
func TestJSONOptionsSetBothSides(t *testing.T) {
	srv := newBodyServer(t, "application/json", `{"firstname":"Makis","extra":1}`)

	c := New(BaseURL(srv.URL), JSONOptions(json.RejectUnknownMembers(true)))

	if _, err := c.BindJSON[testValue](defaultCtx, http.MethodGet, "/", nil); err == nil {
		t.Fatal("expected the unknown member to be rejected")
	}
}

// TestPackageLevelBindersAcceptOptions: Bind, BindError, BindResponse and
// DecodeError take optional json.Options that replace the package default.
func TestPackageLevelBindersAcceptOptions(t *testing.T) {
	strict := json.RejectUnknownMembers(true)
	body := `{"firstname":"Makis","extra":1}`

	newResp := func() *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}
	}

	if _, err := Bind[testValue](newResp()); err != nil {
		t.Fatalf("default options must accept the unknown member, got %v", err)
	}
	if _, err := Bind[testValue](newResp(), strict); err == nil {
		t.Fatal("Bind must apply the given options")
	}

	var v testValue
	if err := BindResponse(newResp(), &v, strict); err == nil {
		t.Fatal("BindResponse must apply the given options")
	}

	apiErr := APIError{Response: newResp(), Body: []byte(body)}
	if _, err := BindError[testValue](apiErr, strict); err == nil {
		t.Fatal("BindError must apply the given options")
	}
	if err := DecodeError(apiErr, &v, strict); err == nil {
		t.Fatal("DecodeError must apply the given options")
	}
	if err := DecodeError(apiErr, &v); err != nil {
		t.Fatalf("DecodeError without options keeps the default, got %v", err)
	}
	if _, err := BindError[testValue](errors.New("plain")); err == nil {
		t.Fatal("a non-API error must still pass through")
	}
}
