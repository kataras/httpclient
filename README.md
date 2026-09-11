# HTTP Client

[![build status](https://img.shields.io/github/actions/workflow/status/kataras/httpclient/ci.yml?style=for-the-badge)](https://github.com/kataras/httpclient/actions) [![report card](https://img.shields.io/badge/report%20card-a%2B-ff3333.svg?style=for-the-badge)](https://goreportcard.com/report/github.com/kataras/httpclient) [![godocs](https://img.shields.io/badge/go-%20docs-488AC7.svg?style=for-the-badge)](https://pkg.go.dev/github.com/kataras/httpclient/)

HTTP Client is a simple HTTP/2 client for Go.

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

* Rate Limit
* Retry with backoff
* Secret redaction in errors and debug output
* Middleware
* JSON (read & write)
* Forms
* File Upload
* Plain Text
* Debug and more...

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

`httpclient.RedactURL(u, "apiKey")` is available for your own log lines.

## 📖 Learning HTTP Client

### Installation

The only requirement is the [Go Programming Language](https://go.dev/dl/).

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
