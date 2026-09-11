package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
)

// Uploader holds the necessary information for upload requests.
//
// Look the Client.NewUploader method.
type Uploader struct {
	client *Client

	body     *bytes.Buffer
	uploaded bool
	Writer   *multipart.Writer
}

// ErrUploaderClosed is returned when an Uploader is used for a second upload.
// Upload closes the multipart writer, so the collected body cannot be extended
// or resent. Build a new Uploader per request.
var ErrUploaderClosed = errors.New("httpclient: uploader already used, build a new one")

// NewUploader returns a structure which is responsible for sending
// file and form data to the server.
func (c *Client) NewUploader() *Uploader {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	return &Uploader{
		client: c,
		body:   body,
		Writer: writer,
	}
}

// AddField adds a form field to the uploader with the given key.
func (u *Uploader) AddField(key, value string) error {
	f, err := u.Writer.CreateFormField(key)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, strings.NewReader(value))
	return err
}

// AddFileSource adds a form file to the uploader with the given key.
func (u *Uploader) AddFileSource(key, filename string, source io.Reader) error {
	f, err := u.Writer.CreateFormFile(key, filename)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, source)
	return err
}

// AddFile adds a local form file to the uploader with the given key.
func (u *Uploader) AddFile(key, filename string) error {
	source, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer source.Close()

	return u.AddFileSource(key, filename, source)
}

// Upload sends the collected form and file data to the server.
//
// Closing the returned response body is up to the caller,
// see Client.DrainResponseBody.
func (u *Uploader) Upload(ctx context.Context, method, urlpath string, opts ...RequestOption) (*http.Response, error) {
	if u.uploaded {
		return nil, ErrUploaderClosed
	}

	if err := u.Writer.Close(); err != nil {
		return nil, err
	}
	u.uploaded = true

	payload := bytes.NewReader(u.body.Bytes())

	return u.client.Do(ctx, method, urlpath, payload,
		withDefaultRequestOption(opts, RequestHeader(true, contentTypeKey, u.Writer.FormDataContentType()))...)
}
