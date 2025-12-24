package providers

import (
	"context"
	"regexp"
	"strings"
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

// ansiRegex matches ANSI escape sequences
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

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

	if err != nil {
		return output, duration, err
	}

	// Strip ANSI escape codes and clean up output
	return cleanPerplexityOutput(output), duration, nil
}

// cleanPerplexityOutput removes ANSI codes and header lines
func cleanPerplexityOutput(output string) string {
	// Remove ANSI escape sequences
	cleaned := ansiRegex.ReplaceAllString(output, "")

	// Remove the "Content" header line that perplexity adds
	lines := strings.Split(cleaned, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Content" || trimmed == "" && len(result) == 0 {
			continue
		}
		result = append(result, line)
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}
