package providers

import (
	"context"
	"time"
)

// GeminiProvider implements the Gemini CLI
type GeminiProvider struct {
	baseProvider
}

// NewGeminiProvider creates a new Gemini provider
func NewGeminiProvider() *GeminiProvider {
	return &GeminiProvider{
		baseProvider: baseProvider{
			name:         "gemini",
			defaultModel: "gemini-2.5-pro",
			command:      "gemini",
		},
	}
}

// Query executes a prompt using Gemini CLI
// Command: gemini -m {model} "{prompt}"
func (p *GeminiProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := []string{"-m", model, prompt}
	output, err := runCommand(ctx, "gemini", args, nil)
	duration := time.Since(start)

	return output, duration, err
}
