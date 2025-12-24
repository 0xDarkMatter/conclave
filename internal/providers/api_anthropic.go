package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// AnthropicAPIProvider implements the Anthropic API
type AnthropicAPIProvider struct {
	apiBaseProvider
}

// NewAnthropicAPIProvider creates a new Anthropic API provider
func NewAnthropicAPIProvider() *AnthropicAPIProvider {
	return &AnthropicAPIProvider{
		apiBaseProvider: apiBaseProvider{
			name:         "claude",
			defaultModel: "claude-opus-4-5-20251101",
			apiKeyEnv:    "ANTHROPIC_API_KEY",
			baseURL:      "https://api.anthropic.com",
		},
	}
}

// Anthropic-specific request/response structures
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Query executes a prompt using Anthropic API
// POST https://api.anthropic.com/v1/messages
func (p *AnthropicAPIProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()

	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: 8192,
		Messages: []anthropicMessage{
			{Role: "user", Content: prompt},
		},
	}

	headers := map[string]string{
		"x-api-key":         p.getAPIKey(),
		"anthropic-version": "2023-06-01",
	}

	respBody, err := p.doRequest(ctx, "POST", p.baseURL+"/v1/messages", headers, reqBody)
	duration := time.Since(start)

	if err != nil {
		return "", duration, nil, err
	}

	text, metrics, err := p.parseResponse(respBody)
	if err != nil {
		return "", duration, nil, err
	}

	return text, duration, metrics, nil
}

func (p *AnthropicAPIProvider) parseResponse(data []byte) (string, *Metrics, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", nil, fmt.Errorf("parse response: %w", err)
	}

	// Extract text from content blocks
	var text string
	for _, block := range resp.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}

	if text == "" {
		return "", nil, fmt.Errorf("no text content in response")
	}

	var metrics *Metrics
	if resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
		metrics = &Metrics{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		}
	}

	return text, metrics, nil
}
