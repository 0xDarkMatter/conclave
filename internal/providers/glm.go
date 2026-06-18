package providers

import (
	"context"
	"fmt"
	"os"
	"time"
)

// codingGLMBaseURL is the GLM Coding Plan OpenAI-compatible endpoint. The
// Coding Plan is billed as a flat subscription (no pay-as-you-go balance),
// unlike the standard paas/v4 endpoint used by GLMAPIProvider. Using it
// directly over HTTP means GLM needs no external CLI (this provider used to
// shell out to `opencode`). Override with GLM_BASE_URL.
const codingGLMBaseURL = "https://api.z.ai/api/coding/paas/v4"

// GLMProvider talks to the GLM Coding Plan over HTTP (OpenAI-compatible),
// so it requires only an API key — no `opencode` (or any other) CLI binary.
type GLMProvider struct {
	apiBaseProvider
	keyRotator *KeyRotator
}

// NewGLMProvider creates a GLM provider backed by the Coding Plan endpoint.
func NewGLMProvider() *GLMProvider {
	return &GLMProvider{
		apiBaseProvider: apiBaseProvider{
			name:         "glm",
			defaultModel: "glm-5.2",
			apiKeyEnv:    "GLM_API_KEY",
			baseURL:      codingGLMBaseURL,
		},
		keyRotator: NewKeyRotator("GLM_API_KEY", "ZAI_API_KEY", "ZHIPU_API_KEY"),
	}
}

func (p *GLMProvider) IsAvailable() bool {
	return p.keyRotator.HasKeys()
}

func (p *GLMProvider) getAPIKey() string {
	return p.keyRotator.Next()
}

// getBaseURL returns the Coding Plan endpoint, overridable via GLM_BASE_URL.
func (p *GLMProvider) getBaseURL() string {
	if v := os.Getenv("GLM_BASE_URL"); v != "" {
		return v
	}
	return p.baseURL
}

// Preflight checks that a GLM Coding Plan API key is available.
func (p *GLMProvider) Preflight(ctx context.Context) error {
	if !p.keyRotator.HasKeys() {
		return fmt.Errorf("no GLM API key set (GLM_API_KEY, ZAI_API_KEY, or ZHIPU_API_KEY)")
	}
	return nil
}

// Query executes a prompt using the GLM Coding Plan API (OpenAI-compatible).
// POST https://api.z.ai/api/coding/paas/v4/chat/completions
func (p *GLMProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
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

	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", p.getAPIKey()),
	}

	respBody, err := p.doRequest(ctx, "POST", p.getBaseURL()+"/chat/completions", headers, reqBody)
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
