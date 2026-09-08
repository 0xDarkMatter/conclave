package providers

// OpenRouter is the API-mode backend for "any model conclave has no direct
// provider for". It is reached through slash-routed tokens, not a provider
// name: in `-g` mode any provider token containing "/" (e.g.
// "deepseek/deepseek-v4", "anthropic/claude-opus-5") is an OpenRouter model,
// and the token is BOTH the provider name (progress line, judge label, --json
// output) AND the model id sent on the wire. Consequences that must hold:
//
//   - There is no plain "openrouter" provider. `registry.GetProvider` builds
//     one of these on the fly per slash token (ADR-010); `AllAPIProviders` does
//     not list it, so `--all` never auto-includes OpenRouter models.
//   - CLI mode rejects slash tokens: OpenRouter has no subscription path.
//   - Transport is OpenAI-compatible, so this file rides api_base.go (ADR-004).
//   - The key resolves through NewKeyRotator, so the OS-keyring fallback
//     (ADR-008) works: `conclave keyring set OPENROUTER_API_KEY`.
//
// Decision record: docs/adr/ADR-010-openrouter-as-a-slash-routed-api-backend.md

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// OpenRouterKeyEnv is the only env var / keyring account OpenRouter reads.
	OpenRouterKeyEnv = "OPENROUTER_API_KEY"
	// OpenRouterListingName is the row shown by --list-providers. It is NOT a
	// routable provider name; it documents the slash syntax.
	OpenRouterListingName = "openrouter"
	// openRouterListingModel is the placeholder "model" for that row.
	openRouterListingModel = "<vendor>/<model>"

	openRouterBaseURL = "https://openrouter.ai/api/v1"
	// Attribution headers OpenRouter asks apps to send so usage shows up under
	// the app on openrouter.ai; harmless if omitted, cheap to include.
	openRouterReferer = "https://github.com/0xDarkMatter/conclave"
	openRouterTitle   = "conclave"
)

// IsOpenRouterModel reports whether a provider token is a slash-routed
// OpenRouter model id ("vendor/model"). This is the ONLY routing rule; keep it
// trivially cheap because the registry, the CLI and the pricing catalog all
// call it on every token.
func IsOpenRouterModel(token string) bool {
	return strings.Contains(token, "/")
}

// OpenRouterAPIProvider sends one OpenRouter model through the shared
// OpenAI-compatible client. Name() and DefaultModel() are the same slug.
type OpenRouterAPIProvider struct {
	apiBaseProvider
	keyRotator *KeyRotator
}

// NewOpenRouterAPIProvider builds a provider for one OpenRouter model slug.
// An empty model yields the --list-providers placeholder row (not routable).
func NewOpenRouterAPIProvider(model string) *OpenRouterAPIProvider {
	name := model
	if model == "" {
		name = OpenRouterListingName
		model = openRouterListingModel
	}
	return &OpenRouterAPIProvider{
		apiBaseProvider: apiBaseProvider{
			name:         name,
			defaultModel: model,
			apiKeyEnv:    OpenRouterKeyEnv,
			baseURL:      openRouterBaseURL,
		},
		keyRotator: NewKeyRotator(OpenRouterKeyEnv),
	}
}

func (p *OpenRouterAPIProvider) IsAvailable() bool {
	return p.keyRotator.HasKeys()
}

func (p *OpenRouterAPIProvider) getAPIKey() string {
	return p.keyRotator.Next()
}

func (p *OpenRouterAPIProvider) headers() map[string]string {
	return map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", p.getAPIKey()),
		"HTTP-Referer":  openRouterReferer,
		"X-Title":       openRouterTitle,
	}
}

// Query executes a prompt via OpenRouter (OpenAI-compatible).
// POST https://openrouter.ai/api/v1/chat/completions
func (p *OpenRouterAPIProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()

	reqBody := chatCompletionRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
	}

	respBody, err := p.doRequest(ctx, "POST", p.baseURL+"/chat/completions", p.headers(), reqBody)
	duration := time.Since(start)

	if err != nil {
		return "", duration, nil, err
	}

	text, metrics, err := extractChatResponse(respBody)
	if err != nil {
		return "", duration, nil, err
	}

	return text, duration, metrics, nil
}

// openRouterKeyInfo is the subset of GET /auth/key we act on. `limit` is null
// for keys with no spend cap, so both fields are pointers: a nil limit means
// "unlimited", not "zero".
type openRouterKeyInfo struct {
	Data struct {
		Label          string   `json:"label"`
		Usage          float64  `json:"usage"`
		Limit          *float64 `json:"limit"`
		LimitRemaining *float64 `json:"limit_remaining"`
		IsFreeTier     bool     `json:"is_free_tier"`
	} `json:"data"`
}

// Preflight validates the key without spending tokens.
// GET https://openrouter.ai/api/v1/auth/key — non-200 fails; a key whose
// spend limit has nothing remaining fails with a "no credit" message so the
// user is not left guessing at a 402 mid-panel.
func (p *OpenRouterAPIProvider) Preflight(ctx context.Context) error {
	body, err := p.doRequest(ctx, "GET", p.baseURL+"/auth/key", p.headers(), nil)
	if err != nil {
		return fmt.Errorf("OpenRouter key check failed: %w", err)
	}
	var info openRouterKeyInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("OpenRouter key check: unexpected response: %w", err)
	}
	if info.Data.Limit != nil && info.Data.LimitRemaining != nil && *info.Data.LimitRemaining <= 0 {
		return fmt.Errorf("OpenRouter key has no credit remaining (limit $%.2f, used $%.2f)", *info.Data.Limit, info.Data.Usage)
	}
	return nil
}
