# Changelog

## v0.1.0

First release since v0.0.11. The v0.0.12 work was never tagged, so everything it added is listed here too.

Requires **Go 1.27**. The package uses generic methods and `encoding/json/v2`, so it does not build under `GOEXPERIMENT=nojsonv2`. Modules that import it need `go 1.27` in their own `go.mod`.

### Typed responses through generic methods

Go 1.27 lets a method declare its own type parameters, so the response type can be named at the call site with no destination pointer to get wrong:

```go
weather, err := c.BindJSON[Weather](ctx, http.MethodGet, "/current.json", nil, params)
```

- `Client.BindJSON[T]` returns a decoded `T`. A status of 400 or above returns an `APIError`, an empty body returns `io.EOF`.
- `Client.BindPlain[T]` reads the body as plain text into a `string`, `[]byte`, `int`, `int64` or `float64`, and into named types with those underlying types.
- `Bind[T](resp)` decodes a response you already hold, picking the decoder from its `Content-Type`.
- `BindError[T](err)` decodes the body carried by an `APIError`. It hands back the original error when there was no `APIError`, so "not an API error" stays distinguishable from "could not decode it".

Nothing was removed for this. `ReadJSON`, `ReadPlain`, `GetPlainUnquote`, `WriteTo` and `BindResponse` fill a value you already own, which is still what you want for a long-lived field or a pooled struct.

### JSON engine

Encoding and decoding now go through `encoding/json/v2`, configured with `encoding/json.DefaultOptionsV1()`. Behaviour is unchanged: field matching stays case-insensitive, duplicate object names stay tolerated, and an empty response body still comes back as `io.EOF`.

`JSONOptions(opts...)` sets your own `json/v2` options on a client, for example `JSONOptions(json.RejectUnknownMembers(true))`.

### Keyed rate limits

`RequestRateLimit(requestsPerSecond int)` built a new limiter every time it was called, so the inline form the doc suggested enforced nothing. It is replaced by a limiter that lives on the client:

```go
c := httpclient.New(
    httpclient.RateLimit(20),             // the whole API
    httpclient.RateLimitFor("search", 2), // this endpoint
)
c.ReadJSON(ctx, &v, "GET", "/search", nil, httpclient.RequestRateLimit("search"))
```

Every call sharing a key shares the budget, and the wait happens on every retry attempt rather than once before the loop. A `Clone` builds its own limiters.

### Other additions

- `Transport(rt)` sets the client's `http.RoundTripper`, for an instrumented transport or for the in-memory network of `httptest.NewTestServer`.
- `RedactHeaders(names...)` adds header names to scrub from `Debug` output. `Authorization`, `Proxy-Authorization`, `Cookie` and `Set-Cookie` are always scrubbed.
- `APIError.Code()` returns the status code and tolerates an error with no response.
- `APIError.ReadErr` holds the error that stopped the body being read, which used to be discarded.
- `ErrUploaderClosed` is returned when an `Uploader` is used twice.
- The package has a doc comment.

### Breaking changes

- **Go 1.27 is required.** Dependents must raise their own `go` directive.
- **`RequestRateLimit` takes a limiter key**, not a rate. Old call sites fail to compile. `RequestRateLimitPerMinute` is gone; register the limiter with `RateLimitForPerMinute`.
- **`GetErrorCode` returns 0**, not 200, when the error is not an `APIError` or carries no response. Returning 200 read as success. Check for zero before using the result as a status code.
- **`GetError` reports false for an `APIError` with no response**, so `apiErr.Response` is safe to read without a nil check.
- **`APIError.Error()` no longer prints the status twice.** The message was `example.com/x: Not Found (404 Not Found)` and is now `example.com/x: 404 Not Found`.
- **`ReadJSON` rejects a destination that is not a non-nil pointer.** It used to accept a struct value, decode into a throwaway copy and return nil.
- **`New(a, NoOption, b)` now behaves like `New(b)` on a second `Clone`.** Options applied after `NoOption` were run but never recorded, so they vanished the next time the client was cloned.
- `BindResponse` and `DecodeError` are marked deprecated in favour of `Bind` and `BindError`. Both still work.

### Fixes

- A response was dropped without being closed when an `EndRequest` handler returned an error, holding the connection until GC.
- `DrainResponseBody` read the body before checking it for nil, so a hand-built response panicked. A nil response is now a no-op too.
- A failed body rewind inside the retry loop returned `client.Do: unreachable` and threw away the response the server had actually sent.
- `Clone` appended into the parent's option slice, so two clones of one parent could overwrite each other's options.
- `New` read the package-level request handler list without holding its mutex, which is a data race, and then shared its backing array with every client.
- `New` also shared the package-level `defaultRequestOptions` slice, which `PersistentRequestOptions` appended to.
- `Debug` passed the request and response dumps as the format string, so a percent-encoded URL or a `%s` in a body printed `%!s(MISSING)`. The test double concatenated instead of formatting, which is why nobody saw it.
- Redaction replaced every occurrence of the secret value anywhere in the dump. A value of `1` or `dev` corrupted headers, timestamps and the body. The request line is now rewritten through the parsed URL and header values are replaced whole. Bodies are still not scrubbed, and the docs now say so.
- `RedactURL` printed `user:password@host` in full. It masks the password now.
- The retry loop reused one `*http.Request` across attempts, which `net/http` documents against. Each attempt gets a clone, taken from the request's own context so `ClientTrace` and the rate-limit key survive.
- `contentTypePlainText` was `plain/text`, so `BindResponse` could never bind a `text/plain` response and always reported an unexpected mime type.
- The forced `Content-Type` was applied after the caller's request options, so a caller could not send form-encoded data through `ReadJSON` or `JSON`. Defaults now run first and the caller wins.
- `RequestHeader` stored the caller's variadic slice in the header map, and `RequestQuery` stored the caller's `url.Values`. Both clone now.
- Appending the default content type to a caller-supplied `opts` slice could write into the caller's backing array.
- `handlerTransport` set `Status` to `Not Found` instead of `404 Not Found`, left `Proto` and `ContentLength` unset, never closed the request body, and shallow copied the request so a handler writing to `r.Header` mutated the caller's request.
- `Form` set a `Content-Length` header, which `net/http` ignores on outgoing requests.
- `WriteTo` closed the response body without draining it, leaving the connection unusable after an early copy error.
- `Uploader.Upload` closed the multipart writer, so a second call sent a body with no closing boundary. It returns `ErrUploaderClosed` now.
- `RateLimit(0)` and `RateLimitPerMinute(0)` built a limiter with a burst of zero, which rejected every request. They disable limiting, as documented.
- `DialTimeout` replaced the whole transport with a bare `&http.Transport{}`, whose nil `Proxy` field silently turned off `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`, along with the connection pool and TLS timeouts. It now clones `http.DefaultTransport`, sets the dialer on an existing `*http.Transport`, and leaves a custom `RoundTripper` alone.
- The `Timeout` doc claimed a 15 second default that never existed. There is no default; the doc says so rather than a timeout appearing under long downloads.
- The unused `keepAlive` field is gone. Its type assertion never succeeded for the default transport anyway.

### From the untagged v0.0.12

- `Retry(RetryPolicy)`: exponential backoff with optional jitter, `Retry-After` support, retryable status codes (429, 502, 503, 504 by default), transport-error retries for idempotent methods (`RetryNonIdempotent` to widen), body replay through `http.Request.GetBody`, and an `OnRetry` observer.
- `RedactQueryParams(keys...)` and `RedactURL(u, keys...)`. `APIError` gained a `URL` field holding the redacted request URL.
- Fix `Handler`: the handler was never assigned to the transport, so every request panicked with a nil pointer dereference.
- Fix `RateLimitPerMinute`: the limit was multiplied by 60 instead of divided, allowing 3600 times more requests than configured.
- Fix the `Debug` handler panic on transport errors, where there is no response to dump.
- `GetError` uses `errors.As`, so wrapped `APIError` values are found.
- A request handler that returns the response error it was given no longer aborts the call. That now holds for a wrapped copy too.

### Internals

- `client.go` was 688 lines covering six jobs. It is split into `client.go`, `request_option.go`, `read.go`, `bind.go`, `upload.go`, `json.go`, `ratelimit.go` and `doc.go`.
- The retry timing tests run under `testing/synctest`, so the backoff ladder, the `Retry-After` cap and cancellation mid-backoff are asserted exactly and cost no wall-clock time. The test that slept for real and checked a two second upper bound is gone.
- CI runs `go vet` and `go test -race`. The root package had never been race tested. The examples build against the checked-out library instead of fetching `@main`, and the step no longer passes on failure.
- Coverage is 82%, up from roughly half the package.

## v0.0.11 and earlier

See the git history.
