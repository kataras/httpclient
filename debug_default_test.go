package httpclient

import (
	"bytes"
	"log"
	"net/http"
	"strings"
	"testing"
)

// TestDebugNilLoggerUsesTheDefault: Debug(nil) used to panic on the first
// request; it now prints through the standard log package.
func TestDebugNilLoggerUsesTheDefault(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`"pong"`))
	})

	c := New(Handler(mux), Debug(nil))
	if _, err := c.BindJSON[string](defaultCtx, http.MethodGet, "http://in.memory/ping", nil); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "GET /ping") || !strings.Contains(out, "pong") {
		t.Fatalf("expected the request and response dumps in the default logger output, got:\n%s", out)
	}
}
