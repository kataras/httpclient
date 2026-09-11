package httpclient

import "golang.org/x/oauth2"

// OAuth2 sends every request with a token obtained from "src", by wrapping the
// Client's current transport in an oauth2.Transport (http.DefaultTransport when
// there is none yet). Tokens are cached and refreshed through
// oauth2.ReuseTokenSource.
//
// Usage with a static token:
//
//	c := httpclient.New(httpclient.OAuth2(oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})))
//
// Or with a configuration that refreshes tokens on its own:
//
//	config := &oauth2.Config{...}
//	src := config.TokenSource(context.Background(), &oauth2.Token{...})
//	c := httpclient.New(httpclient.OAuth2(src))
//
// Order matters: give Transport, Handler or DialTimeout before OAuth2, since it
// wraps whatever transport is set at that point. A DialTimeout given after it
// finds an *oauth2.Transport and does nothing.
//
// The Authorization header is added inside the transport, after the Debug
// handler has dumped the request, so Debug output never shows the token.
func OAuth2(src oauth2.TokenSource) Option {
	return func(c *Client) {
		c.HTTPClient.Transport = &oauth2.Transport{
			Base:   c.HTTPClient.Transport,
			Source: oauth2.ReuseTokenSource(nil, src),
		}
	}
}
