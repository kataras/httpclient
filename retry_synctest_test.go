package httpclient

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// The tests here run inside a synctest bubble, where time is virtual: a backoff
// of ten seconds costs nothing and the assertions are exact instead of
// approximate. The transport has to stay in process for that to work, because
// network reads are not durably blocking and would stop the fake clock from
// advancing. That is what the Handler option gives us.

// flakyHandler answers with the given statuses in order, then 200.
func flakyHandler(calls *atomic.Int32, statuses ...int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		w.Header().Set("Content-Type", "application/json")

		if n <= len(statuses) {
			w.WriteHeader(statuses[n-1])
			_, _ = w.Write([]byte(`{"error":"try again"}`))
			return
		}

		_, _ = w.Write([]byte(`{"firstname":"Makis"}`))
	})
}

// TestRetryBackoffScheduleIsExact pins the exponential ladder: 500ms then 1s.
func TestRetryBackoffScheduleIsExact(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32

		client := New(
			BaseURL("http://example.local"),
			Handler(flakyHandler(&calls, http.StatusServiceUnavailable, http.StatusServiceUnavailable)),
			Retry(RetryPolicy{
				MaxAttempts:    3,
				InitialBackoff: 500 * time.Millisecond,
				MaxBackoff:     10 * time.Second,
				Multiplier:     2,
			}),
		)

		start := time.Now()

		var got testValue
		if err := client.ReadJSON(context.Background(), &got, http.MethodGet, "/", nil); err != nil {
			t.Fatal(err)
		}

		if elapsed := time.Since(start); elapsed != 1500*time.Millisecond {
			t.Fatalf("expected 500ms + 1s of backoff, got %v", elapsed)
		}
		if calls.Load() != 3 {
			t.Fatalf("expected 3 attempts, got %d", calls.Load())
		}
	})
}

// TestRetryBackoffIsCappedByMaxBackoff stops the ladder growing past the cap.
func TestRetryBackoffIsCappedByMaxBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32

		client := New(
			BaseURL("http://example.local"),
			Handler(flakyHandler(&calls,
				http.StatusServiceUnavailable,
				http.StatusServiceUnavailable,
				http.StatusServiceUnavailable)),
			Retry(RetryPolicy{
				MaxAttempts:    4,
				InitialBackoff: time.Second,
				MaxBackoff:     2 * time.Second,
				Multiplier:     10,
			}),
		)

		start := time.Now()

		var got testValue
		if err := client.ReadJSON(context.Background(), &got, http.MethodGet, "/", nil); err != nil {
			t.Fatal(err)
		}

		// 1s, then capped at 2s twice.
		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("expected 1s + 2s + 2s, got %v", elapsed)
		}
	})
}

// TestRetryAfterHeaderWinsOverTheBackoff covers the seconds form of the header.
func TestRetryAfterHeaderWinsOverTheBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32

		mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if calls.Add(1) == 1 {
				w.Header().Set("Retry-After", "3")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte(`{"firstname":"Makis"}`))
		})

		client := New(
			BaseURL("http://example.local"),
			Handler(mux),
			Retry(RetryPolicy{
				MaxAttempts:    2,
				InitialBackoff: 50 * time.Millisecond,
				MaxBackoff:     time.Minute,
				MaxRetryAfter:  time.Minute,
			}),
		)

		start := time.Now()

		var got testValue
		if err := client.ReadJSON(context.Background(), &got, http.MethodGet, "/", nil); err != nil {
			t.Fatal(err)
		}

		if elapsed := time.Since(start); elapsed != 3*time.Second {
			t.Fatalf("expected the 3 second Retry-After to win, got %v", elapsed)
		}
	})
}

// TestMaxRetryAfterCapsTheHeader keeps a hostile server from parking the client.
func TestMaxRetryAfterCapsTheHeader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32

		mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if calls.Add(1) == 1 {
				w.Header().Set("Retry-After", "3600")
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"firstname":"Makis"}`))
		})

		client := New(
			BaseURL("http://example.local"),
			Handler(mux),
			Retry(RetryPolicy{
				MaxAttempts:   2,
				MaxRetryAfter: 5 * time.Second,
			}),
		)

		start := time.Now()

		var got testValue
		if err := client.ReadJSON(context.Background(), &got, http.MethodGet, "/", nil); err != nil {
			t.Fatal(err)
		}

		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("expected the wait to be capped at 5s, got %v", elapsed)
		}
	})
}

// TestContextCancellationDuringBackoffReturnsImmediately replaces a test that
// slept for real and asserted a loose upper bound.
func TestContextCancellationDuringBackoffReturnsImmediately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32

		client := New(
			BaseURL("http://example.local"),
			Handler(flakyHandler(&calls, http.StatusServiceUnavailable)),
			Retry(RetryPolicy{
				MaxAttempts:    3,
				InitialBackoff: time.Hour,
				MaxBackoff:     time.Hour,
			}),
		)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			synctest.Sleep(time.Second)
			cancel()
		}()

		start := time.Now()

		var got testValue
		err := client.ReadJSON(ctx, &got, http.MethodGet, "/", nil)
		if err == nil {
			t.Fatal("expected the cancellation to be returned")
		}

		if elapsed := time.Since(start); elapsed != time.Second {
			t.Fatalf("expected the wait to end when the context was cancelled, got %v", elapsed)
		}
		if calls.Load() != 1 {
			t.Fatalf("expected no second attempt, got %d", calls.Load())
		}
	})
}

// TestJitterStaysWithinTheBackoff bounds the randomised wait.
func TestJitterStaysWithinTheBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32

		client := New(
			BaseURL("http://example.local"),
			Handler(flakyHandler(&calls, http.StatusServiceUnavailable)),
			Retry(RetryPolicy{
				MaxAttempts:    2,
				InitialBackoff: 4 * time.Second,
				MaxBackoff:     4 * time.Second,
				Jitter:         true,
			}),
		)

		start := time.Now()

		var got testValue
		if err := client.ReadJSON(context.Background(), &got, http.MethodGet, "/", nil); err != nil {
			t.Fatal(err)
		}

		if elapsed := time.Since(start); elapsed > 4*time.Second {
			t.Fatalf("jitter must stay inside the backoff, got %v", elapsed)
		}
	})
}
