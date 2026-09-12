package providers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// OpenAIProvider implements the OpenAI Codex CLI
type OpenAIProvider struct {
	baseProvider
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider() *OpenAIProvider {
	return &OpenAIProvider{
		baseProvider: baseProvider{
			name:         "openai",
			defaultModel: "gpt-5.6-sol",
			command:      "codex",
		},
	}
}

// Preflight checks that codex can authenticate. In CLI mode codex normally
// runs on a ChatGPT subscription login, so an API key is NOT required; we ask
// codex itself first and only fall back to the env var. Requiring the key here
// used to reject subscription users outright (reported 2026-09-08).
//
// Preflight is a fast-fail helper, not a gate: it only fails on an explicit
// "not logged in" from codex with no API key to fall back on. If codex cannot
// be asked in time (slow start, sandboxed shell), the query proceeds and any
// real auth problem surfaces from codex itself a few seconds later.
func (p *OpenAIProvider) Preflight(ctx context.Context) error {
	// codex prints its status line to STDERR even on success, hence Combined.
	out, _ := runCommandCombined(ctx, "codex", "login", "status")
	status := strings.ToLower(out)
	loggedOut := strings.Contains(status, "not logged in") || strings.Contains(status, "logged out")
	if !loggedOut {
		return nil // logged in, or indeterminate: let codex decide
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		return nil
	}
	return fmt.Errorf("codex is not logged in and OPENAI_API_KEY is not set")
}

// Query executes a prompt using Codex CLI
// Command: codex exec -m {model} --skip-git-repo-check   (prompt on STDIN)
//
// The prompt goes on stdin, never as a positional argument. On Windows the
// `codex` on PATH is npm's codex.cmd shim, which Go launches via cmd.exe, and
// cmd.exe stops reading an argument at the first newline. A multi-line prompt
// (any -f file, any piped stdin, any judge rubric) reached codex as its first
// line only; codex then "loaded the context" and asked what to work on
// (observed 2026-09-12 against a Praxis judge prompt that began "<context>").
// Stdin also sidesteps the 32K command-line limit. `codex exec` reads the
// prompt from stdin when no positional prompt is given.
func (p *OpenAIProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := []string{"exec", "-m", model, "--skip-git-repo-check"}
	output, err := runCommand(ctx, "codex", args, strings.NewReader(prompt))
	duration := time.Since(start)

	return output, duration, nil, err
}
