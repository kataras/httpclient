package httpclient

import (
	"net/http"
	"testing"
	"time"
)

func TestRateLimitPerMinuteDividesByTheMinuteValue(t *testing.T) {
	client := New(RateLimitPerMinute(60))

	if client.rateLimiter == nil {
		t.Fatal("expected a rate limiter to be configured")
	}

	if got := float64(client.rateLimiter.Limit()); got != 1 {
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

// TestKeyedLimiterIsSharedAcrossCalls is the point of the keyed design: the
// option may be built inline on every call and still share one budget, which
// the old per-call limiter could not do.
func TestKeyedLimiterIsSharedAcrossCalls(t *testing.T) {
	srv := newBodyServer(t, "application/json", `{"firstname":"Makis"}`)

	// One request per second, burst of one: the second call has to wait.
	client := New(BaseURL(srv.URL), RateLimitFor("search", 1))

	start := time.Now()
	for range 2 {
		var got testValue
		if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil,
			RequestRateLimit("search")); err != nil {
			t.Fatal(err)
		}
	}

	if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
		t.Fatalf("the second call should have waited on the shared limiter, took %v", elapsed)
	}
}

// TestKeyedLimitersDoNotInterfere keeps separate endpoint budgets separate.
func TestKeyedLimitersDoNotInterfere(t *testing.T) {
	srv := newBodyServer(t, "application/json", `{"firstname":"Makis"}`)

	client := New(BaseURL(srv.URL),
		RateLimitFor("slow", 1),
		RateLimitFor("fast", 1000),
	)

	start := time.Now()
	for range 3 {
		var got testValue
		if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil,
			RequestRateLimit("fast")); err != nil {
			t.Fatal(err)
		}
	}

	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("the fast key must not wait on the slow one, took %v", elapsed)
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

// TestKeyedLimiterAppliesToEveryRetryAttempt: per-request limiters used to run
// once, before the retry loop, so retries escaped the budget entirely.
func TestKeyedLimiterAppliesToEveryRetryAttempt(t *testing.T) {
	srv := newFlakyServer(t, http.StatusServiceUnavailable, http.StatusServiceUnavailable)

	// Burst 1 at 4/s: attempts two and three each wait about 250ms.
	client := New(BaseURL(srv.URL), RateLimitFor("endpoint", 4), Retry(fastRetry(3)))
	client.keyedLimiters["endpoint"].SetBurst(1)

	start := time.Now()
	var got testValue
	if err := client.ReadJSON(defaultCtx, &got, http.MethodGet, "/", nil,
		RequestRateLimit("endpoint")); err != nil {
		t.Fatal(err)
	}

	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("retry attempts must wait on the keyed limiter too, took %v", elapsed)
	}
	if srv.calls.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", srv.calls.Load())
	}
}

// TestRateLimitForZeroRemovesTheLimiter keeps the option symmetrical with RateLimit.
func TestRateLimitForZeroRemovesTheLimiter(t *testing.T) {
	client := New(RateLimitFor("search", 5), RateLimitFor("search", 0))

	if _, ok := client.keyedLimiters["search"]; ok {
		t.Fatal("a zero rate must remove the named limiter")
	}
}
