# Changelog

## v0.0.12

- Add `Retry(RetryPolicy)` client option: exponential backoff with optional jitter, `Retry-After` support, retryable status codes (429, 502, 503, 504 by default), transport-error retries for idempotent methods (`RetryNonIdempotent` to widen), automatic body replay through `http.Request.GetBody`, and an `OnRetry` observer. Every attempt goes through the rate limiter and the request handlers.
- Add `RedactQueryParams(keys...)` client option and `RedactURL(u, keys...)` helper. `APIError` gains a `URL` field holding the redacted request URL, used by `Error()`; `Debug` dumps are scrubbed as well.
- Fix `Handler` option: the given handler was never assigned to the transport, so every request panicked with a nil pointer dereference.
- Fix `RateLimitPerMinute`: the limit was multiplied by 60 instead of divided, allowing 3600 times more requests than configured.
- Fix `Debug` handler panic on transport errors (no response to dump).
- `GetError` (and therefore `GetErrorCode`, `DecodeError`) now uses `errors.As`, so wrapped `APIError` values are found.
- Request handlers that return the response error they were given no longer abort the call; only errors produced by the handler itself do.
- `DialTimeout` uses `Transport.DialContext` instead of the deprecated `Dial`.
- Require Go 1.26.
