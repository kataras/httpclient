package httpclient

import (
	"net/http"
	"net/http/httptrace"
	"net/url"
	"slices"
)

// RequestOption declares the type of option one can pass
// to the Do methods (JSON, Form, ReadJSON, BindJSON...).
// Request options run before the request is sent.
type RequestOption = func(*http.Request) error

// We always add the following request headers, unless they're removed by custom ones.
var defaultRequestOptions = []RequestOption{
	RequestHeader(false, acceptKey, contentTypeJSON),
}

// RequestHeader adds or sets (if overridePrev is true) a header to the request.
func RequestHeader(overridePrev bool, key string, values ...string) RequestOption {
	key = http.CanonicalHeaderKey(key)

	return func(req *http.Request) error {
		// Clone: the option may be reused across requests and net/http may
		// append to the stored slice.
		if overridePrev { // upsert.
			req.Header[key] = slices.Clone(values)
		} else { // just insert.
			req.Header[key] = append(slices.Clone(req.Header[key]), values...)
		}

		return nil
	}
}

// RequestAuthorization sets an Authorization request header.
// Note that we could do the same with a Transport RoundTrip too.
func RequestAuthorization(value string) RequestOption {
	return RequestHeader(true, "Authorization", value)
}

// RequestAuthorizationBearer sets an Authorization: Bearer $token request header.
func RequestAuthorizationBearer(accessToken string) RequestOption {
	headerValue := "Bearer " + accessToken
	return RequestAuthorization(headerValue)
}

// RequestQuery adds a set of URL query parameters to the request.
func RequestQuery(query url.Values) RequestOption {
	return func(req *http.Request) error {
		q := req.URL.Query()
		for k, v := range query {
			q[k] = slices.Clone(v)
		}
		req.URL.RawQuery = q.Encode()

		return nil
	}
}

// RequestParam sets a single URL query parameter to the request.
func RequestParam(key string, values ...string) RequestOption {
	return RequestQuery(url.Values{
		key: values,
	})
}

// ClientTrace adds a client trace to the request.
func ClientTrace(clientTrace *httptrace.ClientTrace) RequestOption {
	return func(req *http.Request) error {
		newReq := req.WithContext(httptrace.WithClientTrace(req.Context(), clientTrace))
		*req = *newReq
		return nil
	}
}
