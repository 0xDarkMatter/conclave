package providers

import (
	"context"
	"fmt"
	"os"
	"time"
)

// GLMAPIProvider implements the Zhipu GLM API
type GLMAPIProvider struct {
	apiBaseProvider
}

// NewGLMAPIProvider creates a new GLM API provider
func NewGLMAPIProvider() *GLMAPIProvider {
	return &GLMAPIProvider{
		apiBaseProvider: apiBaseProvider{
			name:         "glm",
			defaultModel: "glm-4.7",
			apiKeyEnv:    "ZHIPU_API_KEY",
			baseURL:      "https://open.bigmodel.cn",
		},
	}
}

// IsAvailable checks for ZHIPU_API_KEY or GLM_API_KEY
func (p *GLMAPIProvider) IsAvailable() bool {
	return os.Getenv("ZHIPU_API_KEY") != "" || os.Getenv("GLM_API_KEY") != ""
}

func (p *GLMAPIProvider) getAPIKey() string {
	key := os.Getenv("ZHIPU_API_KEY")
	if key == "" {
		key = os.Getenv("GLM_API_KEY")
	}
	return key
}

func (p *GLMAPIProvider) getBaseURL() string {
	// Support custom base URL via GLM_BASE_URL env var
	if url := os.Getenv("GLM_BASE_URL"); url != "" {
		return url
	}
	return p.baseURL + "/api/paas/v4"
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
