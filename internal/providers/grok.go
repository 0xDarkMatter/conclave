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
			defaultModel: "grok-4.7",
			command:      "grok",
		},
	}
}

// grokMessage represents a message in grok's JSONL output
type grokMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// The grok CLI deliberately has no Preflight. It carries its own login
// (grok.com OAuth or a deployment key; `grok models` names which), exactly
// like codex and claude (Gotcha 7), so demanding XAI_API_KEY refused a working
// CLI before any query ran. A CLI that is not logged in fails the query with
// grok's own message. XAI_API_KEY gates grok@api only, in api_grok.go.
// Pinned by TestGrokCLIPreflightDoesNotDemandAPIKey.

// Query executes a prompt using Grok CLI
// Command: grok --prompt-file {tempfile} -m {model}
//
// The prompt travels in a temp file, never argv (Gotcha 8). As `-p <prompt>`
// a prompt opening with "-" was rejected by grok's argument parser, and one
// past Windows' 32,767-character command line could not start. grok has no
// stdin prompt mode; --prompt-file is its single-turn equivalent of -p.
// Pinned by TestGrokCLIPromptTravelsInAFile.
func (p *GrokProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	promptFile, err := writePromptFile(prompt)
	if err != nil {
		return "", time.Since(start), nil, fmt.Errorf("grok: %w", err)
	}
	defer os.Remove(promptFile)
	args := []string{"--prompt-file", promptFile, "-m", model}
	output, err := runCommand(ctx, "grok", args, nil)
	duration := time.Since(start)

	if err != nil {
		return output, duration, nil, err
	}

	// Parse JSONL output to extract assistant response
	return parseGrokOutput(output), duration, nil, nil
}

// writePromptFile stores the prompt in a private temp file for a CLI that
// reads its prompt from a path. The caller removes it.
func writePromptFile(prompt string) (string, error) {
	f, err := os.CreateTemp("", "conclave-prompt-*.txt")
	if err != nil {
		return "", fmt.Errorf("create prompt file: %w", err)
	}
	if _, err := f.WriteString(prompt); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("write prompt file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("close prompt file: %w", err)
	}
	return f.Name(), nil
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
