// Package httpclient is a small HTTP client for talking to JSON APIs.
//
// Build a client with New and a few options, then send requests with the
// methods on *Client. The default content type, sent and accepted, is JSON.
//
//	c := httpclient.New(
//		httpclient.BaseURL("https://api.example.com/v1"),
//		httpclient.Timeout(30*time.Second),
//		httpclient.PersistentRequestOptions(
//			httpclient.RequestAuthorizationBearer(token),
//		),
//	)
//
// # Reading responses
//
// There are two ways to get a typed value back, and they differ only in where
// the value lives.
//
// BindJSON returns a new value. There is no destination pointer to get wrong:
//
//	weather, err := c.BindJSON[Weather](ctx, http.MethodGet, "/current", nil)
//
// ReadJSON fills a value you already hold, which is what you want for a
// long-lived field or a pooled struct:
//
//	var weather Weather
//	err := c.ReadJSON(ctx, &weather, http.MethodGet, "/current", nil)
//
// Both close the response body for you. An empty response body is reported as
// io.EOF, which IsErrEmptyJSON also recognises, because several APIs answer a
// successful write with no content.
//
// A response status of 400 or above returns an APIError. Read it with GetError,
// GetErrorCode, or decode its body with the generic BindError.
//
// # Owning the response body
//
// Do, JSON, Form and Uploader.Upload hand you the *http.Response with its body
// still open. Closing it is your job, and DrainResponseBody is the way to do it
// so the connection can be reused. The BindXXX and ReadXXX methods and WriteTo
// do it for you.
//
// # Options
//
// Client options, passed to New or Clone:
//
//	BaseURL, Timeout, DialTimeout, Transport, Handler, PersistentRequestOptions,
//	RateLimit, RateLimitPerMinute, RateLimitFor, RateLimitForPerMinute,
//	RateLimiter, RateLimiterFor,
//	Retry, RedactQueryParams, RedactHeaders, JSONOptions, Debug, NoOption
//
// Request options, passed to any request method:
//
//	RequestHeader, RequestAuthorization, RequestAuthorizationBearer,
//	RequestQuery, RequestParam, RequestRateLimit, ClientTrace
//
// # Requirements
//
// Go 1.27 or newer. The package uses generic methods and encoding/json/v2, so
// it does not build under GOEXPERIMENT=nojsonv2.
package httpclient
