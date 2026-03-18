package providers

import (
	"context"
	"encoding/json"
	"fmt"
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
			defaultModel: "claude-opus-4-5-20251101",
			command:      "claude",
		},
	}
}

// claudeJSONOutput represents Claude's JSON output structure
type claudeJSONOutput struct {
	Result       string  `json:"result"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Preflight checks Claude CLI auth status without consuming tokens.
func (p *ClaudeProvider) Preflight(ctx context.Context) error {
	output, err := runCommand(ctx, "claude", []string{"auth", "status"}, nil)
	if err != nil {
		return fmt.Errorf("claude auth check failed: %w", err)
	}

	var status struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		return fmt.Errorf("could not parse claude auth status: %w", err)
	}
	if !status.LoggedIn {
		return fmt.Errorf("claude is not logged in")
	}
	return nil
}

// Query executes a prompt using Claude CLI with JSON output for metrics
// Command: claude --print "{prompt}" --model {model} --output-format json
func (p *ClaudeProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := []string{
		"--print", prompt,
		"--model", model,
		"--output-format", "json",
	}
	output, err := runCommand(ctx, "claude", args, nil)
	duration := time.Since(start)

	if err != nil {
		return output, duration, nil, err
	}

	// Parse JSON to extract response and metrics
	var result claudeJSONOutput
	if jsonErr := json.Unmarshal([]byte(output), &result); jsonErr != nil {
		// If JSON parsing fails, return raw output
		return output, duration, nil, nil
	}

	metrics := &Metrics{
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		CostUSD:      result.TotalCostUSD,
	}

	return result.Result, duration, metrics, nil
}
