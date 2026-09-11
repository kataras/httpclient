package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
)

// ReadJSON binds "dest" to the response's body.
// After this call, the response body reader is closed.
//
// "dest" must be a non-nil pointer, or nil to discard the body.
// Prefer Client.BindJSON, which cannot be given the wrong kind of value.
//
// If the response status code is >= 400 then it returns an APIError.
// If the response body is expected empty sometimes, you can omit the error
// through IsErrEmptyJSON or errors.Is(err, io.EOF).
func (c *Client) ReadJSON(ctx context.Context, dest any, method, urlpath string, payload any, opts ...RequestOption) error {
	if dest != nil {
		if err := checkDestination(dest); err != nil {
			return err
		}
	}

	if payload != nil {
		opts = withDefaultRequestOption(opts, RequestHeader(true, contentTypeKey, contentTypeJSON))
	}

	resp, err := c.Do(ctx, method, urlpath, payload, opts...)
	if err != nil {
		return err
	}
	defer c.DrainResponseBody(resp)

	if resp.StatusCode >= http.StatusBadRequest {
		return c.extractError(resp)
	}

	if dest != nil {
		return decodeJSON(resp.Body, dest, c.jsonOptions)
	}

	return nil
}

// checkDestination reports an error when "dest" cannot receive a decoded value.
// Without it a non-pointer silently decodes into a throwaway copy.
func checkDestination(dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Pointer {
		return fmt.Errorf("httpclient: destination must be a non-nil pointer, got %T", dest)
	}
	if v.IsNil() {
		return fmt.Errorf("httpclient: destination is a nil %T", dest)
	}
	return nil
}

// ReadPlain is like ReadJSON but it accepts a pointer to a string, byte slice
// or integer and it reads the body as plain text.
//
// Prefer Client.BindPlain.
func (c *Client) ReadPlain(ctx context.Context, dest any, method, urlpath string, payload any, opts ...RequestOption) error {
	resp, err := c.Do(ctx, method, urlpath, payload, opts...)
	if err != nil {
		return err
	}
	defer c.DrainResponseBody(resp)

	if resp.StatusCode >= http.StatusBadRequest {
		return c.extractError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	switch ptr := dest.(type) {
	case *[]byte:
		*ptr = body
		return nil
	case *string:
		*ptr = string(body)
		return nil
	case *int:
		*ptr, err = strconv.Atoi(string(body))
		return err
	default:
		return fmt.Errorf("unsupported response body type: %T", ptr)
	}
}

// GetPlainUnquote reads the response body as raw text and tries to unquote it,
// useful when the remote server sends a single key as a value but due to a backend
// mistake it sends it as JSON (quoted) instead of plain text.
func (c *Client) GetPlainUnquote(ctx context.Context, method, urlpath string, payload any, opts ...RequestOption) (string, error) {
	var bodyStr string
	if err := c.ReadPlain(ctx, &bodyStr, method, urlpath, payload, opts...); err != nil {
		return "", err
	}

	s, err := strconv.Unquote(bodyStr)
	if err == nil {
		bodyStr = s
	}

	return bodyStr, nil
}

// WriteTo reads the response and then copies its data to the "dest" writer.
// If the "dest" is a type of HTTP response writer then it writes the
// content-type and content-length of the original request.
//
// Returns the amount of bytes written to "dest".
func (c *Client) WriteTo(ctx context.Context, dest io.Writer, method, urlpath string, payload any, opts ...RequestOption) (int64, error) {
	if payload != nil {
		opts = withDefaultRequestOption(opts, RequestHeader(true, contentTypeKey, contentTypeJSON))
	}

	resp, err := c.Do(ctx, method, urlpath, payload, opts...)
	if err != nil {
		return 0, err
	}
	// Drain, not just close: an early copy error would otherwise leave the
	// connection unusable for the next request.
	defer c.DrainResponseBody(resp)

	if w, ok := dest.(http.ResponseWriter); ok {
		// Copy the content type and content-length.
		w.Header().Set(contentTypeKey, resp.Header.Get(contentTypeKey))
		if resp.ContentLength > 0 {
			w.Header().Set(contentLengthKey, strconv.FormatInt(resp.ContentLength, 10))
		}
	}

	return io.Copy(dest, resp.Body)
}

// BindResponse consumes the response's body and binds the result to the "dest" pointer,
// closing the response's body is up to the caller.
//
// Deprecated: use the generic Bind instead, which cannot be handed a value of
// the wrong kind.
func BindResponse(resp *http.Response, dest any) error {
	if err := checkDestination(dest); err != nil {
		return err
	}

	return bindResponse(resp, dest, defaultJSONOptions())
}

// bindResponse decodes "resp" into "dest" according to the response content type.
// It is strict in order to catch bad actors fast, e.g. it won't try to read plain
// text if that isn't what the response headers say.
func bindResponse(resp *http.Response, dest any, opts jsonOptions) error {
	contentType := trimHeader(resp.Header.Get(contentTypeKey))
	switch contentType {
	case contentTypeJSON: // the most common scenario on successful responses.
		return decodeJSON(resp.Body, dest, opts)
	case contentTypePlainText:
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		switch v := dest.(type) {
		case *string:
			*v = string(b)
		case *[]byte:
			*v = b
		default:
			return errors.New("plain text response should accept a *string or a *[]byte")
		}

		return nil
	default:
		acceptContentType := ""
		if resp.Request != nil {
			acceptContentType = trimHeader(resp.Request.Header.Get(acceptKey))
		}

		if acceptContentType == contentType {
			// Here we make a special case, if the content type
			// was explicitly set by the request but we cannot handle it.
			return fmt.Errorf("current implementation can not handle the received (and accepted) mime type: %s", contentType)
		}

		return fmt.Errorf("unexpected mime type received: %s", contentType)
	}
}

func trimHeader(v string) string {
	for i, char := range v {
		if char == ' ' || char == ';' {
			return v[:i]
		}
	}
	return v
}
