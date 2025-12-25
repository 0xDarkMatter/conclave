package providers

import (
	"context"
	"fmt"
	"time"
)

// OpenAIAPIProvider implements the OpenAI API
type OpenAIAPIProvider struct {
	apiBaseProvider
	keyRotator *KeyRotator
}

// NewOpenAIAPIProvider creates a new OpenAI API provider
func NewOpenAIAPIProvider() *OpenAIAPIProvider {
	return &OpenAIAPIProvider{
		apiBaseProvider: apiBaseProvider{
			name:         "openai",
			defaultModel: "gpt-5.2",
			apiKeyEnv:    "OPENAI_API_KEY",
			baseURL:      "https://api.openai.com",
		},
		keyRotator: NewKeyRotator("OPENAI_API_KEY"),
	}
}

func (p *OpenAIAPIProvider) IsAvailable() bool {
	return p.keyRotator.HasKeys()
}

func (p *OpenAIAPIProvider) getAPIKey() string {
	return p.keyRotator.Next()
}

// Query executes a prompt using OpenAI API
// POST https://api.openai.com/v1/chat/completions
func (p *OpenAIAPIProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
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
