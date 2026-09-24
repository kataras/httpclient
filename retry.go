package httpclient

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// RetryPolicy configures automatic retries of failed requests.
// Register it with the Retry client option.
//
// A request is retried when the transport returned an error (network failure,
// timeout) and the method is idempotent (or RetryNonIdempotent is set), or when
// the response status code is one of StatusCodes. Requests whose context is done
// are never retried. Bodies are replayed through http.Request.GetBody, which the
// standard library provides for the buffered payloads Do accepts ([]byte, string,
// url.Values, JSON-encoded structs); a raw io.Reader payload is sent once.
//
// Every attempt waits on the client's rate limiter and is visible to the
// registered request handlers, so debug output shows the failed attempts too.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts including the first one.
	// Values below 2 disable retrying.
	MaxAttempts int
	// InitialBackoff is the wait before the second attempt. Defaults to 500ms.
	InitialBackoff time.Duration
	// MaxBackoff caps the exponential backoff. Defaults to 10s.
	MaxBackoff time.Duration
	// Multiplier grows the backoff on each attempt. Defaults to 2.
	Multiplier float64
	// Jitter, when true, randomises each wait in [0, backoff].
	Jitter bool
	// StatusCodes lists the response status codes that trigger a retry.
	// Defaults to 429, 502, 503 and 504.
	StatusCodes []int
	// RetryOn, when set, replaces the built-in decision (StatusCodes and
	// transport-error rules). It receives the request, the response (nil on a
	// transport error) and the error (nil on a received response).
	RetryOn func(req *http.Request, resp *http.Response, err error) bool
	// MaxRetryAfter caps the wait requested by a Retry-After response header.
	// Defaults to MaxBackoff. A Retry-After header, when present and valid,
	// takes precedence over the exponential backoff.
	MaxRetryAfter time.Duration
	// RetryNonIdempotent allows retrying POST, PATCH and other non-idempotent
	// methods (GET, HEAD, OPTIONS, TRACE, PUT, DELETE and QUERY are idempotent)
	// after a transport error. Retries on a received response status
	// are always allowed, since the server did process the request.
	RetryNonIdempotent bool
	// OnRetry, when set, is called right before each wait with the attempt number
	// that just failed (1-based), its request, response, error and the wait duration.
	OnRetry func(attempt int, req *http.Request, resp *http.Response, err error, wait time.Duration)
}

// Retry enables automatic retries according to the given policy.
// A policy with MaxAttempts below 2 disables retrying.
func Retry(policy RetryPolicy) Option {
	return func(c *Client) {
		if policy.MaxAttempts < 2 {
			c.retry = nil
			return
		}

		p := policy.normalized()
		c.retry = &p
	}
}

var defaultRetryStatusCodes = []int{
	http.StatusTooManyRequests,
	http.StatusBadGateway,
	http.StatusServiceUnavailable,
	http.StatusGatewayTimeout,
}

// normalized returns a copy of the policy with defaults applied.
func (p RetryPolicy) normalized() RetryPolicy {
	if p.InitialBackoff <= 0 {
		p.InitialBackoff = 500 * time.Millisecond
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = 10 * time.Second
	}
	if p.Multiplier <= 0 {
		p.Multiplier = 2
	}
	if p.StatusCodes == nil {
		p.StatusCodes = defaultRetryStatusCodes
	}
	if p.MaxRetryAfter <= 0 {
		p.MaxRetryAfter = p.MaxBackoff
	}
	return p
}

// shouldRetry reports whether the attempt that produced resp/err may be repeated.
func (p RetryPolicy) shouldRetry(req *http.Request, resp *http.Response, err error) bool {
	if req != nil {
		if ctxErr := req.Context().Err(); ctxErr != nil {
			return false
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	if p.RetryOn != nil {
		return p.RetryOn(req, resp, err)
	}

	if err != nil {
		return p.RetryNonIdempotent || isIdempotent(req)
	}

	if resp == nil {
		return false
	}

	for _, code := range p.StatusCodes {
		if resp.StatusCode == code {
			return true
		}
	}

	return false
}

// methodQuery is the RFC 10008 QUERY method: safe and idempotent, with a body.
// It becomes http.MethodQuery once go.mod requires Go 1.28.
const methodQuery = "QUERY"

// isIdempotent follows RFC 9110 section 9.2.2, plus QUERY (RFC 10008).
func isIdempotent(req *http.Request) bool {
	if req == nil {
		return false
	}

	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace, http.MethodPut, http.MethodDelete, methodQuery, "":
		return true
	default:
		return false
	}
}

// wait returns how long to sleep after the given failed attempt (1-based).
// A valid Retry-After header wins, capped by MaxRetryAfter; otherwise the
// exponential backoff (with optional jitter) applies, capped by MaxBackoff.
func (p RetryPolicy) wait(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if d, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
			if d > p.MaxRetryAfter {
				d = p.MaxRetryAfter
			}
			return d
		}
	}

	if attempt < 1 {
		attempt = 1
	}

	backoff := float64(p.InitialBackoff) * math.Pow(p.Multiplier, float64(attempt-1))
	if backoff > float64(p.MaxBackoff) || math.IsInf(backoff, 0) || math.IsNaN(backoff) {
		backoff = float64(p.MaxBackoff)
	}

	d := time.Duration(backoff)
	if p.Jitter && d > 0 {
		d = time.Duration(rand.Int64N(int64(d) + 1))
	}

	return d
}

// parseRetryAfter understands both forms of the Retry-After header:
// a number of seconds or an HTTP-date.
func parseRetryAfter(value string) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}

	if secs, err := strconv.Atoi(value); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}

	if at, err := http.ParseTime(value); err == nil {
		d := time.Until(at)
		if d < 0 {
			d = 0
		}
		return d, true
	}

	return 0, false
}

// sleepContext waits for d or until ctx is done, whichever comes first.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// rewindBody prepares req for another attempt. It reports false when the body
// was consumed and cannot be reproduced.
func rewindBody(req *http.Request) bool {
	if req.Body == nil || req.Body == http.NoBody {
		return true
	}

	if req.GetBody == nil {
		return false
	}

	body, err := req.GetBody()
	if err != nil {
		return false
	}

	req.Body = body
	return true
}
