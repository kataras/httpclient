# HTTP Client

[![build status](https://img.shields.io/github/actions/workflow/status/kataras/httpclient/ci.yml?style=for-the-badge)](https://github.com/kataras/httpclient/actions) [![report card](https://img.shields.io/badge/report%20card-a%2B-ff3333.svg?style=for-the-badge)](https://goreportcard.com/report/github.com/kataras/httpclient) [![godocs](https://img.shields.io/badge/go-%20docs-488AC7.svg?style=for-the-badge)](https://pkg.go.dev/github.com/kataras/httpclient/)

HTTP Client is a small HTTP client for Go, for talking to JSON APIs. Requires Go 1.27.

```go
package main

import (
	"context"
	"net/http"
	"net/url"

	"github.com/kataras/httpclient"
)

// The BaseURL of our API client.
const BaseURL = "https://api.weatherapi.com/v1"

type (
	Options struct {
		APIKey string `json:"api_key" yaml:"APIKey" toml:"APIKey"`
	}

	Client struct {
		*httpclient.Client
	}
)

func NewClient(opts Options) *Client {
	apiKeyParameterSetter := httpclient.RequestParam("key", opts.APIKey)

	c := httpclient.New(
		httpclient.BaseURL(BaseURL),
		httpclient.PersistentRequestOptions(apiKeyParameterSetter),
	)

	return &Client{c}
}

func (c *Client) GetCurrentByCity(ctx context.Context, city string) (resp Response, err error) {
	urlpath := "/current.json"
	// ?q=Athens&aqi=no
	params := httpclient.RequestQuery(url.Values{
		"q":   []string{city},
		"aqi": []string{"no"},
	})

	err = c.Client.ReadJSON(ctx, &resp, http.MethodGet, urlpath, nil, params)
	return
}

```

Some of the features HTTP Client offers:

* Typed responses through generic methods
* Rate limits, per client, per endpoint, or one budget shared by several clients
* Retry with backoff
* Query parameter and header redaction in errors and debug output
* Middleware
* JSON (read & write)
* Forms
* File upload
* Plain text
* Debug and more...

### Typed responses

`BindJSON` names the response type at the call site and returns it. There is no destination pointer, so there is no way to pass the wrong kind of value.

```go
weather, err := c.BindJSON[Response](ctx, http.MethodGet, "/current.json", nil, params)
```

`ReadJSON` fills a value you already hold, which is what you want for a long-lived field or a pooled struct. Both close the response body for you.

```go
var weather Response
err := c.ReadJSON(ctx, &weather, http.MethodGet, "/current.json", nil, params)
```

`BindPlain[T]` does the same for plain text bodies, into a string, byte slice or number. `Bind[T](resp)` decodes a response you already hold, choosing by `Content-Type`. `BindError[T](err)` decodes the body an `APIError` carries.

An empty response body comes back as `io.EOF`, which `IsErrEmptyJSON` also recognises. Several APIs answer a successful write with no content, so that is a normal outcome rather than a failure.

### Who closes the body

`Do`, `JSON`, `Form` and `Uploader.Upload` hand you the `*http.Response` with its body still open. Closing it is your job, and `DrainResponseBody` is how to do it so the connection can be reused. The `Bind` and `Read` methods and `WriteTo` do it for you.

### Retries

Retrying is opt-in. Pass a `RetryPolicy` and failed attempts are repeated with exponential backoff (a `Retry-After` response header wins when present). By default only network errors on idempotent methods and the 429, 502, 503 and 504 statuses are retried; buffered request bodies are replayed automatically.

```go
c := httpclient.New(
	httpclient.BaseURL(BaseURL),
	httpclient.RateLimit(20),
	httpclient.Retry(httpclient.RetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		Jitter:         true,
	}),
)
```

Every attempt waits on the rate limiter and is visible to the registered request handlers, so `Debug` output shows the failed attempts as well.

### Rate limits per endpoint

`RateLimit` covers the whole API. When one endpoint has a tighter budget of its own, register a named limiter and tag the calls that use it. Every call sharing the key shares the budget, retries included.

```go
c := httpclient.New(
	httpclient.BaseURL(BaseURL),
	httpclient.RateLimit(20),             // the whole API
	httpclient.RateLimitFor("search", 2), // this endpoint
)

results, err := c.BindJSON[Results](ctx, http.MethodGet, "/search", nil,
	httpclient.RequestRateLimit("search"))
```

A `Clone` builds its own limiters rather than sharing the parent's. When you want the opposite, read on.

### One budget across several clients

Sometimes one upstream quota covers more than one client. A host that allows 20 requests per second counts them per IP address, not per client, so two clients against that host with `RateLimit(20)` each can send 40 and have the excess dropped with no error.

Build the limiter yourself and hand the same one to both:

```go
// The host allows 20 requests per second, whoever is asking.
var hostLimiter = httpclient.NewRateLimiter(20)

// Different path prefix, different API key, same quota.
content := httpclient.New(
	httpclient.BaseURL(host+"/rest"),
	httpclient.RateLimiter(hostLimiter),
)
catalogue := httpclient.New(
	httpclient.BaseURL(host),
	httpclient.RateLimiter(hostLimiter),
)
```

`RateLimiterFor(key, limiter)` does the same for a named limiter. Clients sharing a limiter share the budget behind it, even when they register it under different names.

This is the one case where a `Clone` does share: the option carries the limiter you passed, and `Clone` replays the options, so the clone waits on the same one. `RateLimit` and `RateLimitFor` keep building a fresh limiter per clone.

A `Limiter` is any type with `Wait(ctx) error`, so a distributed limiter of your own works here too. It has to be safe for concurrent use.

### Redacting secrets

When the API key travels in the query string it would otherwise be printed by `APIError.Error()` and by the `Debug` dumps. Register the parameter names once and their values are replaced with `REDACTED` in all rendered text. The request itself is not modified.

```go
c := httpclient.New(
	httpclient.BaseURL(BaseURL),
	httpclient.PersistentRequestOptions(httpclient.RequestParam("apiKey", opts.APIKey)),
	httpclient.RedactQueryParams("apiKey"),
	httpclient.Debug(golog.Default),
)
```

`RedactHeaders` adds header names to the list. `Authorization`, `Proxy-Authorization`, `Cookie` and `Set-Cookie` are always scrubbed, and a password in a URL is masked.

Request and response bodies are not scrubbed. There is no general way to find a secret inside one, so do not send a secret in a body and expect `Debug` to hide it.

`httpclient.RedactURL(u, "apiKey")` is available for your own log lines.

## 📖 Learning HTTP Client

### Installation

The only requirement is the [Go Programming Language](https://go.dev/dl/), version 1.27 or newer. The package uses generic methods and `encoding/json/v2`.

#### Create a new project

```sh
$ mkdir myapp
$ cd myapp
$ go mod init myapp
$ go get github.com/kataras/httpclient
```

<details><summary>Install on existing project</summary>

```sh
$ cd myapp
$ go get github.com/kataras/httpclient
```

**Run**

```sh
$ go mod tidy
$ go run .
```

</details>

<br/>

Navigate through [_examples](_examples) folder for more.

## 📝 License

This project is licensed under the [MIT License](LICENSE).
