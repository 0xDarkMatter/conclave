package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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

// grokMessage represents a message in grok's JSONL output
type grokMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Preflight checks that a Grok/xAI API key is available.
func (p *GrokProvider) Preflight(ctx context.Context) error {
	if os.Getenv("XAI_API_KEY") == "" {
		return fmt.Errorf("XAI_API_KEY is not set")
	}
	return nil
}

// Query executes a prompt using Grok CLI
// Command: grok -p "{prompt}" -m {model}
func (p *GrokProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := []string{"-p", prompt, "-m", model}
	output, err := runCommand(ctx, "grok", args, nil)
	duration := time.Since(start)

	if err != nil {
		return output, duration, nil, err
	}

	// Parse JSONL output to extract assistant response
	return parseGrokOutput(output), duration, nil, nil
}

// parseGrokOutput extracts the assistant's content from grok's JSONL output
func parseGrokOutput(output string) string {
	var responses []string

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var msg grokMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		if msg.Role == "assistant" && msg.Content != "" {
			responses = append(responses, msg.Content)
		}
	}

	if len(responses) > 0 {
		return strings.Join(responses, "\n")
	}

	// Fallback: return original if parsing failed
	return output
}
