package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
			defaultModel: "claude-opus-5",
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

	// Parse JSON to extract response and metrics. The envelope is located, not
	// assumed: the claude CLI prints MCP diagnostics on STDOUT ahead of it
	// ("Client.listTools() called but server does not advertise tools
	// capability - returning empty list", 2026-09-13), and a strict Unmarshal
	// of the whole buffer then fails, leaking the raw noise + envelope to the
	// caller as the "answer". Pinned by TestClaudeCLIIgnoresLeadingNoiseBeforeJSON.
	result, ok := parseClaudeJSONOutput(output)
	if !ok {
		// No envelope anywhere (e.g. --output-format ignored): return raw output
		return output, duration, nil, nil
	}

	metrics := &Metrics{
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		CostUSD:      result.TotalCostUSD,
	}

	return result.Result, duration, metrics, nil
}

// parseClaudeJSONOutput finds the claude CLI's JSON envelope inside output that
// may carry non-JSON noise around it and decodes it.
//
// It tries every '{' in the buffer as a candidate start and accepts the first
// one that decodes as a JSON object carrying a "result" key. Requiring the key
// is what makes this safe against structured noise: a warning line that
// happens to be a JSON object (`{"level":"warn",...}`) is skipped, and a
// candidate that decodes only because it starts inside the real envelope's
// nested `usage` object cannot win because it has no "result" of its own.
// json.Decoder is used rather than Unmarshal so trailing bytes after the
// envelope (another noise line, a stray newline) do not fail the decode.
//
// Returns ok=false when no such object exists anywhere in output.
func parseClaudeJSONOutput(output string) (claudeJSONOutput, bool) {
	for i := 0; i < len(output); i++ {
		start := strings.IndexByte(output[i:], '{')
		if start < 0 {
			break
		}
		i += start

		var fields map[string]json.RawMessage
		if err := json.NewDecoder(strings.NewReader(output[i:])).Decode(&fields); err != nil {
			continue
		}
		if _, hasResult := fields["result"]; !hasResult {
			continue
		}

		var result claudeJSONOutput
		if err := json.NewDecoder(strings.NewReader(output[i:])).Decode(&result); err != nil {
			continue
		}
		return result, true
	}
	return claudeJSONOutput{}, false
}
