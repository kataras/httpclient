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

// JSONOptions sets the encoding/json/v2 options this Client uses both to encode
// request payloads and to decode response bodies.
//
// The default is encoding/json.DefaultOptionsV1(), which reproduces the
// behaviour of the original encoding/json package: case-insensitive field
// matching, duplicate object names allowed, lenient RFC 3339 parsing and so on.
// Pass your own options to tighten that up, for example:
//
//	httpclient.JSONOptions(json.RejectUnknownMembers(true))
//
// Options given here replace the default set, they are not merged with it.
// Join them yourself with json.JoinOptions if you want both. No options
// restores the default.
//
// To configure one direction only, see JSONMarshalOptions and
// JSONUnmarshalOptions.
func JSONOptions(opts ...json.Options) Option {
	return func(c *Client) {
		c.jsonMarshalOptions = joinOrDefault(opts)
		c.jsonUnmarshalOptions = joinOrDefault(opts)
	}
}

// JSONMarshalOptions sets the encoding/json/v2 options used to encode request
// payloads, leaving the decoding set alone. Options given replace the marshal
// default, none restores it.
//
// It exists for policies that differ by direction, such as
// jsontext.AllowInvalidUTF8(true) on the way out while decoding stays strict.
func JSONMarshalOptions(opts ...json.Options) Option {
	return func(c *Client) {
		c.jsonMarshalOptions = joinOrDefault(opts)
	}
}

// JSONUnmarshalOptions sets the encoding/json/v2 options used to decode response
// bodies through Client.BindJSON and Client.ReadJSON, leaving the encoding set
// alone. Options given replace the unmarshal default, none restores it.
func JSONUnmarshalOptions(opts ...json.Options) Option {
	return func(c *Client) {
		c.jsonUnmarshalOptions = joinOrDefault(opts)
	}
}

// defaultJSONOptions returns the v1-compatible option set.
func defaultJSONOptions() json.Options {
	return jsonv1.DefaultOptionsV1()
}

// joinOrDefault joins the given options, or returns the package default when
// there are none. Given options replace the default, they are not merged with it.
func joinOrDefault(opts []json.Options) json.Options {
	if len(opts) == 0 {
		return defaultJSONOptions()
	}

	return json.JoinOptions(opts...)
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
