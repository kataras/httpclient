package httpclient

import (
	"net/http"
	"net/url"
	"strings"
)

// redactedValue replaces the value of every query parameter listed in
// RedactQueryParams when a URL is rendered inside an error message or a debug dump.
const redactedValue = "REDACTED"

// RedactQueryParams registers query parameter names (e.g. "apiKey", "token")
// whose values must never appear in APIError messages or in Debug output.
//
// The request itself is not modified; only rendered text is scrubbed.
func RedactQueryParams(keys ...string) Option {
	return func(c *Client) {
		c.redactQueryParams = append(c.redactQueryParams, keys...)
	}
}

// RedactURL returns the string form of "u" with the values of the given
// query parameter names replaced by "REDACTED". The input is not modified.
// With no keys it is equivalent to u.String().
func RedactURL(u *url.URL, keys ...string) string {
	if u == nil {
		return ""
	}

	if len(keys) == 0 {
		return u.String()
	}

	q := u.Query()
	changed := false
	for _, key := range keys {
		if values, ok := q[key]; ok {
			for i := range values {
				values[i] = redactedValue
			}
			changed = true
		}
	}

	if !changed {
		return u.String()
	}

	copied := *u
	copied.RawQuery = q.Encode()
	return copied.String()
}

// redactText scrubs every value of the listed query parameters of "req" from "text",
// in both their raw and query-escaped forms. Used on request/response dumps,
// where the URL appears percent-encoded in the request line.
func redactText(text string, req *http.Request, keys []string) string {
	if req == nil || req.URL == nil || len(keys) == 0 {
		return text
	}

	q := req.URL.Query()
	for _, key := range keys {
		for _, value := range q[key] {
			if value == "" {
				continue
			}

			text = strings.ReplaceAll(text, value, redactedValue)
			if escaped := url.QueryEscape(value); escaped != value {
				text = strings.ReplaceAll(text, escaped, redactedValue)
			}
		}
	}

	return text
}
