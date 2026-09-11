package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	json "encoding/json/v2"
)

// PlainText lists the Go types Client.BindPlain can produce from a
// plain text response body.
type PlainText interface {
	~string | ~[]byte | ~int | ~int64 | ~float64
}

// BindJSON sends the request and decodes the JSON response body into a new T.
//
// It is the generic form of Client.ReadJSON: there is no destination pointer to
// get wrong, and the zero T comes back alongside any error.
//
//	weather, err := c.BindJSON[Weather](ctx, http.MethodGet, "/current.json", nil)
//
// A response status >= 400 returns an APIError. An empty response body returns
// io.EOF, which IsErrEmptyJSON also recognises. The response body is closed
// before this method returns.
func (c *Client) BindJSON[T any](ctx context.Context, method, urlpath string, payload any, opts ...RequestOption) (T, error) {
	var value T

	if payload != nil {
		opts = withDefaultRequestOption(opts, RequestHeader(true, contentTypeKey, contentTypeJSON))
	}

	resp, err := c.Do(ctx, method, urlpath, payload, opts...)
	if err != nil {
		return value, err
	}
	defer c.DrainResponseBody(resp)

	if resp.StatusCode >= http.StatusBadRequest {
		return value, c.extractError(resp)
	}

	if err = decodeJSON(resp.Body, &value, c.jsonUnmarshalOptions); err != nil {
		var zero T
		return zero, err
	}

	return value, nil
}

// BindPlain sends the request and reads the response body as plain text into a new T.
//
//	token, err := c.BindPlain[string](ctx, http.MethodGet, "/token", nil)
//	count, err := c.BindPlain[int](ctx, http.MethodGet, "/count", nil)
//
// A response status >= 400 returns an APIError. The response body is closed
// before this method returns.
func (c *Client) BindPlain[T PlainText](ctx context.Context, method, urlpath string, payload any, opts ...RequestOption) (T, error) {
	var value T

	resp, err := c.Do(ctx, method, urlpath, payload, opts...)
	if err != nil {
		return value, err
	}
	defer c.DrainResponseBody(resp)

	if resp.StatusCode >= http.StatusBadRequest {
		return value, c.extractError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return value, err
	}

	return parsePlainText[T](body)
}

// parsePlainText converts a raw body to T.
func parsePlainText[T PlainText](body []byte) (T, error) {
	var value T

	switch ptr := any(&value).(type) {
	case *string:
		*ptr = string(body)
	case *[]byte:
		*ptr = body
	case *int:
		n, err := strconv.Atoi(text(body))
		if err != nil {
			return value, err
		}
		*ptr = n
	case *int64:
		n, err := strconv.ParseInt(text(body), 10, 64)
		if err != nil {
			return value, err
		}
		*ptr = n
	case *float64:
		f, err := strconv.ParseFloat(text(body), 64)
		if err != nil {
			return value, err
		}
		*ptr = f
	default:
		// A named type whose underlying type is one of the above,
		// e.g. "type Token string".
		return parseNamedPlainText[T](body)
	}

	return value, nil
}

// parseNamedPlainText handles defined types such as "type Token string",
// whose pointer does not match the plain type switch above.
func parseNamedPlainText[T PlainText](body []byte) (T, error) {
	var value T
	v := reflect.ValueOf(&value).Elem()

	switch v.Kind() {
	case reflect.String:
		v.SetString(string(body))
	case reflect.Slice:
		v.SetBytes(body)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(text(body), 10, 64)
		if err != nil {
			return value, err
		}
		v.SetInt(n)
	case reflect.Float64:
		f, err := strconv.ParseFloat(text(body), 64)
		if err != nil {
			return value, err
		}
		v.SetFloat(f)
	default:
		return value, fmt.Errorf("httpclient: unsupported plain text type: %T", value)
	}

	return value, nil
}

// text trims the surrounding whitespace a server may add around a bare number.
func text(body []byte) string {
	return strings.TrimSpace(string(body))
}

// Bind decodes an already-received response into a new T, choosing the decoder
// from the response's Content-Type header. Closing the response body is up to
// the caller.
//
// The optional "opts" are the encoding/json/v2 options for a JSON body. They
// replace the package default of encoding/json.DefaultOptionsV1(), they are
// not merged with it.
//
// It is the generic form of BindResponse.
func Bind[T any](resp *http.Response, opts ...json.Options) (T, error) {
	var value T

	if err := bindResponse(resp, &value, joinOrDefault(opts)); err != nil {
		var zero T
		return zero, err
	}

	return value, nil
}

// BindError decodes the JSON body carried by the APIError inside "err" into a new T.
//
//	problem, decodeErr := httpclient.BindError[ProblemDetails](err)
//
// It returns the original "err" untouched when "err" does not carry an APIError,
// so a caller can tell "not an API error" apart from "an API error I could not decode".
//
// The optional "opts" are the encoding/json/v2 options for the body. They
// replace the package default of encoding/json.DefaultOptionsV1().
func BindError[T any](err error, opts ...json.Options) (T, error) {
	var value T

	apiErr, ok := GetError(err)
	if !ok {
		return value, err
	}

	if decodeErr := decodeJSON(bytes.NewReader(apiErr.Body), &value, joinOrDefault(opts)); decodeErr != nil {
		var zero T
		return zero, decodeErr
	}

	return value, nil
}
