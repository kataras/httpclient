package httpclient

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
)

// See the "Handler" client option.
type handlerTransport struct {
	handler http.Handler
}

// RoundTrip completes the http.RoundTripper interface.
// It can be used to test calls to a server's handler.
func (t *handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// A deep copy: the handler is free to write to r.Header, and a shallow
	// struct copy would share that map with the caller's request.
	reqCopy := req.Clone(req.Context())

	if req.Body != nil {
		// RoundTrip owns the request body and must close it.
		defer req.Body.Close()
	}

	if reqCopy.Proto == "" {
		reqCopy.Proto = "HTTP/1.1"
		reqCopy.ProtoMajor, reqCopy.ProtoMinor = 1, 1
	}

	if reqCopy.Body == nil {
		reqCopy.Body = io.NopCloser(bytes.NewReader(nil))
	} else if reqCopy.ContentLength == -1 {
		reqCopy.TransferEncoding = []string{"chunked"}
	}

	if reqCopy.RequestURI == "" {
		reqCopy.RequestURI = reqCopy.URL.RequestURI()
	}

	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, reqCopy)

	// Result fills in Status ("404 Not Found"), Proto and a non-nil Body,
	// which a hand-built response would leave wrong or empty.
	resp := recorder.Result()
	resp.Request = reqCopy

	if resp.ContentLength < 0 && recorder.Body != nil {
		if _, declared := resp.Header[contentLengthKey]; !declared {
			resp.ContentLength = int64(recorder.Body.Len())
			resp.Header.Set(contentLengthKey, strconv.Itoa(recorder.Body.Len()))
		}
	}

	if recorder.Flushed {
		resp.TransferEncoding = []string{"chunked"}
	}

	return resp, nil
}
