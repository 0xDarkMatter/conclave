package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const geminiTestKey = "AIzaTESTKEY-must-never-print"

// TestGeminiAPIKeyTravelsInAHeaderNotTheURL defends the key against the error
// path. It used to ride in the query string (?key=...), and Go's *url.Error
// prints the full request URL, so any DNS, TLS, refused-connection or timeout
// failure put the key into Response.Error, stderr and the --json output that
// callers store. The key now goes in the x-goog-api-key header.
func TestGeminiAPIKeyTravelsInAHeaderNotTheURL(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", geminiTestKey)
	t.Setenv("GOOGLE_API_KEY", "")

	var gotQueryKey, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQueryKey = r.URL.Query().Get("key")
		gotHeader = r.Header.Get("x-goog-api-key")
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"OK"}]}}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1,"totalTokenCount":4}}`))
	}))
	defer srv.Close()

	p := NewGeminiAPIProvider()
	p.baseURL = srv.URL
	out, _, _, err := p.Query(context.Background(), "hi", "gemini-test")
	if err != nil || out != "OK" {
		t.Fatalf("query failed: %q %v", out, err)
	}
	if gotQueryKey != "" {
		t.Fatalf("API key was sent in the URL query string")
	}
	if gotHeader != geminiTestKey {
		t.Fatalf("x-goog-api-key header = %q, want the configured key", gotHeader)
	}
}

func TestGeminiAPIKeyAbsentFromTransportErrors(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", geminiTestKey)
	t.Setenv("GOOGLE_API_KEY", "")

	p := NewGeminiAPIProvider()
	p.baseURL = "http://127.0.0.1:1" // nothing listens: connection refused
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, _, err := p.Query(ctx, "hi", "gemini-test")
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), geminiTestKey) {
		t.Fatalf("API key leaked into the error text: %q", err)
	}
}
