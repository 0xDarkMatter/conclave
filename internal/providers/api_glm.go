package providers

import (
	"context"
	"fmt"
	"time"
)

// GLMAPIProvider implements the Zhipu GLM API
type GLMAPIProvider struct {
	apiBaseProvider
	keyRotator *KeyRotator
}

// NewGLMAPIProvider creates a new GLM API provider
func NewGLMAPIProvider() *GLMAPIProvider {
	return &GLMAPIProvider{
		apiBaseProvider: apiBaseProvider{
			name:         "glm",
			defaultModel: "glm-5.2",
			apiKeyEnv:    "ZHIPU_API_KEY",
			baseURL:      "https://open.bigmodel.cn",
		},
		keyRotator: NewKeyRotator("ZHIPU_API_KEY", "GLM_API_KEY"),
	}
}

func (p *GLMAPIProvider) IsAvailable() bool {
	return p.keyRotator.HasKeys()
}

func (p *GLMAPIProvider) getAPIKey() string {
	return p.keyRotator.Next()
}

func (p *GLMAPIProvider) getBaseURL() string {
	// API uses non-coding endpoint (GLM_BASE_URL is for CLI coding endpoint)
	return "https://api.z.ai/api/paas/v4"
}

// Query executes a prompt using Zhipu GLM API (OpenAI-compatible)
// POST https://open.bigmodel.cn/api/paas/v4/chat/completions
func (p *GLMAPIProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
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
