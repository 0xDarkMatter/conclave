package providers

import (
	"context"
	"time"
)

// GrokProvider implements the Grok CLI
type GrokProvider struct {
	baseProvider
}

// NewGrokProvider creates a new Grok provider
func NewGrokProvider() *GrokProvider {
	return &GrokProvider{
		baseProvider: baseProvider{
			name:         "grok",
			defaultModel: "grok-code-fast-1",
			command:      "grok",
		},
	}
}

// Query executes a prompt using Grok CLI
// Command: grok -p "{prompt}" -m {model}
func (p *GrokProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := []string{"-p", prompt, "-m", model}
	output, err := runCommand(ctx, "grok", args, nil)
	duration := time.Since(start)

	return output, duration, err
}
