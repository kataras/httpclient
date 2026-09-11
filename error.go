package httpclient

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	jsonv1 "encoding/json"
)

// APIError errors that may return from the Client.
type APIError struct {
	Response *http.Response
	// Body is the raw response body, already read and closed.
	Body jsonv1.RawMessage
	// URL is the request URL as it should be displayed, with the values of any
	// query parameters registered through RedactQueryParams replaced by "REDACTED".
	// Empty when the error was built by the package-level ExtractError.
	URL string
	// ReadErr holds the error that stopped the body from being read in full,
	// if there was one. Body then holds whatever arrived before it.
	ReadErr error
}

// Error implements the standard error type.
func (e APIError) Error() string {
	if e.Response == nil {
		return ""
	}

	var b strings.Builder

	if e.URL != "" {
		b.WriteString(e.URL)
	} else if e.Response.Request != nil && e.Response.Request.URL != nil {
		b.WriteString(e.Response.Request.URL.String())
	}
	b.WriteString(": ")

	// Response.Status already reads "404 Not Found". Only fall back to the
	// generated text when a hand-built response left it empty.
	if e.Response.Status != "" {
		b.WriteString(e.Response.Status)
	} else {
		b.WriteString(http.StatusText(e.Response.StatusCode))
	}

	if len(e.Body) > 0 {
		b.WriteString(": ")
		b.Write(e.Body)
	}

	return b.String()
}

// Code returns the response status code, or zero when the error carries no
// response. Unlike GetErrorCode it never panics on a zero APIError.
func (e APIError) Code() int {
	if e.Response == nil {
		return 0
	}

	return e.Response.StatusCode
}

// ExtractError returns the response wrapped inside an APIError.
// It reads and closes the response body.
func ExtractError(resp *http.Response) APIError {
	apiErr := APIError{Response: resp}

	if resp == nil || resp.Body == nil {
		return apiErr
	}

	body, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()

	apiErr.Body = body
	apiErr.ReadErr = errors.Join(err, closeErr)

	return apiErr
}

// extractError is like ExtractError but renders the request URL through
// the Client's RedactQueryParams configuration.
func (c *Client) extractError(resp *http.Response) APIError {
	apiErr := ExtractError(resp)
	if resp != nil && resp.Request != nil {
		apiErr.URL = RedactURL(resp.Request.URL, c.redactQueryParams...)
	}

	return apiErr
}

// GetError reports whether the given "err" is, or wraps, an APIError.
//
// It reports false for an APIError carrying no response, so that callers can
// read apiErr.Response without a nil check.
func GetError(err error) (APIError, bool) {
	if err == nil {
		return APIError{}, false
	}

	apiErr, ok := errors.AsType[APIError](err)
	if !ok || apiErr.Response == nil {
		return APIError{}, false
	}

	return apiErr, true
}

// DecodeError binds a json error to the "destPtr".
//
// Prefer the generic BindError, which returns the decoded value.
func DecodeError(err error, destPtr any) error {
	apiErr, ok := GetError(err)
	if !ok {
		return err
	}

	return decodeJSON(bytes.NewReader(apiErr.Body), destPtr, defaultJSONOptions())
}

// GetErrorCode reads an error, which should be a type of APIError,
// and returns its status code.
//
// It returns zero when "err" is nil, is not an APIError, or carries no
// response. Check for zero before using the result as a status code.
func GetErrorCode(err error) int {
	apiErr, ok := GetError(err)
	if !ok {
		return 0
	}

	return apiErr.Response.StatusCode
}
