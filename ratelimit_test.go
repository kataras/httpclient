package httpclient

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"golang.org/x/time/rate"
)

// The timing assertions for rate limiting live in ratelimit_synctest_test.go,
// where the clock is virtual and they can be exact. What is left here needs no
// clock at all.

func TestRateLimitPerMinuteDividesByTheMinuteValue(t *testing.T) {
	client := New(RateLimitPerMinute(60))

	if client.rateLimiter == nil {
		t.Fatal("expected a rate limiter to be configured")
	}

	limiter, ok := client.rateLimiter.(*rate.Limiter)
	if !ok {
		t.Fatalf("expected the built limiter to be a *rate.Limiter, got %T", client.rateLimiter)
	}

	if got := float64(limiter.Limit()); got != 1 {
		t.Fatalf("60 requests per minute must be 1 request per second, got %v", got)
	}
}

// TestZeroRateLimitDisablesLimiting: a limiter built with burst zero rejects
// every request instead of letting them all through, so the documented
// "zero disables rate limiting" has to be handled explicitly.
func TestZeroRateLimitDisablesLimiting(t *testing.T) {
	for name, client := range map[string]*Client{
		"per second": New(RateLimit(0)),
		"per minute": New(RateLimitPerMinute(0)),
	} {
		if client.rateLimiter != nil {
			t.Fatalf("%s: expected no limiter", name)
		}

		srv := newBodyServer(t, "application/json", `{"firstname":"Makis"}`)
		c := client.Clone(BaseURL(srv.URL))

		var got testValue
		if err := c.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil); err != nil {
			t.Fatalf("%s: a zero rate limit must not block the request: %v", name, err)
		}
	}
}

// TestNewRateLimiterZeroReturnsAnUntypedNil pins the reason the constructors
// return the interface and not *rate.Limiter. A nil *rate.Limiter returned as
// itself would sit inside a non-nil interface and panic on the first Wait.
func TestNewRateLimiterZeroReturnsAnUntypedNil(t *testing.T) {
	if l := NewRateLimiter(0); l != nil {
		t.Fatalf("NewRateLimiter(0) must be nil, got %T", l)
	}
	if l := NewRateLimiterPerMinute(0); l != nil {
		t.Fatalf("NewRateLimiterPerMinute(0) must be nil, got %T", l)
	}
}

// TestNilLimiterDisablesLimiting covers both shapes of nil, including the typed
// one that passes an "l != nil" check and then panics inside Wait.
func TestNilLimiterDisablesLimiting(t *testing.T) {
	var typedNil *rate.Limiter

	for name, client := range map[string]*Client{
		"untyped nil":     New(RateLimiter(nil)),
		"typed nil":       New(RateLimiter(typedNil)),
		"clears an early": New(RateLimit(5), RateLimiter(nil)),
	} {
		if client.rateLimiter != nil {
			t.Fatalf("%s: expected no limiter, got %T", name, client.rateLimiter)
		}

		srv := newBodyServer(t, "application/json", `{"firstname":"Makis"}`)
		c := client.Clone(BaseURL(srv.URL))

		var got testValue
		if err := c.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil); err != nil {
			t.Fatalf("%s: a nil limiter must not block the request: %v", name, err)
		}
	}
}

// TestRateLimiterForNilRemovesTheLimiter keeps the option symmetrical with
// RateLimitFor(key, 0).
func TestRateLimiterForNilRemovesTheLimiter(t *testing.T) {
	var typedNil *rate.Limiter

	client := New(
		RateLimiterFor("search", NewRateLimiter(5)),
		RateLimiterFor("browse", NewRateLimiter(5)),
		RateLimiterFor("search", nil),
		RateLimiterFor("browse", typedNil),
	)

	if _, ok := client.keyedLimiters["search"]; ok {
		t.Fatal("an untyped nil limiter must remove the named limiter")
	}
	if _, ok := client.keyedLimiters["browse"]; ok {
		t.Fatal("a typed nil limiter must remove the named limiter")
	}
}

// TestRateLimitForZeroRemovesTheLimiter keeps the option symmetrical with RateLimit.
func TestRateLimitForZeroRemovesTheLimiter(t *testing.T) {
	client := New(RateLimitFor("search", 5), RateLimitFor("search", 0))

	if _, ok := client.keyedLimiters["search"]; ok {
		t.Fatal("a zero rate must remove the named limiter")
	}
}

// TestUnknownRateLimitKeyIsANoOp: tagging a request before the limiter exists
// must not block it.
func TestUnknownRateLimitKeyIsANoOp(t *testing.T) {
	srv := newBodyServer(t, "application/json", `{"firstname":"Makis"}`)
	client := New(BaseURL(srv.URL))

	var got testValue
	if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil,
		RequestRateLimit("never-registered")); err != nil {
		t.Fatalf("an unknown key must not block: %v", err)
	}
}

// TestCloneSharesAnExplicitLimiterButNotABuiltOne pins the difference between
// the two families of option. Clone replays c.opts, so RateLimit runs the
// constructor again and produces a fresh limiter, while RateLimiter carries the
// instance the caller passed and hands the clone the same one.
func TestCloneSharesAnExplicitLimiterButNotABuiltOne(t *testing.T) {
	shared := NewRateLimiter(20)

	explicit := New(RateLimiter(shared))
	if clone := explicit.Clone(); clone.rateLimiter != shared || explicit.rateLimiter != shared {
		t.Fatal("a limiter passed to RateLimiter must be shared with the clone")
	}

	built := New(RateLimit(20))
	if clone := built.Clone(); clone.rateLimiter == built.rateLimiter {
		t.Fatal("RateLimit must still give the clone its own limiter")
	}

	// The same split for the keyed pair.
	keyed := New(RateLimiterFor("search", shared))
	if clone := keyed.Clone(); clone.keyedLimiters["search"] != shared {
		t.Fatal("a limiter passed to RateLimiterFor must be shared with the clone")
	}

	builtKeyed := New(RateLimitFor("search", 20))
	if clone := builtKeyed.Clone(); clone.keyedLimiters["search"] == builtKeyed.keyedLimiters["search"] {
		t.Fatal("RateLimitFor must still give the clone its own limiter")
	}
}

// countingLimiter is a hand-written Limiter: the point of the interface is that
// a caller can bring their own, a distributed one for instance.
type countingLimiter struct{ waits atomic.Int32 }

func (l *countingLimiter) Wait(ctx context.Context) error {
	l.waits.Add(1)
	return ctx.Err()
}

// TestCustomLimiterIsWaitedOnEveryAttempt: a caller's own Limiter has to be
// reached by the same path the built one is, retries included.
func TestCustomLimiterIsWaitedOnEveryAttempt(t *testing.T) {
	srv := newFlakyServer(t, http.StatusServiceUnavailable, http.StatusServiceUnavailable)

	limiter := new(countingLimiter)
	client := New(BaseURL(srv.URL), RateLimiter(limiter), Retry(fastRetry(3)))

	var got testValue
	if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil); err != nil {
		t.Fatal(err)
	}

	if waits := limiter.waits.Load(); waits != 3 {
		t.Fatalf("expected the custom limiter to be waited on 3 times, got %d", waits)
	}
	if calls := srv.calls.Load(); calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

// TestCustomKeyedLimiterIsCalled does the same through the named registration.
func TestCustomKeyedLimiterIsCalled(t *testing.T) {
	srv := newBodyServer(t, "application/json", `{"firstname":"Makis"}`)

	limiter := new(countingLimiter)
	client := New(BaseURL(srv.URL), RateLimiterFor("search", limiter))

	var got testValue
	for range 2 {
		if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil,
			RequestRateLimit("search")); err != nil {
			t.Fatal(err)
		}
	}

	// An untagged call must not touch it.
	if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil); err != nil {
		t.Fatal(err)
	}

	if waits := limiter.waits.Load(); waits != 2 {
		t.Fatalf("expected only the two tagged calls to wait, got %d", waits)
	}
}
