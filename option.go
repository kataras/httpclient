package httpclient

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
)

// All the builtin client options should live here, for easy discovery.
// Rate limiting lives in ratelimit.go, redaction in redact.go,
// retrying in retry.go, JSON configuration in json.go and OAuth2 in oauth2.go.

type Option = func(*Client)

// BaseURL registers the base URL of this client.
// All of its methods will prepend this url.
func BaseURL(uri string) Option {
	return func(c *Client) {
		c.BaseURL = strings.TrimSuffix(uri, "/")
	}
}

// Timeout specifies a time limit for requests made by this
// Client. The timeout includes connection time, any
// redirects, and reading the response body.
// A Timeout of zero means no timeout.
//
// There is no default: without this option a request runs until the server
// answers or the context is done. Set one for anything talking to the internet.
func Timeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.HTTPClient.Timeout = timeout
	}
}

// DialTimeout specifies a time limit for creating connections
// by this Client. It is the maximum amount of time a dial will wait
// for a connect to complete. If the connection establishing process
// takes longer than this value, the dial will be cancelled.
//
// The timeout includes name resolution, if required.
// When using TCP, and the host in the address parameter resolves to
// multiple IP addresses, the timeout is spread over each consecutive
// dial, such that each is given an appropriate fraction of the time
// to connect.
//
// It sets the dialer of the Client's *http.Transport, cloning
// http.DefaultTransport when the Client has no transport yet, so proxy
// settings and pool defaults are kept. A Client whose transport is not an
// *http.Transport, such as one built by the Handler option, is left alone.
func DialTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		dialer := &net.Dialer{Timeout: timeout}

		switch transport := c.HTTPClient.Transport.(type) {
		case nil:
			// Clone the default transport rather than building a bare one:
			// a bare *http.Transport has a nil Proxy, which silently turns off
			// the HTTP_PROXY, HTTPS_PROXY and NO_PROXY variables that a plain
			// client honours, along with the connection pool defaults.
			cloned := http.DefaultTransport.(*http.Transport).Clone()
			cloned.DialContext = dialer.DialContext
			c.HTTPClient.Transport = cloned
		case *http.Transport:
			// Keep whatever else was configured on it.
			transport.DialContext = dialer.DialContext
		default:
			// A custom RoundTripper does its own dialing; nothing to set.
		}
	}
}

// Transport sets the http.RoundTripper the Client sends through.
//
// Use it to plug in an instrumented transport, a recorded one, or the transport
// of an httptest.NewTestServer, whose in-memory network the default transport
// cannot reach:
//
//	srv := httptest.NewTestServer(t, mux)
//	c := httpclient.New(httpclient.BaseURL(srv.URL), httpclient.Transport(srv.Client().Transport))
//
// Transport, DialTimeout and Handler all write the same field. The last one
// given wins, except that DialTimeout only adjusts an *http.Transport.
func Transport(rt http.RoundTripper) Option {
	return func(c *Client) {
		c.HTTPClient.Transport = rt
	}
}

// Handler specifies an http.Handler
// instance which can be tested using this Client.
//
// It registers a custom HTTP client transport
// which allows "fake calls" to the "h" server. Use it for testing.
//
// It replaces the Client's transport, so a DialTimeout given before it has no
// effect and one given after it is ignored.
//
// The response is recorded in full before the Client sees a byte, so a
// streaming handler (Server-Sent Events, chunked output) or a mid-response
// cancellation cannot be exercised through it. For those cases use the
// Transport option with an httptest.NewTestServer.
func Handler(h http.Handler) Option {
	return func(c *Client) {
		c.HTTPClient.Transport = &handlerTransport{handler: h}
	}
}

// PersistentRequestOptions adds one or more persistent request options
// that all requests made by this Client will respect.
func PersistentRequestOptions(reqOpts ...RequestOption) Option {
	return func(c *Client) {
		c.PersistentRequestOptions = append(c.PersistentRequestOptions, reqOpts...)
	}
}

// A DebugLogger receives the request and response dumps of the Debug option.
// Most loggers satisfy it as written, for example *golog.Logger.
type DebugLogger interface {
	Debugf(string, ...any)
}

// DefaultDebugLogger is what Debug uses when handed a nil logger. It prints
// through the standard log package, so log.SetOutput and log.SetFlags apply.
var DefaultDebugLogger DebugLogger = stdDebugLogger{}

type stdDebugLogger struct{}

func (stdDebugLogger) Debugf(format string, args ...any) {
	log.Printf("HTTP Client: "+format, args...)
}

// Debug enables the client's debug logger.
// It fires right before request is created
// and right after a response from the server is received.
//
// Example Output for request:
//
//	[DBUG] 2022/03/01 21:54 HTTP Client: POST / HTTP/1.1
//	Host: 127.0.0.1:50948
//	User-Agent: Go-http-client/1.1
//	Content-Length: 22
//	Accept: application/json
//	Content-Type: application/json
//	Accept-Encoding: gzip
//
//	{"firstname":"Makis"}
//
// Example Output for response:
//
//	[DBUG] 2022/03/01 21:54 HTTP Client: HTTP/1.1 200 OK
//	Content-Length: 27
//	Content-Type: application/json; charset=utf-8
//	Date: Tue, 01 Mar 2022 19:54:03 GMT
//
//	{
//	    "firstname": "Makis"
//	}
//
// Values of query parameters registered through RedactQueryParams,
// and the credentials of any Authorization header, are replaced
// by "REDACTED" in the output.
//
// A nil logger uses DefaultDebugLogger, the standard log package.
func Debug(logger DebugLogger) Option {
	return func(c *Client) {
		if logger == nil {
			logger = DefaultDebugLogger
		}

		handler := &debugRequestHandler{
			logger: logger,
			client: c,
		}

		c.requestHandlers = append(c.requestHandlers, handler)
	}
}

type debugRequestHandler struct {
	logger DebugLogger
	client *Client // to read the redaction configuration at request time.
}

func (h *debugRequestHandler) redact(text string, req *http.Request) string {
	if h.client == nil {
		return text
	}

	return redactText(text, req, h.client.redactQueryParams, h.client.redactHeaders)
}

func (h *debugRequestHandler) BeginRequest(ctx context.Context, req *http.Request) error {
	dump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		return err
	}

	// The dump is data, never a format string: it carries percent signs from
	// percent-encoded URLs and from response bodies.
	h.logger.Debugf("%s", h.redact(string(dump), req))
	return nil
}

func (h *debugRequestHandler) EndRequest(ctx context.Context, resp *http.Response, err error) error {
	if err != nil {
		if resp != nil && resp.Request != nil {
			h.logger.Debugf("%s: %s: ERR: %s", resp.Request.Method,
				h.redact(resp.Request.URL.String(), resp.Request), err.Error())
		} else {
			// Transport errors (dial, TLS, timeout) carry no response.
			h.logger.Debugf("HTTP Client: ERR: %s", err.Error())
		}

		return nil // observers never abort the call.
	}

	dump, dumpErr := httputil.DumpResponse(resp, true)
	if dumpErr != nil {
		return dumpErr
	}

	h.logger.Debugf("%s", h.redact(string(dump), resp.Request))
	return nil
}
