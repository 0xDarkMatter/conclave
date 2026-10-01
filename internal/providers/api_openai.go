package providers

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultGPT5MaxCompletionTokens is the budget used for reasoning models
// (gpt-5 onward, o-series; see isReasoningModel). They consume hidden reasoning tokens against the completion budget;
// 16000 leaves room for both reasoning and a substantive answer.
// Override with CONCLAVE_OPENAI_MAX_COMPLETION_TOKENS.
const defaultGPT5MaxCompletionTokens = 16000

// isReasoningModel reports whether the model is an OpenAI reasoning model
// that rejects max_tokens and requires max_completion_tokens: gpt-N for any
// major version N >= 5 (gpt-5.x, gpt-6-sol, gpt-6.1-sol, ...) and the o-series
// (o1, o3, o4-mini, ...).
//
// Deliberately a version comparison, not a list of prefixes. The rule used to
// be the literal prefixes "gpt-5", "o1", "o3", and every new generation
// (GPT-6, 2026-09; o4-mini) silently fell outside it and got HTTP 400
// unsupported_parameter. Older ids (gpt-4o, gpt-4.1) and non-numeric names
// (gpt-oss-120b, gpt-chat-latest) still take max_tokens.
// Pinned by TestReasoningModelDetection.
func isReasoningModel(model string) bool {
	m := strings.ToLower(model)
	if rest, ok := strings.CutPrefix(m, "gpt-"); ok {
		digits := 0
		for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
			digits++
		}
		major, err := strconv.Atoi(rest[:digits])
		return err == nil && major >= 5
	}
	// o-series: "o" followed directly by a digit (not "omni-...").
	return len(m) >= 2 && m[0] == 'o' && m[1] >= '1' && m[1] <= '9'
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
			defaultModel: "gpt-6.1-sol",
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

	if isReasoningModel(model) {
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
