package providers

import (
	"context"
	"time"
)

// GLMProvider implements the OpenCode CLI (for GLM models)
type GLMProvider struct {
	baseProvider
}

// NewGLMProvider creates a new GLM provider
func NewGLMProvider() *GLMProvider {
	return &GLMProvider{
		baseProvider: baseProvider{
			name:         "glm",
			defaultModel: "zai-coding-plan/glm-4.7",
			command:      "opencode",
		},
	}
}

// Query executes a prompt using OpenCode CLI
// Command: opencode run "{message}" --model {model}
func (p *GLMProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := []string{"run", prompt, "--model", model}
	output, err := runCommand(ctx, "opencode", args, nil)
	duration := time.Since(start)

	return output, duration, err
}
