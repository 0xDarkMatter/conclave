package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// GeminiProvider implements the Gemini CLI
type GeminiProvider struct {
	baseProvider
}

// NewGeminiProvider creates a new Gemini provider
func NewGeminiProvider() *GeminiProvider {
	return &GeminiProvider{
		baseProvider: baseProvider{
			name:         "gemini",
			defaultModel: "gemini-3.1-pro-preview",
			command:      "gemini",
		},
	}
}

// geminiTokens represents token usage for a model
type geminiTokens struct {
	Prompt     int `json:"prompt"`
	Candidates int `json:"candidates"`
	Total      int `json:"total"`
	Cached     int `json:"cached"`
}

// geminiModelStats represents stats for a single model
type geminiModelStats struct {
	Tokens geminiTokens `json:"tokens"`
}

// geminiJSONOutput represents Gemini's JSON output structure
type geminiJSONOutput struct {
	Response string `json:"response"`
	Stats    struct {
		Models map[string]geminiModelStats `json:"models"`
	} `json:"stats"`
}

// Preflight checks that a Gemini API key is available.
func (p *GeminiProvider) Preflight(ctx context.Context) error {
	if os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("GOOGLE_API_KEY") == "" {
		return fmt.Errorf("neither GEMINI_API_KEY nor GOOGLE_API_KEY is set")
	}
	return nil
}

// Query executes a prompt using Gemini CLI with JSON output for metrics
// Command: gemini -o json -m {model} "{prompt}"
func (p *GeminiProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := []string{"-o", "json", "-m", model, prompt}
	output, err := runCommand(ctx, "gemini", args, nil)
	duration := time.Since(start)

	if err != nil {
		return output, duration, nil, err
	}

	// Parse JSON to extract response and metrics
	var result geminiJSONOutput
	if jsonErr := json.Unmarshal([]byte(output), &result); jsonErr != nil {
		// If JSON parsing fails, return raw output
		return output, duration, nil, nil
	}

	// Aggregate metrics across all models used
	var metrics Metrics
	for _, m := range result.Stats.Models {
		metrics.InputTokens += m.Tokens.Prompt
		metrics.OutputTokens += m.Tokens.Candidates
		metrics.CacheTokens += m.Tokens.Cached
	}

	// Return nil metrics if nothing was captured
	if metrics.InputTokens == 0 && metrics.OutputTokens == 0 {
		return result.Response, duration, nil, nil
	}

	return result.Response, duration, &metrics, nil
}
