package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
//
// Unlike claude/codex, gemini-cli in CLI mode genuinely needs a key here:
// Google retired the free "Gemini Code Assist for individuals" OAuth tier for
// gemini-cli (IneligibleTierError, 2026-09), so the only working headless auth
// is GEMINI_API_KEY / GOOGLE_API_KEY. The check is deliberate, not a leftover.
func (p *GeminiProvider) Preflight(ctx context.Context) error {
	if os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("GOOGLE_API_KEY") == "" {
		return fmt.Errorf("neither GEMINI_API_KEY nor GOOGLE_API_KEY is set (gemini-cli's free OAuth tier was retired; a key is required)")
	}
	return nil
}

// isGeminiCLIAuthError recognises gemini-cli's Code Assist / OAuth failures.
// Strings are from gemini-cli 0.58 (IneligibleTierError, onboardUser 403).
func isGeminiCLIAuthError(err error) bool {
	s := strings.ToLower(err.Error())
	for _, needle := range []string{
		"error authenticating",
		"ineligibletiererror",
		"cloudaicompanion",
		"onboarduser",
		"unsupported_client",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// Query executes a prompt using Gemini CLI with JSON output for metrics
// Command: gemini -o json -m {model} --skip-trust -p "{prompt}"
//
// Three things here are load-bearing (each was a live failure on 2026-09-08):
//   - "-p": without it gemini-cli 0.58 treats the positional prompt as the seed
//     for INTERACTIVE mode and never returns in a headless run.
//   - "--skip-trust" + GEMINI_CLI_TRUST_WORKSPACE: the trusted-folder gate
//     exits 55 in any directory the user has not blessed interactively.
//   - GEMINI_CLI_AUTH_TYPE=gemini-api-key: gemini-cli picks auth from its own
//     settings.json (usually "oauth-personal"), ignoring an exported key. That
//     OAuth path is dead (see Preflight), so we force key auth when a key is
//     present and the user has not chosen otherwise.
func (p *GeminiProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := []string{"-o", "json", "-m", model, "--skip-trust", "-p", prompt}
	env := []string{"GEMINI_CLI_TRUST_WORKSPACE=true"}
	hasKey := os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != ""
	output, err := runCommandEnv(ctx, "gemini", args, nil, env)
	duration := time.Since(start)

	if err != nil {
		// gemini-cli picks its auth method from ~/.gemini/settings.json
		// (security.auth.selectedType) and ignores an exported key when that
		// says "oauth-personal". That OAuth route is dead (see Preflight), so a
		// CLI auth failure with a key present is a configuration trap, not a
		// missing credential. Fall back to the direct API: same model, same
		// key, no coding-CLI system prompt. Precedent: ADR-007 (GLM).
		if hasKey && isGeminiCLIAuthError(err) {
			return NewGeminiAPIProvider().Query(ctx, prompt, model)
		}
		return output, duration, nil, err
	}

	// Parse JSON to extract response and metrics. Located, not assumed: like
	// claude (Gotcha 13), a CLI that promises JSON can still print a
	// diagnostic line on stdout, and a strict Unmarshal of the whole buffer
	// would then hand the noise plus the raw envelope back as the answer.
	// Pinned by TestGeminiCLIIgnoresStdoutNoiseAroundJSON.
	var result geminiJSONOutput
	_, ok := findJSONObject(output, func(raw json.RawMessage, fields map[string]json.RawMessage) bool {
		if _, has := fields["response"]; !has {
			return false
		}
		result = geminiJSONOutput{}
		return json.Unmarshal(raw, &result) == nil
	})
	if !ok {
		// No envelope anywhere: return raw output
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
