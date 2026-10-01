package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAnthropicOverloaded529IsRetried defends against failing a call on a
// load spike that one backoff would ride out. Anthropic answers "temporarily
// overloaded" with HTTP 529 overloaded_error (platform.claude.com/docs/en/api/
// errors, checked 2026-10-01). isRetryable listed 429, 500, 502, 503 and 504,
// so doRequest returned the 529 on the first attempt and a single `-g claude`
// query failed outright.
func TestAnthropicOverloaded529IsRetried(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(529)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"},"request_id":"req_011CSHoEeqs5C35K2UUqR7Fy"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test")
	p := NewAnthropicAPIProvider()
	p.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, _, _, err := p.Query(ctx, "Reply with exactly: OK", "claude-opus-5-5")
	if err != nil {
		t.Fatalf("a single 529 should be retried, not returned: %v", err)
	}
	if out != "ok" {
		t.Errorf("response = %q, want %q", out, "ok")
	}
	if callCount != 2 {
		t.Errorf("got %d calls, want exactly 2 (the 529, then the retry)", callCount)
	}
}

// TestRetryableIsTheStatusClassNotAList pins the rule isRetryable switched to
// when 529 slipped through its list: every 5xx retries, and among 4xx only
// 429 does. Narrowing back to a list drops the vendor-specific 5xx (529,
// Cloudflare's 520-524); widening into 4xx retries requests that cannot
// succeed, which TestDoRequest_NoRetryOn400 also guards end to end.
func TestRetryableIsTheStatusClassNotAList(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{400, false},
		{401, false},
		{402, false},
		{404, false},
		{413, false},
		{429, true},
		{499, false},
		{500, true},
		{503, true},
		{504, true},
		{520, true},
		{524, true},
		{529, true},
		{599, true},
		{600, false},
	}
	for _, tc := range cases {
		if got := isRetryable(tc.status); got != tc.want {
			t.Errorf("isRetryable(%d) = %v, want %v", tc.status, got, tc.want)
		}
	}
}
