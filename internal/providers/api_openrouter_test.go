package providers

// Tests for the slash-routed OpenRouter backend (ADR-010): wire shape and
// attribution headers, error surfacing, the /auth/key preflight including the
// "no credit" case, registry routing in both modes, and DisplayName's catalog
// hook. All HTTP goes to httptest; nothing here touches openrouter.ai.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
)

const orTestModel = "deepseek/deepseek-v4"

func TestOpenRouter_QuerySuccess(t *testing.T) {
	var gotPath, gotAuth, gotReferer, gotTitle, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotReferer = r.Header.Get("HTTP-Referer")
		gotTitle = r.Header.Get("X-Title")
		body, _ := io.ReadAll(r.Body)
		var req chatCompletionRequest
		_ = json.Unmarshal(body, &req)
		gotModel = req.Model
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	defer srv.Close()

	t.Setenv(OpenRouterKeyEnv, "test-key")
	p := NewOpenRouterAPIProvider(orTestModel)
	p.baseURL = srv.URL

	if p.Name() != orTestModel || p.DefaultModel() != orTestModel {
		t.Fatalf("name/model must both be the slug; got %q / %q", p.Name(), p.DefaultModel())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, _, metrics, err := p.Query(ctx, "reply with exactly: OK", "")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	cases := []struct{ name, got, want string }{
		{"response", out, "OK"},
		{"path", gotPath, "/chat/completions"},
		{"authorization", gotAuth, "Bearer test-key"},
		{"HTTP-Referer", gotReferer, openRouterReferer},
		{"X-Title", gotTitle, openRouterTitle},
		{"model on the wire", gotModel, orTestModel},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if metrics == nil || metrics.InputTokens != 3 || metrics.OutputTokens != 1 {
		t.Errorf("metrics = %+v, want in=3 out=1", metrics)
	}
}

func TestOpenRouter_QueryHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(402)
		_, _ = w.Write([]byte(`{"error":{"message":"Insufficient credits","code":402}}`))
	}))
	defer srv.Close()

	t.Setenv(OpenRouterKeyEnv, "test-key")
	p := NewOpenRouterAPIProvider(orTestModel)
	p.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, _, err := p.Query(ctx, "x", "")
	if err == nil {
		t.Fatal("expected error on 402")
	}
	for _, want := range []string{"HTTP 402", "Insufficient credits"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q; got: %s", want, err)
		}
	}
}

func TestOpenRouter_Preflight(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string // substring; empty means success
	}{
		{"unlimited key", 200, `{"data":{"label":"k","usage":1.5,"limit":null,"limit_remaining":null,"is_free_tier":false}}`, ""},
		{"credit remaining", 200, `{"data":{"label":"k","usage":4,"limit":10,"limit_remaining":6}}`, ""},
		{"no credit", 200, `{"data":{"label":"k","usage":10,"limit":10,"limit_remaining":0}}`, "no credit"},
		{"bad key", 401, `{"error":{"message":"User not found.","code":401}}`, "HTTP 401"},
		{"garbage body", 200, `not json`, "unexpected response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath, gotMethod string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotMethod = r.URL.Path, r.Method
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			t.Setenv(OpenRouterKeyEnv, "test-key")
			p := NewOpenRouterAPIProvider(orTestModel)
			p.baseURL = srv.URL

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := p.Preflight(ctx)

			if gotPath != "/auth/key" || gotMethod != http.MethodGet {
				t.Errorf("preflight hit %s %s, want GET /auth/key", gotMethod, gotPath)
			}
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestOpenRouter_PreflightRemediationForSlashToken(t *testing.T) {
	if r := getRemediation(orTestModel); !strings.Contains(r, OpenRouterKeyEnv) {
		t.Errorf("remediation for a slash token should name %s; got %q", OpenRouterKeyEnv, r)
	}
}

func TestIsOpenRouterModel(t *testing.T) {
	cases := map[string]bool{
		"deepseek/deepseek-v4":    true,
		"anthropic/claude-opus-5": true,
		"openai/gpt-5.6-sol":      true,
		"claude":                  false,
		"gemini":                  false,
		"zai-coding-plan/glm-5.2": true, // a slash is a slash; -m values never reach this check
		"":                        false,
		"/model":                  false, // malformed: empty vendor
		"model/":                  false, // malformed: empty slug
		"/":                       false,
	}
	for in, want := range cases {
		if got := IsOpenRouterModel(in); got != want {
			t.Errorf("IsOpenRouterModel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestRegistry_SlashRouting(t *testing.T) {
	t.Setenv(OpenRouterKeyEnv, "test-key")
	cfg := config.DefaultConfig()

	t.Run("API mode routes to OpenRouter", func(t *testing.T) {
		r := NewRegistry(cfg, true, false)
		p, err := r.GetProvider(orTestModel, nil)
		if err != nil {
			t.Fatalf("GetProvider: %v", err)
		}
		if p.Name() != orTestModel || p.DefaultModel() != orTestModel {
			t.Errorf("name/model = %q / %q, want both %q", p.Name(), p.DefaultModel(), orTestModel)
		}
		if _, ok := unwrapPreflighter(p); !ok {
			t.Error("slash-routed provider must expose Preflight through the override wrapper")
		}
		// Same token again (e.g. as judge) reuses the cached instance.
		if _, err := r.GetProvider(orTestModel, nil); err != nil {
			t.Errorf("second lookup: %v", err)
		}
		if _, cached := r.providers[orTestModel]; !cached {
			t.Error("slash-routed provider should be cached in the registry")
		}
	})

	t.Run("API mode honours -m override", func(t *testing.T) {
		r := NewRegistry(cfg, true, false)
		p, err := r.GetProvider(orTestModel, map[string]string{orTestModel: "deepseek/deepseek-v4:free"})
		if err != nil {
			t.Fatalf("GetProvider: %v", err)
		}
		if p.DefaultModel() != "deepseek/deepseek-v4:free" {
			t.Errorf("override not applied: %q", p.DefaultModel())
		}
	})

	t.Run("cheap mode falls back to the token", func(t *testing.T) {
		r := NewRegistry(cfg, true, true)
		p, err := r.GetProvider(orTestModel, nil)
		if err != nil {
			t.Fatalf("GetProvider: %v", err)
		}
		if p.DefaultModel() != orTestModel {
			t.Errorf("cheap mode has no cheap OpenRouter model; want token, got %q", p.DefaultModel())
		}
	})

	t.Run("CLI mode rejects slash tokens", func(t *testing.T) {
		r := NewRegistry(cfg, false, false)
		_, err := r.GetProvider(orTestModel, nil)
		if err == nil {
			t.Fatal("expected error in CLI mode")
		}
		for _, want := range []string{"API-only", "-g"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error should say %q; got: %s", want, err)
			}
		}
	})

	t.Run("plain openrouter is not a provider", func(t *testing.T) {
		r := NewRegistry(cfg, true, false)
		_, err := r.GetProvider(OpenRouterListingName, nil)
		if err == nil {
			t.Fatal("bare 'openrouter' must not resolve; slash tokens are the whole surface")
		}
		if !strings.Contains(err.Error(), "vendor/model") {
			t.Errorf("bare 'openrouter' should hint at the slash syntax; got: %s", err)
		}
	})

	t.Run("malformed slugs are rejected, not sent upstream", func(t *testing.T) {
		r := NewRegistry(cfg, true, false)
		for _, bad := range []string{"/model", "model/", "/"} {
			_, err := r.GetProvider(bad, nil)
			if err == nil || !strings.Contains(err.Error(), "malformed") {
				t.Errorf("GetProvider(%q) = %v, want malformed-slug error", bad, err)
			}
		}
	})
}

func TestOpenRouter_InlineErrorIn200(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string // empty: no inline error detected
	}{
		{"upstream failure relayed as 200", `{"error":{"message":"Provider returned error","code":502},"user_id":"u"}`, "OpenRouter error 502: Provider returned error"},
		{"error without code", `{"error":{"message":"boom"}}`, "OpenRouter error: boom"},
		{"normal response", `{"choices":[{"message":{"content":"OK"}}]}`, ""},
		{"error field but choices present wins", `{"error":{"message":"partial"},"choices":[{"message":{"content":"OK"}}]}`, ""},
		{"not json", `nope`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := openRouterInlineError([]byte(tt.body))
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr) {
				t.Fatalf("got %v, want %q", err, tt.wantErr)
			}
		})
	}

	// End to end: a 200 error envelope must surface as an error from Query.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"message":"Provider returned error","code":502}}`))
	}))
	defer srv.Close()
	t.Setenv(OpenRouterKeyEnv, "test-key")
	p := NewOpenRouterAPIProvider(orTestModel)
	p.baseURL = srv.URL
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, _, err := p.Query(ctx, "x", ""); err == nil || !strings.Contains(err.Error(), "Provider returned error") {
		t.Errorf("Query should surface the inline error; got %v", err)
	}
}

func TestAllAPIProvidersExcludesOpenRouter(t *testing.T) {
	for _, p := range AllAPIProviders() {
		if p.Name() == OpenRouterListingName || IsOpenRouterModel(p.Name()) {
			t.Fatalf("AllAPIProviders must not include OpenRouter (would leak into --all): %s", p.Name())
		}
	}
	l := OpenRouterListing()
	if l.Name() != OpenRouterListingName || !strings.Contains(l.DefaultModel(), "/") {
		t.Errorf("listing row = %q %q, want openrouter + vendor/model placeholder", l.Name(), l.DefaultModel())
	}
}

func TestDisplayName_SlashToken(t *testing.T) {
	SetOpenRouterNamer(nil)
	if got := DisplayName(orTestModel, orTestModel); got != orTestModel {
		t.Errorf("without a namer, want raw slug; got %q", got)
	}
	SetOpenRouterNamer(func(id string) (string, bool) {
		if id == orTestModel {
			return "DeepSeek: DeepSeek V4", true
		}
		return "", false
	})
	defer SetOpenRouterNamer(nil)
	if got := DisplayName(orTestModel, "default"); got != "DeepSeek: DeepSeek V4" {
		t.Errorf("with a namer, want catalog label; got %q", got)
	}
	if got := DisplayName("other/unknown", "other/unknown"); got != "other/unknown" {
		t.Errorf("namer miss should fall back to slug; got %q", got)
	}
	if got := DisplayName("claude", "claude-opus-5"); !strings.HasPrefix(got, "Anthropic") {
		t.Errorf("non-slash providers unaffected; got %q", got)
	}
}
