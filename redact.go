package httpclient

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// redactedValue replaces the value of every query parameter listed in
// RedactQueryParams, and of every header listed in RedactHeaders, when a URL or
// a dump is rendered inside an error message or debug output.
const redactedValue = "REDACTED"

// defaultRedactedHeaders are scrubbed from Debug dumps without being asked for.
// Their whole value is a credential.
var defaultRedactedHeaders = []string{
	"Authorization",
	"Proxy-Authorization",
	"Cookie",
	"Set-Cookie",
}

// RedactQueryParams registers query parameter names (e.g. "apiKey", "token")
// whose values must never appear in APIError messages or in Debug output.
//
// The request itself is not modified; only rendered text is scrubbed.
//
// Request bodies are never scrubbed. Do not send a secret in a body and expect
// Debug to hide it.
func RedactQueryParams(keys ...string) Option {
	return func(c *Client) {
		c.redactQueryParams = append(c.redactQueryParams, keys...)
	}
}

// RedactHeaders registers extra request and response header names whose values
// are replaced by "REDACTED" in Debug output. Authorization,
// Proxy-Authorization, Cookie and Set-Cookie are always redacted.
func RedactHeaders(names ...string) Option {
	return func(c *Client) {
		for _, name := range names {
			c.redactHeaders = append(c.redactHeaders, http.CanonicalHeaderKey(name))
		}
	}
}

// RedactURL returns the string form of "u" with the values of the given
// query parameter names replaced by "REDACTED", and any userinfo password
// masked. The input is not modified.
func RedactURL(u *url.URL, keys ...string) string {
	if u == nil {
		return ""
	}

	if len(keys) == 0 {
		return u.Redacted()
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
		return u.Redacted()
	}

	copied := u.Clone()
	copied.RawQuery = q.Encode()
	return copied.Redacted()
}

// redactText scrubs a request or response dump.
//
// It works line by line over the head of the dump rather than replacing raw
// secret values everywhere: a secret of "1" or "dev" would otherwise rewrite
// unrelated text. The request line is rebuilt from the parsed URL and the value
// of every redacted header is replaced whole. The body is left untouched,
// because there is no general way to find a secret inside it.
func redactText(text string, req *http.Request, queryKeys, extraHeaders []string) string {
	headerNames := slices.Concat(defaultRedactedHeaders, extraHeaders)

	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		if trimmed == "" {
			break // end of the head, the body starts here.
		}

		if i == 0 {
			lines[i] = redactRequestLine(line, req, queryKeys)
			continue
		}

		lines[i] = redactHeaderLine(line, headerNames)
	}

	return strings.Join(lines, "\n")
}

// redactRequestLine rewrites "GET /path?apiKey=secret HTTP/1.1" through the
// parsed URL, so the value is replaced and nothing else on the line is.
func redactRequestLine(line string, req *http.Request, queryKeys []string) string {
	if req == nil || req.URL == nil || len(queryKeys) == 0 {
		return line
	}

	method, rest, ok := strings.Cut(strings.TrimRight(line, "\r"), " ")
	if !ok {
		return line
	}

	target, proto, ok := strings.Cut(rest, " ")
	if !ok {
		return line
	}

	parsed, err := url.Parse(target)
	if err != nil {
		return line
	}

	q := parsed.Query()
	changed := false
	for _, key := range queryKeys {
		if values, found := q[key]; found {
			for i := range values {
				values[i] = redactedValue
			}
			changed = true
		}
	}

	if !changed {
		return line
	}

	parsed.RawQuery = q.Encode()

	suffix := ""
	if strings.HasSuffix(line, "\r") {
		suffix = "\r"
	}

	return method + " " + parsed.String() + " " + proto + suffix
}

// redactHeaderLine replaces the whole value of a header whose name is redacted.
func redactHeaderLine(line string, names []string) string {
	name, _, ok := strings.Cut(line, ":")
	if !ok {
		return line
	}

	canonical := http.CanonicalHeaderKey(strings.TrimSpace(name))
	if !slices.Contains(names, canonical) {
		return line
	}

	suffix := ""
	if strings.HasSuffix(line, "\r") {
		suffix = "\r"
	}

	return name + ": " + redactedValue + suffix
}
