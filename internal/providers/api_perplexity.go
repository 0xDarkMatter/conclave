package providers

import (
	"context"
	"fmt"
	"time"
)

// PerplexityAPIProvider implements the Perplexity API
type PerplexityAPIProvider struct {
	apiBaseProvider
	keyRotator *KeyRotator
}

// NewPerplexityAPIProvider creates a new Perplexity API provider
func NewPerplexityAPIProvider() *PerplexityAPIProvider {
	return &PerplexityAPIProvider{
		apiBaseProvider: apiBaseProvider{
			name:         "perplexity",
			defaultModel: "sonar-pro",
			apiKeyEnv:    "PERPLEXITY_API_KEY",
			baseURL:      "https://api.perplexity.ai",
		},
		keyRotator: NewKeyRotator("PERPLEXITY_API_KEY"),
	}
}

func (p *PerplexityAPIProvider) IsAvailable() bool {
	return p.keyRotator.HasKeys()
}

func (p *PerplexityAPIProvider) getAPIKey() string {
	return p.keyRotator.Next()
}

// Query executes a prompt using Perplexity API (OpenAI-compatible)
// POST https://api.perplexity.ai/chat/completions
func (p *PerplexityAPIProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
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

	respBody, err := p.doRequest(ctx, "POST", p.baseURL+"/chat/completions", headers, reqBody)
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
