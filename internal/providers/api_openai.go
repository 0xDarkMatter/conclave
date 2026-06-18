package providers

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultGPT5MaxCompletionTokens is the budget used for gpt-5.x reasoning
// models. They consume hidden reasoning tokens against the completion budget;
// 16000 leaves room for both reasoning and a substantive answer.
// Override with CONCLAVE_OPENAI_MAX_COMPLETION_TOKENS.
const defaultGPT5MaxCompletionTokens = 16000

// isGPT5Family reports whether the model is a gpt-5.x reasoning model that
// requires max_completion_tokens instead of max_tokens.
func isGPT5Family(model string) bool {
	m := strings.ToLower(model)
	return strings.HasPrefix(m, "gpt-5") || strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3")
}

// gpt5BudgetFromEnv reads CONCLAVE_OPENAI_MAX_COMPLETION_TOKENS, falling back
// to the supplied default. Invalid values fall back to default.
func gpt5BudgetFromEnv(def int) int {
	if v := os.Getenv("CONCLAVE_OPENAI_MAX_COMPLETION_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

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
			defaultModel: "gpt-5.5",
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

	if isGPT5Family(model) {
		budget := gpt5BudgetFromEnv(defaultGPT5MaxCompletionTokens)
		reqBody.MaxCompletionTokens = &budget
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
