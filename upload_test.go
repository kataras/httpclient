package httpclient

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploaderSendsFieldsAndFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("file contents"), 0o600); err != nil {
		t.Fatal(err)
	}

	var (
		gotField string
		gotFile  string
		gotName  string
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			return
		}

		gotField = r.FormValue("title")

		f, header, err := r.FormFile("attachment")
		if err != nil {
			t.Errorf("form file: %v", err)
			return
		}
		defer f.Close()

		gotName = header.Filename
		b, _ := io.ReadAll(f)
		gotFile = string(b)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"firstname":"Makis"}`))
	})

	client := New(BaseURL("http://example.local"), Handler(mux))

	uploader := client.NewUploader()
	if err := uploader.AddField("title", "a note"); err != nil {
		t.Fatal(err)
	}
	if err := uploader.AddFile("attachment", path); err != nil {
		t.Fatal(err)
	}

	resp, err := uploader.Upload(defaultCtx, http.MethodPost, "/upload")
	if err != nil {
		t.Fatal(err)
	}
	defer DrainResponseBody(resp)

	if gotField != "a note" {
		t.Fatalf("field: got %q", gotField)
	}
	if gotFile != "file contents" {
		t.Fatalf("file: got %q", gotFile)
	}
	if !strings.HasSuffix(gotName, "note.txt") {
		t.Fatalf("filename: got %q", gotName)
	}
}

func TestUploaderAddFileSourceReadsFromAnyReader(t *testing.T) {
	var got string

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			return
		}

		f, _, err := r.FormFile("data")
		if err != nil {
			t.Errorf("form file: %v", err)
			return
		}
		defer f.Close()

		b, _ := io.ReadAll(f)
		got = string(b)
		_, _ = w.Write([]byte("{}"))
	})

	client := New(BaseURL("http://example.local"), Handler(mux))

	uploader := client.NewUploader()
	if err := uploader.AddFileSource("data", "in-memory.txt", strings.NewReader("streamed")); err != nil {
		t.Fatal(err)
	}

	resp, err := uploader.Upload(defaultCtx, http.MethodPost, "/")
	if err != nil {
		t.Fatal(err)
	}
	defer DrainResponseBody(resp)

	if got != "streamed" {
		t.Fatalf("got %q", got)
	}
}

// TestUploaderRefusesASecondUpload: Upload closes the multipart writer, so a
// second call used to send a body with no closing boundary.
func TestUploaderRefusesASecondUpload(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{}"))
	})

	client := New(BaseURL("http://example.local"), Handler(mux))

	uploader := client.NewUploader()
	if err := uploader.AddField("k", "v"); err != nil {
		t.Fatal(err)
	}

	resp, err := uploader.Upload(defaultCtx, http.MethodPost, "/")
	if err != nil {
		t.Fatal(err)
	}
	DrainResponseBody(resp)

	if _, err = uploader.Upload(defaultCtx, http.MethodPost, "/"); !errors.Is(err, ErrUploaderClosed) {
		t.Fatalf("expected ErrUploaderClosed, got %v", err)
	}
}
