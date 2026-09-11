package httpclient

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/time/rate"
)

// Every rate limit assertion that involves waiting lives here, inside a synctest
// bubble where the clock is virtual: the waits are free and the assertions are
// exact rather than a loose bound. The same constraint as retry_synctest_test.go
// applies, the transport has to stay in process, which is what Handler gives us.
//
// Two further rules hold for limiters specifically. Build the limiter inside the
// bubble and use it only there, because a limiter also used outside stamps its
// internal clock with real time and the exactness dies. And pick rates that are
// exact in float64: golang.org/x/time/rate computes the delay as
// (tokens/limit)*1e9, so 4/s is a round 250ms while 3/s is 333333333ns.

// TestSharedLimiterIsOneBudgetAcrossClients is the point of the RateLimiter
// option. Two Clients against one host used to mean two budgets, so a program
// mixing calls between them could send twice the documented rate and have the
// excess silently dropped.
func TestSharedLimiterIsOneBudgetAcrossClients(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		handler := flakyHandler(&calls)

		// Two per second, burst two: the third and fourth calls wait 500ms each.
		shared := NewRateLimiter(2)

		content := New(BaseURL("http://example.local"), Handler(handler), RateLimiter(shared))
		catalogue := New(BaseURL("http://example.local"), Handler(handler), RateLimiter(shared))

		start := time.Now()

		for _, c := range []*Client{content, catalogue, content, catalogue} {
			var got testValue
			if err := c.ReadJSON(context.Background(), &got, http.MethodGet, "/", nil); err != nil {
				t.Fatal(err)
			}
		}

		if elapsed := time.Since(start); elapsed != time.Second {
			t.Fatalf("the two clients must share one budget: expected 1s of waiting, got %v", elapsed)
		}
		if calls.Load() != 4 {
			t.Fatalf("expected 4 calls, got %d", calls.Load())
		}
	})
}

// TestSeparateRateLimitsAreSeparateBudgets is the contrast that gives the test
// above its meaning: the same four calls cost nothing when each client builds
// its own limiter, which is the bug the shared limiter fixes.
func TestSeparateRateLimitsAreSeparateBudgets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		handler := flakyHandler(&calls)

		content := New(BaseURL("http://example.local"), Handler(handler), RateLimit(2))
		catalogue := New(BaseURL("http://example.local"), Handler(handler), RateLimit(2))

		start := time.Now()

		for _, c := range []*Client{content, catalogue, content, catalogue} {
			var got testValue
			if err := c.ReadJSON(context.Background(), &got, http.MethodGet, "/", nil); err != nil {
				t.Fatal(err)
			}
		}

		if elapsed := time.Since(start); elapsed != 0 {
			t.Fatalf("each client has its own burst of two, so nothing should wait, got %v", elapsed)
		}
	})
}

// TestSharedKeyedLimiterIsOneBudgetAcrossClients does the same through the named
// registration, and under two different names, because the budget belongs to the
// Limiter instance rather than to the key it was registered under.
func TestSharedKeyedLimiterIsOneBudgetAcrossClients(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		handler := flakyHandler(&calls)

		shared := NewRateLimiter(2)

		content := New(BaseURL("http://example.local"), Handler(handler),
			RateLimiterFor("search", shared))
		catalogue := New(BaseURL("http://example.local"), Handler(handler),
			RateLimiterFor("lookup", shared))

		start := time.Now()

		for _, c := range []struct {
			client *Client
			key    string
		}{
			{content, "search"}, {catalogue, "lookup"},
			{content, "search"}, {catalogue, "lookup"},
		} {
			var got testValue
			if err := c.client.ReadJSON(context.Background(), &got, http.MethodGet, "/", nil,
				RequestRateLimit(c.key)); err != nil {
				t.Fatal(err)
			}
		}

		if elapsed := time.Since(start); elapsed != time.Second {
			t.Fatalf("both keys point at one limiter: expected 1s of waiting, got %v", elapsed)
		}
		if calls.Load() != 4 {
			t.Fatalf("expected 4 calls, got %d", calls.Load())
		}
	})
}

// TestSharedLimiterHoldsUnderConcurrentClients is the shape the motivating bug
// actually has: both clients sending at once, from different goroutines, against
// one quota. Six requests with a burst of two leave four to wait 500ms each, and
// that total does not depend on which goroutine wins a given token.
func TestSharedLimiterHoldsUnderConcurrentClients(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		handler := flakyHandler(&calls)

		shared := NewRateLimiter(2)

		content := New(BaseURL("http://example.local"), Handler(handler), RateLimiter(shared))
		catalogue := New(BaseURL("http://example.local"), Handler(handler), RateLimiter(shared))

		start := time.Now()

		var wg sync.WaitGroup
		for _, c := range []*Client{content, catalogue} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 3 {
					var got testValue
					if err := c.ReadJSON(context.Background(), &got, http.MethodGet, "/", nil); err != nil {
						t.Error(err)
						return
					}
				}
			}()
		}
		wg.Wait()

		if elapsed := time.Since(start); elapsed != 2*time.Second {
			t.Fatalf("six requests on a shared 2/s budget must take 2s, got %v", elapsed)
		}
		if calls.Load() != 6 {
			t.Fatalf("expected 6 calls, got %d", calls.Load())
		}
	})
}

// TestKeyedLimiterIsSharedAcrossCalls is the point of the keyed design: the
// option may be built inline on every call and still share one budget, which
// the old per-call limiter could not do.
func TestKeyedLimiterIsSharedAcrossCalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32

		// One request per second, burst of one: the second call waits a second.
		client := New(BaseURL("http://example.local"), Handler(flakyHandler(&calls)),
			RateLimitFor("search", 1))

		start := time.Now()
		for range 2 {
			var got testValue
			if err := client.ReadJSON(context.Background(), &got, http.MethodGet, "/", nil,
				RequestRateLimit("search")); err != nil {
				t.Fatal(err)
			}
		}

		if elapsed := time.Since(start); elapsed != time.Second {
			t.Fatalf("the second call should have waited on the shared limiter, took %v", elapsed)
		}
	})
}

// TestKeyedLimitersDoNotInterfere keeps separate endpoint budgets separate.
func TestKeyedLimitersDoNotInterfere(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32

		client := New(BaseURL("http://example.local"), Handler(flakyHandler(&calls)),
			RateLimitFor("slow", 1),
			RateLimitFor("fast", 1000),
		)

		start := time.Now()
		for range 3 {
			var got testValue
			if err := client.ReadJSON(context.Background(), &got, http.MethodGet, "/", nil,
				RequestRateLimit("fast")); err != nil {
				t.Fatal(err)
			}
		}

		if elapsed := time.Since(start); elapsed != 0 {
			t.Fatalf("the fast key must not wait on the slow one, took %v", elapsed)
		}
	})
}

// TestKeyedLimiterAppliesToEveryRetryAttempt: per-request limiters used to run
// once, before the retry loop, so retries escaped the budget entirely.
func TestKeyedLimiterAppliesToEveryRetryAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32

		// Four per second with a burst of one: attempts two and three each wait
		// 250ms, which swallows the millisecond backoffs of fastRetry.
		client := New(BaseURL("http://example.local"),
			Handler(flakyHandler(&calls,
				http.StatusServiceUnavailable, http.StatusServiceUnavailable)),
			RateLimiterFor("endpoint", rate.NewLimiter(4, 1)),
			Retry(fastRetry(3)),
		)

		start := time.Now()
		var got testValue
		if err := client.ReadJSON(context.Background(), &got, http.MethodGet, "/", nil,
			RequestRateLimit("endpoint")); err != nil {
			t.Fatal(err)
		}

		if elapsed := time.Since(start); elapsed != 500*time.Millisecond {
			t.Fatalf("retry attempts must wait on the keyed limiter too, took %v", elapsed)
		}
		if calls.Load() != 3 {
			t.Fatalf("expected 3 attempts, got %d", calls.Load())
		}
	})
}
