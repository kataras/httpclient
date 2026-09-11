package httpclient

import (
	"context"
	"net/http"

	"golang.org/x/time/rate"
)

// RateLimit configures how many requests per second this Client may send.
// Every attempt, including each retry, waits on the limiter.
//
// A value of zero or less disables rate limiting.
func RateLimit(requestsPerSecond int) Option {
	return func(c *Client) {
		if requestsPerSecond <= 0 {
			c.rateLimiter = nil
			return
		}

		c.rateLimiter = rate.NewLimiter(rate.Limit(requestsPerSecond), requestsPerSecond)
	}
}

// RateLimitPerMinute configures how many requests per minute this Client may send.
//
// A value of zero or less disables rate limiting.
func RateLimitPerMinute(requestsPerMinute int) Option {
	return func(c *Client) {
		if requestsPerMinute <= 0 {
			c.rateLimiter = nil
			return
		}

		ratePerSecond := rate.Limit(float64(requestsPerMinute) / 60.0)
		c.rateLimiter = rate.NewLimiter(ratePerSecond, requestsPerMinute)
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
// budget. A Clone builds its own limiters, it does not share the parent's.
//
// A value of zero or less removes the named limiter.
func RateLimitFor(key string, requestsPerSecond int) Option {
	return func(c *Client) {
		c.setKeyedLimiter(key, requestsPerSecond, rate.Limit(requestsPerSecond))
	}
}

// RateLimitForPerMinute is RateLimitFor expressed per minute.
func RateLimitForPerMinute(key string, requestsPerMinute int) Option {
	return func(c *Client) {
		c.setKeyedLimiter(key, requestsPerMinute, rate.Limit(float64(requestsPerMinute)/60.0))
	}
}

func (c *Client) setKeyedLimiter(key string, burst int, limit rate.Limit) {
	if key == "" {
		return
	}

	if burst <= 0 {
		delete(c.keyedLimiters, key)
		return
	}

	if c.keyedLimiters == nil {
		c.keyedLimiters = make(map[string]*rate.Limiter)
	}

	c.keyedLimiters[key] = rate.NewLimiter(limit, burst)
}

// rateLimitKeyContextKey carries the selected limiter name on the request context.
type rateLimitKeyContextKey struct{}

// RequestRateLimit makes the request wait on the named limiter registered with
// RateLimitFor or RateLimitForPerMinute. The wait happens on every attempt,
// retries included.
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
