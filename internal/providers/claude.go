package providers

import (
	"context"
	"time"
)

// ClaudeProvider implements the Claude Code CLI
type ClaudeProvider struct {
	baseProvider
}

// NewClaudeProvider creates a new Claude provider
func NewClaudeProvider() *ClaudeProvider {
	return &ClaudeProvider{
		baseProvider: baseProvider{
			name:         "claude",
			defaultModel: "sonnet",
			command:      "claude",
		},
	}
}

// Query executes a prompt using Claude CLI
// Command: claude --print "{prompt}" --model {model} --output-format text
func (p *ClaudeProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := []string{
		"--print", prompt,
		"--model", model,
		"--output-format", "text",
	}
	output, err := runCommand(ctx, "claude", args, nil)
	duration := time.Since(start)

	return output, duration, err
}
