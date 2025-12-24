package providers

import (
	"context"
	"fmt"
	"os"
	"time"
)

// GrokAPIProvider implements the xAI Grok API
type GrokAPIProvider struct {
	apiBaseProvider
}

// NewGrokAPIProvider creates a new Grok API provider
func NewGrokAPIProvider() *GrokAPIProvider {
	return &GrokAPIProvider{
		apiBaseProvider: apiBaseProvider{
			name:         "grok",
			defaultModel: "grok-4-1-fast-reasoning",
			apiKeyEnv:    "XAI_API_KEY",
			baseURL:      "https://api.x.ai",
		},
	}
}

// IsAvailable checks for XAI_API_KEY or GROK_API_KEY
func (p *GrokAPIProvider) IsAvailable() bool {
	return os.Getenv("XAI_API_KEY") != "" || os.Getenv("GROK_API_KEY") != ""
}

func (p *GrokAPIProvider) getAPIKey() string {
	key := os.Getenv("XAI_API_KEY")
	if key == "" {
		key = os.Getenv("GROK_API_KEY")
	}
	return key
}

// Query executes a prompt using xAI Grok API (OpenAI-compatible)
// POST https://api.x.ai/v1/chat/completions
func (p *GrokAPIProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
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

	respBody, err := p.doRequest(ctx, "POST", p.baseURL+"/v1/chat/completions", headers, reqBody)
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
