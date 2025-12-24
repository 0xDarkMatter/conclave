package providers

import (
	"context"
	"time"
)

// PerplexityProvider implements the Perplexity CLI
type PerplexityProvider struct {
	baseProvider
}

// NewPerplexityProvider creates a new Perplexity provider
func NewPerplexityProvider() *PerplexityProvider {
	return &PerplexityProvider{
		baseProvider: baseProvider{
			name:         "perplexity",
			defaultModel: "sonar-pro",
			command:      "perplexity",
		},
	}
}

// Query executes a prompt using Perplexity CLI
// Command: perplexity -m {model} "{query}"
func (p *PerplexityProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := []string{"-m", model, prompt}
	output, err := runCommand(ctx, "perplexity", args, nil)
	duration := time.Since(start)

	return output, duration, err
}
