package httpclient

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"
)

// TestOAuth2SendsTheBearerToken: the option wraps the transport, so every
// request carries the token from the source.
func TestOAuth2SendsTheBearerToken(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"firstname":"Makis"}`))
	}))
	t.Cleanup(srv.Close)

	c := New(BaseURL(srv.URL), OAuth2(oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "secret-token"})))

	if _, err := c.BindJSON[testValue](defaultCtx, http.MethodGet, "/", nil); err != nil {
		t.Fatal(err)
	}

	if got != "Bearer secret-token" {
		t.Fatalf("expected the bearer token on the request, got %q", got)
	}
}

// TestOAuth2WrapsTheTransportGivenBeforeIt: a Transport set earlier stays the base.
func TestOAuth2WrapsTheTransportGivenBeforeIt(t *testing.T) {
	base := &http.Transport{}
	c := New(Transport(base), OAuth2(oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "x"})))

	tr, ok := c.HTTPClient.Transport.(*oauth2.Transport)
	if !ok {
		t.Fatalf("expected an *oauth2.Transport, got %T", c.HTTPClient.Transport)
	}
	if tr.Base != base {
		t.Fatal("OAuth2 must wrap the transport that was set before it")
	}
}
