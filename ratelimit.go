package httpclient

import (
	"context"
	"net/http"

	"golang.org/x/time/rate"
)

// A Limiter blocks until the next request may be sent, or until the ctx ends.
//
// The *Limiter of golang.org/x/time/rate satisfies this as written, and
// NewRateLimiter returns one. An implementation must be safe for concurrent
// use: the Client waits on it from every goroutine sending through the Client,
// and a Limiter shared between Clients is waited on by all of them.
type Limiter interface {
	Wait(ctx context.Context) error
}

// NewRateLimiter returns a Limiter that allows requestsPerSecond requests per
// second, with a burst of the same size. Hand it to RateLimiter or
// RateLimiterFor to give several Clients one budget.
//
// A value of zero or less returns nil, which disables limiting.
//
// The result is the Limiter interface rather than the concrete type on purpose.
// A nil *rate.Limiter returned as itself would arrive inside a non-nil
// interface and panic on the first Wait.
func NewRateLimiter(requestsPerSecond int) Limiter {
	if requestsPerSecond <= 0 {
		return nil
	}

	return rate.NewLimiter(rate.Limit(requestsPerSecond), requestsPerSecond)
}

// NewRateLimiterPerMinute is NewRateLimiter expressed per minute. The burst is
// the whole minute's worth, so a minute of requests may leave at once.
//
// A value of zero or less returns nil, which disables limiting.
func NewRateLimiterPerMinute(requestsPerMinute int) Limiter {
	if requestsPerMinute <= 0 {
		return nil
	}

	ratePerSecond := rate.Limit(float64(requestsPerMinute) / 60.0)
	return rate.NewLimiter(ratePerSecond, requestsPerMinute)
}

// RateLimit configures how many requests per second this Client may send.
// Every attempt, including each retry, waits on the limiter.
//
// The Client builds the limiter, so a Clone builds its own and the two do not
// share a budget. Use RateLimiter to give several Clients one limiter you own.
//
// A value of zero or less disables rate limiting.
func RateLimit(requestsPerSecond int) Option {
	return func(c *Client) {
		c.rateLimiter = NewRateLimiter(requestsPerSecond)
	}
}

// RateLimitPerMinute configures how many requests per minute this Client may send.
//
// A value of zero or less disables rate limiting.
func RateLimitPerMinute(requestsPerMinute int) Option {
	return func(c *Client) {
		c.rateLimiter = NewRateLimiterPerMinute(requestsPerMinute)
	}
}

// RateLimiter sets a Limiter you own as this Client's client-wide limiter,
// in place of the one RateLimit would build. Clients given the same Limiter
// share one budget, which is what an upstream quota counted per IP address
// rather than per client asks for:
//
//	// The host allows 20 requests per second, whoever is asking.
//	var hostLimiter = httpclient.NewRateLimiter(20)
//
//	content := httpclient.New(
//		httpclient.BaseURL(host+"/rest"),
//		httpclient.RateLimiter(hostLimiter),
//	)
//	catalogue := httpclient.New(
//		httpclient.BaseURL(host),
//		httpclient.RateLimiter(hostLimiter),
//	)
//
// Unlike RateLimit, a Clone shares the limiter with its parent rather than
// building its own: Clone replays the options and this one carries the very
// instance you passed.
//
// A nil Limiter disables limiting, clearing anything set earlier in the chain.
// The Limiter's error is returned as it came, so a context error stays
// comparable with errors.Is.
func RateLimiter(l Limiter) Option {
	return func(c *Client) {
		c.rateLimiter = limiterOrNil(l)
	}
}

// RateLimitFor registers a named rate limiter on this Client, on top of any
// client-wide one. Requests select it with the RequestRateLimit request option,
// so several endpoints can carry their own budget:
//
//	c := httpclient.New(
//		httpclient.RateLimit(20),                  // the whole API.
//		httpclient.RateLimitFor("search", 2),      // this endpoint only.
//	)
//	c.ReadJSON(ctx, &v, "GET", "/search", nil, httpclient.RequestRateLimit("search"))
//
// The limiter belongs to the Client, so every call sharing the key shares the
// budget. A Clone builds its own, it does not share the parent's. Use
// RateLimiterFor to give several Clients one limiter you own.
//
// A value of zero or less removes the named limiter.
func RateLimitFor(key string, requestsPerSecond int) Option {
	return func(c *Client) {
		c.setKeyedLimiter(key, NewRateLimiter(requestsPerSecond))
	}
}

// RateLimitForPerMinute is RateLimitFor expressed per minute.
func RateLimitForPerMinute(key string, requestsPerMinute int) Option {
	return func(c *Client) {
		c.setKeyedLimiter(key, NewRateLimiterPerMinute(requestsPerMinute))
	}
}

// RateLimiterFor registers a Limiter you own under a name, in place of the one
// RateLimitFor would build. Requests select it with RequestRateLimit, and every
// Client given the same Limiter shares the budget behind that name, even when
// they register it under different names.
//
// As with RateLimiter, a Clone shares the limiter rather than building its own.
//
// A nil Limiter removes the named limiter.
func RateLimiterFor(key string, l Limiter) Option {
	return func(c *Client) {
		c.setKeyedLimiter(key, limiterOrNil(l))
	}
}

// limiterOrNil returns l, or nil when l carries no limiter. A nil *rate.Limiter
// inside a non-nil interface passes an "l != nil" check and then panics on Wait,
// and that is the only typed nil this package can hand out.
func limiterOrNil(l Limiter) Limiter {
	if v, ok := l.(*rate.Limiter); ok && v == nil {
		return nil
	}

	return l
}

// setKeyedLimiter stores l under key. A nil limiter deletes the entry rather
// than storing nil, so waitForRateLimits can keep reading an empty map as
// "nothing registered" and never finds a nil to wait on.
func (c *Client) setKeyedLimiter(key string, l Limiter) {
	if key == "" {
		return
	}

	if l == nil {
		delete(c.keyedLimiters, key)
		return
	}

	if c.keyedLimiters == nil {
		c.keyedLimiters = make(map[string]Limiter)
	}

	c.keyedLimiters[key] = l
}

// rateLimitKeyContextKey carries the selected limiter name on the request context.
type rateLimitKeyContextKey struct{}

// RequestRateLimit makes the request wait on the named limiter registered with
// RateLimitFor, RateLimitForPerMinute or RateLimiterFor. The wait happens on
// every attempt, retries included.
//
// An unknown key is a no-op, so a request option can be registered before the
// limiter it names. A request carries one key; the last option to set it wins.
func RequestRateLimit(key string) RequestOption {
	return func(req *http.Request) error {
		ctx := context.WithValue(req.Context(), rateLimitKeyContextKey{}, key)
		*req = *req.WithContext(ctx)
		return nil
	}
}

// waitForRateLimits blocks until both the client-wide limiter and the named one,
// if the request selected one, allow this attempt through.
func (c *Client) waitForRateLimits(ctx context.Context, req *http.Request) error {
	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return err
		}
	}

	if len(c.keyedLimiters) == 0 || req == nil {
		return nil
	}

	key, ok := req.Context().Value(rateLimitKeyContextKey{}).(string)
	if !ok {
		return nil
	}

	if limiter, registered := c.keyedLimiters[key]; registered {
		return limiter.Wait(ctx)
	}

	return nil
}
