package providers

import (
	"context"
	"time"
)

// OpenAIProvider implements the OpenAI Codex CLI
type OpenAIProvider struct {
	baseProvider
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider() *OpenAIProvider {
	return &OpenAIProvider{
		baseProvider: baseProvider{
			name:         "openai",
			defaultModel: "gpt-5.2",
			command:      "codex",
		},
	}
}

// Query executes a prompt using Codex CLI
// Command: codex exec "{prompt}" -m {model} --skip-git-repo-check
func (p *OpenAIProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := []string{"exec", prompt, "-m", model, "--skip-git-repo-check"}
	output, err := runCommand(ctx, "codex", args, nil)
	duration := time.Since(start)

	return output, duration, nil, err
}
