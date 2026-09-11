package httpclient

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// APIError errors that may return from the Client.
type APIError struct {
	Response *http.Response
	Body     json.RawMessage // may be any []byte, response body is closed at this point.
	// URL is the request URL as it should be displayed, with the values of any
	// query parameters registered through RedactQueryParams replaced by "REDACTED".
	// Empty when the error was built by the package-level ExtractError.
	URL string
}

// Error implements the standard error type.
func (e APIError) Error() string {
	var b strings.Builder
	if e.Response != nil {
		if e.URL != "" {
			b.WriteString(e.URL)
		} else if e.Response.Request != nil && e.Response.Request.URL != nil {
			b.WriteString(e.Response.Request.URL.String())
		}
		b.WriteByte(':')
		b.WriteByte(' ')

		b.WriteString(http.StatusText(e.Response.StatusCode))
		b.WriteByte(' ')
		b.WriteByte('(')
		b.WriteString(e.Response.Status)
		b.WriteByte(')')

		if len(e.Body) > 0 {
			b.WriteByte(':')
			b.WriteByte(' ')
			b.Write(e.Body)
		}
	}

	return b.String()
}

// ExtractError returns the response wrapped inside an APIError.
func ExtractError(resp *http.Response) APIError {
	body, _ := io.ReadAll(resp.Body)

	return APIError{
		Response: resp,
		Body:     body,
	}
}

// extractError is like ExtractError but renders the request URL through
// the Client's RedactQueryParams configuration.
func (c *Client) extractError(resp *http.Response) APIError {
	apiErr := ExtractError(resp)
	if resp.Request != nil {
		apiErr.URL = RedactURL(resp.Request.URL, c.redactQueryParams...)
	}

	return apiErr
}

// GetError reports whether the given "err" is, or wraps, an APIError.
func GetError(err error) (APIError, bool) {
	if err == nil {
		return APIError{}, false
	}

	var apiErr APIError
	if !errors.As(err, &apiErr) {
		return APIError{}, false
	}

	return apiErr, true
}

// DecodeError binds a json error to the "destPtr".
func DecodeError(err error, destPtr any) error {
	apiErr, ok := GetError(err)
	if !ok {
		return err
	}

	return json.Unmarshal(apiErr.Body, destPtr)
}

// GetErrorCode reads an error, which should be a type of APIError,
// and returns its status code.
// If the given "err" is nil or is not an APIError it returns 200,
// acting as we have no error.
func GetErrorCode(err error) int {
	apiErr, ok := GetError(err)
	if !ok {
		return http.StatusOK
	}

	return apiErr.Response.StatusCode
}
