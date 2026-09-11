package httpclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	jsonv1 "encoding/json"
	json "encoding/json/v2"
)

// A Client is an HTTP client. Initialize with the New package-level function.
type Client struct {
	opts []Option // keep for clones.

	HTTPClient *http.Client

	// BaseURL prepends to all requests.
	BaseURL string

	// A list of persistent request options.
	PersistentRequestOptions []RequestOption

	// Optional rate limiter, see the RateLimit and RateLimiter options.
	rateLimiter Limiter

	// Optional named rate limiters, see the RateLimitFor and RateLimiterFor options.
	keyedLimiters map[string]Limiter

	// Optional handlers that are being fired before and after each new request.
	requestHandlers []RequestHandler

	// Query parameter names whose values are scrubbed from error messages
	// and debug output. See the RedactQueryParams option.
	redactQueryParams []string

	// Extra header names whose values are scrubbed from debug output.
	// See the RedactHeaders option.
	redactHeaders []string

	// Optional retry policy, see the Retry option. Nil disables retrying.
	retry *RetryPolicy

	// encoding/json/v2 options for request payloads and response bodies.
	// See the JSONOptions, JSONMarshalOptions and JSONUnmarshalOptions options.
	jsonMarshalOptions   json.Options
	jsonUnmarshalOptions json.Options
}

// New returns a new HTTP Client.
// Available options:
//   - BaseURL
//   - Timeout
//   - DialTimeout
//   - Transport
//   - Handler
//   - PersistentRequestOptions
//   - RateLimit, RateLimitPerMinute, RateLimiter
//   - RateLimitFor, RateLimitForPerMinute, RateLimiterFor
//   - Retry
//   - OAuth2
//   - RedactQueryParams, RedactHeaders
//   - JSONOptions, JSONMarshalOptions, JSONUnmarshalOptions
//   - Debug
//
// Look the Client.Do/JSON/... methods to send requests,
// the Client.BindXXX methods to receive typed responses,
// the Client.ReadXXX methods to fill a value you already hold and
// Client.Call for endpoints whose body carries nothing.
//
// The default content type to send and receive data is JSON.
func New(opts ...Option) *Client {
	c := &Client{
		HTTPClient:               &http.Client{},
		PersistentRequestOptions: slices.Clone(defaultRequestOptions),
		requestHandlers:          cloneDefaultRequestHandlers(),
		jsonMarshalOptions:       defaultJSONOptions(),
		jsonUnmarshalOptions:     defaultJSONOptions(),
	}

	// Record each option as it is applied, so that NoOption clears only what
	// came before it and a Clone of a Clone keeps the later options.
	c.opts = make([]Option, 0, len(opts))
	for _, opt := range opts {
		if opt == nil {
			continue
		}

		opt(c)
		c.opts = append(c.opts, opt)
	}

	return c
}

// NoOption is a helper function that clears the previous options in the chain.
// See the Client.Clone method.
var NoOption = func(c *Client) { c.opts = c.opts[:0] /* clear previous options */ }

// Clone returns a new Client with the same options as the original.
// If you want to override the options from the base "c" Client,
// use the NoOption variable as the 1st argument.
//
// The clone carries its own request handler list, and it builds its own
// limiters for the rates given to RateLimit, RateLimitPerMinute, RateLimitFor
// and RateLimitForPerMinute, so parent and clone do not share those budgets.
//
// A Limiter handed to RateLimiter or RateLimiterFor is the exception. Clone
// replays the options and that option carries the instance you passed, so the
// clone shares the limiter, and the budget, with its parent.
func (c *Client) Clone(opts ...Option) *Client {
	// slices.Concat always allocates, so sibling clones cannot overwrite
	// each other through a shared backing array.
	return New(slices.Concat(c.opts, opts)...)
}

// RegisterRequestHandler registers one or more request handlers
// to be ran before and after of each new request.
//
// Request handler's BeginRequest method run after each request constructed
// and right before sent to the server.
//
// Request handler's EndRequest method run after response each received
// and right before methods return back to the caller.
//
// Any request handlers MUST be set right after the Client's initialization.
func (c *Client) RegisterRequestHandler(reqHandlers ...RequestHandler) {
	for _, h := range reqHandlers {
		if h == nil {
			continue
		}

		c.requestHandlers = append(c.requestHandlers, h)
	}
}

func (c *Client) emitBeginRequest(ctx context.Context, req *http.Request) error {
	for _, h := range c.requestHandlers {
		if hErr := h.BeginRequest(ctx, req); hErr != nil {
			return hErr
		}
	}

	return nil
}

// emitEndRequest fires the EndRequest handlers. It returns the first error a
// handler produced on its own. A handler that passes "err" back through, wrapped
// or not, is not treated as aborting the call.
func (c *Client) emitEndRequest(ctx context.Context, resp *http.Response, err error) error {
	for _, h := range c.requestHandlers {
		hErr := h.EndRequest(ctx, resp, err)
		if hErr == nil {
			continue
		}

		if err != nil && errors.Is(hErr, err) {
			continue // the handler handed back what it was given.
		}

		return hErr
	}

	return nil
}

// handlerError marks an error returned by a RequestHandler, which aborts the
// whole Do call regardless of any retry policy.
type handlerError struct{ error }

func (e handlerError) Unwrap() error { return e.error }

// withDefaultRequestOption returns a new slice holding "defaults" followed by
// "opts", so an option the caller passed wins over the default. It never writes
// into the caller's backing array.
func withDefaultRequestOption(opts []RequestOption, defaults ...RequestOption) []RequestOption {
	return slices.Concat(defaults, opts)
}

// Do sends an HTTP request and returns an HTTP response.
//
// The payload can be:
//   - io.Reader
//   - raw []byte
//   - JSON raw message
//   - string
//   - url.Values
//   - struct (JSON).
//
// If method is empty then it defaults to "GET".
// The final variadic, optional input argument sets
// the custom request options to use before the request.
//
// Closing the returned response body is up to the caller,
// see Client.DrainResponseBody. The Client.BindXXX and Client.ReadXXX
// methods do that for you.
//
// Any HTTP returned error will be of type APIError
// or a timeout error if the given context was canceled.
func (c *Client) Do(ctx context.Context, method, urlpath string, payload any, opts ...RequestOption) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Method defaults to GET.
	if method == "" {
		method = http.MethodGet
	}

	body, err := c.payloadReader(payload)
	if err != nil {
		return nil, err
	}

	if c.BaseURL != "" {
		urlpath = c.BaseURL + urlpath // note that we don't do any special checks here, the caller is responsible.
	}

	// Initialize the request.
	req, err := http.NewRequestWithContext(ctx, method, urlpath, body)
	if err != nil {
		return nil, err
	}

	// We separate the error for the default options for now.
	for i, opt := range c.PersistentRequestOptions {
		if opt == nil {
			continue
		}

		if err = opt(req); err != nil {
			return nil, fmt.Errorf("client.Do: default request option[%d]: %w", i, err)
		}
	}

	// Apply any custom request options (e.g. content type, accept headers, query...)
	for _, opt := range opts {
		if opt == nil {
			continue
		}

		if err = opt(req); err != nil {
			return nil, err
		}
	}

	return c.send(ctx, req)
}

// payloadReader turns the accepted payload shapes into a request body reader.
func (c *Client) payloadReader(payload any) (io.Reader, error) {
	if payload == nil {
		return nil, nil
	}

	switch v := payload.(type) {
	case io.Reader:
		return v, nil
	case []byte:
		return bytes.NewReader(v), nil
	case jsonv1.RawMessage:
		return bytes.NewReader(v), nil
	case string:
		return strings.NewReader(v), nil
	case url.Values:
		return strings.NewReader(v.Encode()), nil
	default:
		// We assume it's a struct, we won't make use of reflection to find out though.
		w := new(bytes.Buffer)
		if err := encodeJSON(w, v, c.jsonMarshalOptions); err != nil {
			return nil, err
		}
		return w, nil
	}
}

// send runs the request, repeating it according to the configured retry policy.
func (c *Client) send(ctx context.Context, req *http.Request) (*http.Response, error) {
	var (
		resp    *http.Response
		respErr error
	)

	for attempt := 1; ; attempt++ {
		// Each attempt gets its own request value: net/http may mutate the one
		// it was handed and documents that a request must not be reused.
		// Clone from the request's own context, not the outer one: request
		// options such as ClientTrace and RequestRateLimit store their state
		// there and would otherwise be dropped on every attempt.
		attemptReq := req.Clone(req.Context())
		if attempt > 1 && !rewindBody(attemptReq) {
			// The body was consumed and cannot be reproduced;
			// hand back whatever the previous attempt produced.
			return resp, respErr
		}

		resp, respErr = c.attempt(ctx, attemptReq)

		var hErr handlerError
		if errors.As(respErr, &hErr) {
			// A request handler aborted the call; this is not a transport failure.
			// Any response was already released by attempt.
			return nil, hErr.error
		}

		if c.retry == nil || attempt >= c.retry.MaxAttempts || !c.retry.shouldRetry(attemptReq, resp, respErr) {
			return resp, respErr
		}

		wait := c.retry.wait(attempt, resp)
		if c.retry.OnRetry != nil {
			c.retry.OnRetry(attempt, attemptReq, resp, respErr, wait)
		}

		if resp != nil {
			_ = DrainResponseBody(resp) // release the connection before retrying.
		}

		if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
			// Cannot replay the body: give the caller the result we have.
			return resp, respErr
		}

		if err := sleepContext(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// attempt performs a single round trip: it waits on the rate limiters, fires the
// BeginRequest handlers, sends the request and fires the EndRequest handlers.
// An error produced by a request handler is returned as a handlerError, which
// aborts the whole call regardless of any retry policy.
func (c *Client) attempt(ctx context.Context, req *http.Request) (*http.Response, error) {
	if err := c.waitForRateLimits(ctx, req); err != nil {
		return nil, err
	}

	if err := c.emitBeginRequest(ctx, req); err != nil {
		return nil, handlerError{err}
	}

	// Caller is responsible for closing the response body.
	// Also note that the gzip compression is handled automatically nowadays.
	resp, respErr := c.HTTPClient.Do(req)

	if err := c.emitEndRequest(ctx, resp, respErr); err != nil {
		// The response is ours to release: the caller never sees it.
		if resp != nil {
			_ = DrainResponseBody(resp)
		}

		return nil, handlerError{err}
	}

	return resp, respErr
}

// DrainResponseBody drains the response body and closes it, allowing the
// transport to reuse TCP connections. A nil response or a nil body is a no-op.
func DrainResponseBody(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}

	_, err := io.Copy(io.Discard, resp.Body)
	closeErr := resp.Body.Close()

	// errors.Join checks for each err != nil, so no further checks needed.
	return errors.Join(err, closeErr)
}

// DrainResponseBody drains the response body and closes it, allowing the
// transport to reuse TCP connections.
// It's automatically called by the Client.BindXXX and Client.ReadXXX methods.
func (c *Client) DrainResponseBody(resp *http.Response) error {
	return DrainResponseBody(resp)
}

const (
	acceptKey                 = "Accept"
	contentTypeKey            = "Content-Type"
	contentLengthKey          = "Content-Length"
	contentTypePlainText      = "text/plain"
	contentTypeJSON           = "application/json"
	contentTypeFormURLEncoded = "application/x-www-form-urlencoded"
)

// JSON writes data as JSON to the server.
//
// Closing the returned response body is up to the caller,
// see Client.DrainResponseBody.
func (c *Client) JSON(ctx context.Context, method, urlpath string, payload any, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, method, urlpath, payload,
		withDefaultRequestOption(opts, RequestHeader(true, contentTypeKey, contentTypeJSON))...)
}

// Form writes form data to the server.
//
// Closing the returned response body is up to the caller,
// see Client.DrainResponseBody.
func (c *Client) Form(ctx context.Context, method, urlpath string, formValues url.Values, opts ...RequestOption) (*http.Response, error) {
	payload := formValues.Encode()

	// Note: net/http derives Content-Length from the body reader.
	// An outgoing Content-Length header would be ignored.
	return c.Do(ctx, method, urlpath, payload,
		withDefaultRequestOption(opts, RequestHeader(true, contentTypeKey, contentTypeFormURLEncoded))...)
}
