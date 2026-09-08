package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Provider defines the interface for LLM providers
type Provider interface {
	Name() string
	DefaultModel() string
	Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error)
	IsAvailable() bool
}

// Preflighter is an optional interface for pre-query auth validation.
// Checks must be fast and must not consume API tokens.
type Preflighter interface {
	Preflight(ctx context.Context) error
}

// PreflightResult holds the result of a provider's preflight check.
type PreflightResult struct {
	Provider    string
	OK          bool
	Error       error
	Remediation string
}

// Metrics holds optional usage metrics from providers
type Metrics struct {
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CacheTokens  int     `json:"cache_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	// Cached marks a response served from conclave's own on-disk response
	// store (internal/cache), not from the provider. It is the signal every
	// cost path uses to charge nothing, and it is distinct from CacheTokens,
	// which counts a VENDOR-side prompt-cache hit on a real billed call.
	Cached bool `json:"cached,omitempty"`
}

// Response holds a provider's query result
type Response struct {
	Provider string        `json:"provider"`
	Model    string        `json:"model"`
	Status   string        `json:"status"` // "success" or "error"
	Response string        `json:"response,omitempty"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration_ms"`
	Metrics  *Metrics      `json:"metrics,omitempty"`
	// Cached mirrors Metrics.Cached so renderers do not have to nil-check.
	Cached bool `json:"cached,omitempty"`
}

// baseProvider provides common functionality
type baseProvider struct {
	name         string
	defaultModel string
	command      string
}

func (p *baseProvider) Name() string {
	return p.name
}

func (p *baseProvider) DefaultModel() string {
	return p.defaultModel
}

func (p *baseProvider) IsAvailable() bool {
	_, err := exec.LookPath(p.command)
	return err == nil
}

// runCommand executes an external command with optional stdin
func runCommand(ctx context.Context, name string, args []string, stdin io.Reader) (string, error) {
	return runCommandEnv(ctx, name, args, stdin, nil)
}

// runCommandCombined runs a command and returns stdout+stderr together plus the
// exit error. For status probes (codex login status prints to stderr on
// success) where the stream split of runCommand hides the answer.
func runCommandCombined(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// runCommandEnv is runCommand with extra KEY=VALUE pairs appended to the
// child's environment (the parent environment is inherited; later entries win).
func runCommandEnv(ctx context.Context, name string, args []string, stdin io.Reader, extraEnv []string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if stdin != nil {
		cmd.Stdin = stdin
	}

	err := cmd.Run()
	if err != nil {
		// Include stderr in error message for debugging
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%s: %w\nstderr: %s", name, err, strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("%s: %w", name, err)
	}

	return strings.TrimSpace(stdout.String()), nil
}
