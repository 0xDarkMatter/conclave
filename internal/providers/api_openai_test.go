package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestDoRequest_TransportErrorTagged verifies that connection-level failures
// are distinguishable in the error message from API-level failures. The user
// originally saw "execute request: Post https://api.op..." with no clue
// whether it was transport or 4xx; the new "transport error" prefix is the
// signal that downstream rendering surfaces.
func TestDoRequest_TransportErrorTagged(t *testing.T) {
	// Spin up then immediately close a server to guarantee a connection failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	t.Setenv("OPENAI_API_KEY", "test")
	p := NewOpenAIAPIProvider()
	p.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, _, err := p.Query(ctx, "x", "gpt-4o")
	if err == nil {
		t.Fatal("expected transport error")
	}
	if !strings.Contains(err.Error(), "transport error") {
		t.Errorf("transport failure should be tagged 'transport error'; got: %s", err.Error())
	}
}

// TestDoRequest_NoRetryOn400 verifies that 400 errors (auth, bad params) do
// not trigger retries — retrying a 400 won't fix it and wastes the user's time.
// Regression test for the user's report that --retries felt broken; the real
// reason was that single-call already retries 429/5xx but correctly never
// retries 4xx-non-429.
func TestDoRequest_NoRetryOn400(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test")
	p := NewOpenAIAPIProvider()
	p.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, _, err := p.Query(ctx, "x", "gpt-4o")
	if err == nil {
		t.Fatal("expected error")
	}
	if callCount != 1 {
		t.Errorf("400 should not retry; got %d calls, want 1", callCount)
	}
}

// TestDoRequest_RetriesOn429 verifies retry behavior IS active for transient
// errors in single-call mode (the README claims this; let's verify it).
func TestDoRequest_RetriesOn429(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test")
	p := NewOpenAIAPIProvider()
	p.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, _, _, err := p.Query(ctx, "x", "gpt-4o")
	if err != nil {
		t.Fatalf("expected eventual success after retries: %v", err)
	}
	if out != "ok" {
		t.Errorf("response = %q, want %q", out, "ok")
	}
	if callCount < 3 {
		t.Errorf("expected at least 3 calls (2 retries + success); got %d", callCount)
	}
}

// TestOutOfCredit429IsNotRetried defends against retrying a 429 that is a
// billing state, not a rate limit. On 2026-10-01 an out-of-credit OpenAI key
// answered every completion with this exact body; doRequest retried it three
// times (~7 s of backoff, multiplied per item in batch mode) before returning
// the same message with "(after 3 retries)". No amount of waiting adds credit.
func TestOutOfCredit429IsNotRetried(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"message":"You have no credits remaining. Add credits to continue using the API at https://platform.openai.com/settings/organization/billing/.","code":"credit_balance_exhausted"}}`))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test")
	p := NewOpenAIAPIProvider()
	p.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _, _, err := p.Query(ctx, "Reply with exactly: OK", "gpt-6-luna")
	if err == nil {
		t.Fatal("expected the billing error")
	}
	if callCount != 1 {
		t.Errorf("an out-of-credit 429 was sent %d times, want exactly 1", callCount)
	}
	for _, want := range []string{"HTTP 429", "You have no credits remaining", "credit_balance_exhausted"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should carry the vendor's message (%q); got: %s", want, err)
		}
	}
	if strings.Contains(err.Error(), "retries") {
		t.Errorf("a billing failure was retried; got: %s", err)
	}
	// Batch's rate limiter relies on the type, not the text, to skip it.
	if !IsBillingError(err) {
		t.Errorf("error is not a *BillingError, so batch would count it as a rate limit: %T", err)
	}
}

// TestBillingIsToldApartFromRateLimits pins the per-vendor classification.
// Too narrow, and an out-of-credit key burns three backoffs per call; too
// broad, and a per-minute rate limit that one backoff would have absorbed
// fails a panel member outright. Gemini's RESOURCE_EXHAUSTED is the trap: it
// mentions quota and billing but is mostly a per-minute limit.
func TestBillingIsToldApartFromRateLimits(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		billing bool
	}{
		{"openai credit exhausted", 429, `{"error":{"message":"You have no credits remaining.","code":"credit_balance_exhausted"}}`, true},
		{"openai insufficient quota", 429, `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota","param":null,"code":"insufficient_quota"}}`, true},
		{"anthropic billing_error", 402, `{"type":"error","error":{"type":"billing_error","message":"Billing issue"}}`, true},
		{"openrouter 402 with a numeric code", 402, `{"error":{"message":"Insufficient credits","code":402}}`, true},
		{"openai rate limit", 429, `{"error":{"message":"Rate limit reached for requests","type":"requests","param":null,"code":"rate_limit_exceeded"}}`, false},
		{"gemini resource exhausted", 429, `{"error":{"code":429,"message":"You exceeded your current quota, please check your plan and billing details.","status":"RESOURCE_EXHAUSTED"}}`, false},
		{"bare 429", 429, ``, false},
		{"plain-text 429", 429, `Too Many Requests`, false},
	}
	for _, tc := range cases {
		if _, got := billingCode(tc.status, []byte(tc.body)); got != tc.billing {
			t.Errorf("%s: billing = %v, want %v", tc.name, got, tc.billing)
		}
	}
}

// TestParseAPIError_OpenAIMaxTokensRejection verifies that the actual
// OpenAI error body for the gpt-5.x max_tokens bug surfaces param/code in
// the returned error — this is what made the original report hard to debug.
func TestParseAPIError_OpenAIMaxTokensRejection(t *testing.T) {
	body := []byte(`{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead.","type":"invalid_request_error","param":"max_tokens","code":"unsupported_parameter"}}`)
	err := parseAPIError(400, body)
	msg := err.Error()
	for _, want := range []string{"HTTP 400", "max_tokens", "unsupported_parameter", "max_completion_tokens"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q; got: %s", want, msg)
		}
	}
}

func TestParseAPIError_RawBodyOn401(t *testing.T) {
	// Unparseable body — falls through to switch with raw body included.
	err := parseAPIError(401, []byte("Invalid Bearer token: abc..."))
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 401") {
		t.Errorf("missing status code; got: %s", msg)
	}
	if !strings.Contains(msg, "Invalid Bearer") {
		t.Errorf("raw body should be surfaced on 401; got: %s", msg)
	}
}

// TestOpenAIProvider_QueryGPT5SendsCompletionTokens spins up an httptest
// server that captures the request body and asserts the wire format for a
// gpt-5.x model includes max_completion_tokens, not max_tokens.
func TestOpenAIProvider_QueryGPT5SendsCompletionTokens(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")
	p := NewOpenAIAPIProvider()
	p.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, _, _, err := p.Query(ctx, "hello", "gpt-5.5")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if out != "ok" {
		t.Errorf("response = %q, want %q", out, "ok")
	}

	var req map[string]any
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("decode captured body: %v", err)
	}

	if _, ok := req["max_tokens"]; ok {
		t.Errorf("max_tokens leaked into gpt-5.x request: %s", capturedBody)
	}
	v, ok := req["max_completion_tokens"]
	if !ok {
		t.Fatalf("max_completion_tokens missing from gpt-5.x request: %s", capturedBody)
	}
	if v.(float64) != defaultGPT5MaxCompletionTokens {
		t.Errorf("max_completion_tokens = %v, want %d", v, defaultGPT5MaxCompletionTokens)
	}
}

// TestOpenAIProvider_QueryLegacyOmitsCompletionTokens ensures gpt-4 calls
// still match the pre-fix wire format (no max_completion_tokens).
func TestOpenAIProvider_QueryLegacyOmitsCompletionTokens(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")
	p := NewOpenAIAPIProvider()
	p.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, _, err := p.Query(ctx, "hello", "gpt-4o"); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(capturedBody), "max_completion_tokens") {
		t.Errorf("legacy model should not send max_completion_tokens: %s", capturedBody)
	}
	if strings.Contains(string(capturedBody), "max_tokens") {
		t.Errorf("legacy model should not send max_tokens (current default): %s", capturedBody)
	}
}

// TestOpenAIProvider_QueryGPT5BudgetOverride confirms the env override path.
func TestOpenAIProvider_QueryGPT5BudgetOverride(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"x"}}]}`))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("CONCLAVE_OPENAI_MAX_COMPLETION_TOKENS", "32000")
	p := NewOpenAIAPIProvider()
	p.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, _, err := p.Query(ctx, "hi", "gpt-5.5"); err != nil {
		t.Fatal(err)
	}

	var req map[string]any
	_ = json.Unmarshal(capturedBody, &req)
	if v := req["max_completion_tokens"]; v != float64(32000) {
		t.Errorf("override not applied: got %v, want 32000", v)
	}
}

// TestOpenAIProvider_Query400Surfaced verifies that a 400 from the real
// OpenAI error shape surfaces param + code, regression for the user's bug
// report not being able to debug the original failure.
func TestOpenAIProvider_Query400Surfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"Unsupported parameter: 'max_tokens'","type":"invalid_request_error","param":"max_tokens","code":"unsupported_parameter"}}`))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")
	p := NewOpenAIAPIProvider()
	p.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, _, err := p.Query(ctx, "hi", "gpt-5.5")
	if err == nil {
		t.Fatal("expected error from 400 response")
	}
	msg := err.Error()
	for _, want := range []string{"HTTP 400", "max_tokens", "unsupported_parameter"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q in user-facing message; got: %s", want, msg)
		}
	}
}

func TestParseAPIError_NoStatusCodeStripping(t *testing.T) {
	// Ensure every code path includes the numeric status.
	for _, code := range []int{400, 401, 403, 429, 500, 502, 503, 504, 418} {
		err := parseAPIError(code, []byte(`{"weird":"body"}`))
		if !strings.Contains(err.Error(), "HTTP") {
			t.Errorf("status %d: missing 'HTTP' prefix; got: %s", code, err.Error())
		}
	}
}

// TestReasoningModelDetection: the family test was a literal "gpt-5" prefix,
// so the GPT-6 line (gpt-6-sol, gpt-6.1-sol, gpt-6-luna, gpt-6-astra; shipped
// 2026-09) and o4-mini were sent max_tokens and rejected with HTTP 400
// unsupported_parameter, the exact bug the gpt-5 fix closed.
func TestReasoningModelDetection(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"gpt-5.2", true},
		{"gpt-5.5", true},
		{"gpt-5-nano", true},
		{"GPT-5.2", true},
		{"gpt-6-sol", true},
		{"gpt-6.1-sol", true},
		{"gpt-6-luna", true},
		{"gpt-6-astra", true},
		{"gpt-10", true},
		{"o1-preview", true},
		{"o3-mini", true},
		{"o4-mini", true},
		{"gpt-4o", false},
		{"gpt-4-turbo", false},
		{"gpt-4.1", false},
		{"gpt-oss-120b", false},
		{"gpt-chat-latest", false},
		{"omni-moderation-latest", false},
		{"", false},
		{"claude-opus-4-5", false},
	}
	for _, tc := range cases {
		if got := isReasoningModel(tc.model); got != tc.want {
			t.Errorf("isReasoningModel(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestGPT5BudgetFromEnv(t *testing.T) {
	const key = "CONCLAVE_OPENAI_MAX_COMPLETION_TOKENS"
	t.Cleanup(func() { os.Unsetenv(key) })

	os.Unsetenv(key)
	if got := gpt5BudgetFromEnv(16000); got != 16000 {
		t.Errorf("default: got %d, want 16000", got)
	}

	os.Setenv(key, "8000")
	if got := gpt5BudgetFromEnv(16000); got != 8000 {
		t.Errorf("override: got %d, want 8000", got)
	}

	os.Setenv(key, "garbage")
	if got := gpt5BudgetFromEnv(16000); got != 16000 {
		t.Errorf("invalid value should fall back to default: got %d, want 16000", got)
	}

	os.Setenv(key, "-5")
	if got := gpt5BudgetFromEnv(16000); got != 16000 {
		t.Errorf("non-positive value should fall back to default: got %d, want 16000", got)
	}
}

// TestChatCompletionRequest_GPT5Encoding verifies that the wire format includes
// max_completion_tokens for gpt-5.x models and excludes max_tokens.
func TestChatCompletionRequest_GPT5Encoding(t *testing.T) {
	budget := 16000
	req := chatCompletionRequest{
		Model:               "gpt-5.5",
		Messages:            []chatMessage{{Role: "user", Content: "hi"}},
		MaxCompletionTokens: &budget,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if _, ok := decoded["max_tokens"]; ok {
		t.Errorf("max_tokens should be omitted for gpt-5.x; got %s", string(data))
	}
	if v, ok := decoded["max_completion_tokens"]; !ok {
		t.Errorf("max_completion_tokens missing; got %s", string(data))
	} else if v.(float64) != 16000 {
		t.Errorf("max_completion_tokens = %v, want 16000", v)
	}
}

// TestChatCompletionRequest_LegacyEncoding verifies that legacy gpt-4 requests
// omit both fields (current behavior).
func TestChatCompletionRequest_LegacyEncoding(t *testing.T) {
	req := chatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []chatMessage{{Role: "user", Content: "hi"}},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if _, ok := decoded["max_tokens"]; ok {
		t.Errorf("max_tokens should be omitted by default; got %s", string(data))
	}
	if _, ok := decoded["max_completion_tokens"]; ok {
		t.Errorf("max_completion_tokens should be omitted for legacy models; got %s", string(data))
	}
}
