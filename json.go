package httpclient

import (
	"bufio"
	"errors"
	"io"
	"strings"

	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
)

// jsonOptions is the encoding/json/v2 option set carried by a Client.
type jsonOptions = json.Options

// JSONOptions sets the encoding/json/v2 options this Client uses to encode
// request payloads and decode response bodies.
//
// The default is encoding/json.DefaultOptionsV1(), which reproduces the
// behaviour of the original encoding/json package: case-insensitive field
// matching, duplicate object names allowed, lenient RFC 3339 parsing and so on.
// Pass your own options to tighten that up, for example:
//
//	httpclient.JSONOptions(json.RejectUnknownMembers(true))
//
// Options given here replace the default set, they are not merged with it.
// Join them yourself with json.JoinOptions if you want both.
func JSONOptions(opts ...json.Options) Option {
	return func(c *Client) {
		if len(opts) == 0 {
			c.jsonOptions = defaultJSONOptions()
			return
		}

		c.jsonOptions = json.JoinOptions(opts...)
	}
}

// defaultJSONOptions returns the v1-compatible option set.
func defaultJSONOptions() json.Options {
	return jsonv1.DefaultOptionsV1()
}

// encodeJSON writes "v" to "w" as JSON.
func encodeJSON(w io.Writer, v any, opts json.Options) error {
	return json.MarshalWrite(w, v, opts)
}

// decodeJSON reads a single JSON value from "r" into "dest".
//
// An input that is empty or holds nothing but whitespace returns io.EOF, the
// same as the original encoding/json Decoder.Decode did. Callers rely on that:
// an empty response body is a normal, successful outcome for several APIs and
// is recognised through errors.Is(err, io.EOF) or IsErrEmptyJSON.
func decodeJSON(r io.Reader, dest any, opts json.Options) error {
	br := bufio.NewReader(r)

	for {
		b, err := br.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return io.EOF // empty body.
			}
			return err
		}

		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			continue // leading whitespace.
		}

		if err = br.UnreadByte(); err != nil {
			return err
		}
		break
	}

	return json.UnmarshalRead(br, dest, opts)
}

// IsErrEmptyJSON reports whether the given "err" is caused by a
// Client.ReadJSON or Client.BindJSON call when the response body was empty or
// didn't start with { or [.
//
// It is a variable so that it can be replaced when a third-party JSON package
// reports empty input differently.
var IsErrEmptyJSON = func(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	// encoding/json/v2 reports a truncated or absent value syntactically.
	var syntacticErr *jsontext.SyntacticError
	if errors.As(err, &syntacticErr) {
		return syntacticErr.ByteOffset == 0 || errors.Is(syntacticErr.Err, io.ErrUnexpectedEOF)
	}

	// encoding/json v1 error, still reachable through a custom decoder.
	var syntaxErr *jsonv1.SyntaxError
	if errors.As(err, &syntaxErr) {
		return syntaxErr.Offset == 0 && syntaxErr.Error() == "unexpected end of JSON input"
	}

	// 3rd party packages.
	errMsg := err.Error()
	return strings.Contains(errMsg, "readObjectStart: expect {") || strings.Contains(errMsg, "readArrayStart: expect [")
}
