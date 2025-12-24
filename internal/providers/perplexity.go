package providers

import (
	"context"
	"regexp"
	"strconv"
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

// Query executes a prompt using Perplexity CLI with usage stats
// Command: perplexity --usage -m {model} "{query}"
func (p *PerplexityProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := []string{"--usage", "-m", model, prompt}
	output, err := runCommand(ctx, "perplexity", args, nil)
	duration := time.Since(start)

	if err != nil {
		return output, duration, nil, err
	}

	// Parse output to extract content and metrics
	content, metrics := parsePerplexityOutput(output)
	return content, duration, metrics, nil
}

// parsePerplexityOutput extracts content and metrics from perplexity --usage output
func parsePerplexityOutput(output string) (string, *Metrics) {
	// Remove ANSI escape sequences
	cleaned := ansiRegex.ReplaceAllString(output, "")

	var metrics Metrics
	var contentLines []string
	inContent := false

	for _, line := range strings.Split(cleaned, "\n") {
		trimmed := strings.TrimSpace(line)

		// Parse token counts
		if strings.HasPrefix(trimmed, "- prompt_tokens:") {
			if v, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, "- prompt_tokens:"))); err == nil {
				metrics.InputTokens = v
			}
		} else if strings.HasPrefix(trimmed, "- completion_tokens:") {
			if v, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, "- completion_tokens:"))); err == nil {
				metrics.OutputTokens = v
			}
		} else if strings.Contains(trimmed, "'total_cost':") {
			// Parse cost from: - cost: {'input_tokens_cost': 0.0, ..., 'total_cost': 0.007}
			if idx := strings.Index(trimmed, "'total_cost':"); idx != -1 {
				rest := trimmed[idx+len("'total_cost':"):]
				rest = strings.TrimSpace(rest)
				rest = strings.TrimSuffix(rest, "}")
				if v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64); err == nil {
					metrics.CostUSD = v
				}
			}
		}

		// Detect content section
		if trimmed == "Content" {
			inContent = true
			continue
		}

		if inContent && trimmed != "" {
			contentLines = append(contentLines, line)
		}
	}

	content := strings.TrimSpace(strings.Join(contentLines, "\n"))

	// Return nil metrics if nothing was parsed
	if metrics.InputTokens == 0 && metrics.OutputTokens == 0 {
		return content, nil
	}

	return content, &metrics
}
