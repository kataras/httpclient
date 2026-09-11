package httpclient

import (
	"context"
	"net/http"
	"slices"
	"sync"
)

// RequestHandler can be set to each Client instance and it should be
// responsible to handle the begin and end states of each request.
// Its BeginRequest fires right before the client talks to the server
// and its EndRequest fires right after the client receives a response from the server.
//
// A non-nil error from either one stops the call and is returned to the caller.
// An EndRequest handler that returns the error it was given, wrapped or not,
// is treated as passing it through rather than as a failure of its own.
type RequestHandler interface {
	BeginRequest(context.Context, *http.Request) error
	EndRequest(context.Context, *http.Response, error) error
}

var (
	defaultRequestHandlers []RequestHandler
	mu                     sync.Mutex
)

// RegisterRequestHandler registers one or more request handlers
// to be ran before and after of each request on all newly created HTTP Clients.
// Useful for HTTP Client 3rd-party libraries
// e.g. on init register a custom request-response lifecycle logging.
func RegisterRequestHandler(reqHandlers ...RequestHandler) {
	mu.Lock()
	for _, h := range reqHandlers {
		if h == nil {
			continue
		}

		defaultRequestHandlers = append(defaultRequestHandlers, h)
	}
	mu.Unlock()
}

// cloneDefaultRequestHandlers copies the globally registered handlers for a new
// Client. The copy matters twice: the read races with RegisterRequestHandler,
// and a Client appends to its own list afterwards.
func cloneDefaultRequestHandlers() []RequestHandler {
	mu.Lock()
	defer mu.Unlock()

	return slices.Clone(defaultRequestHandlers)
}
