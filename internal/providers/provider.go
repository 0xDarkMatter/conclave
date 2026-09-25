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
	// Transport is "cli" or "api": the path this response actually took
	// (ADR-012). It is what gates per-response pricing now that one panel can
	// mix subscription-billed CLIs with metered API calls; a global "API mode"
	// flag can no longer answer "was this one billed?". Empty means unknown
	// (a provider built outside the registry), which prices as nothing.
	Transport string `json:"transport,omitempty"`
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

// cliWaitDelay bounds how long Run() waits for a killed CLI's pipes to close.
// Without it a deadline did not bound an npm .cmd shim at all: cmd.exe was
// killed, node kept the inherited stdout open, and Run() waited for node to
// finish on its own (19s past a 1s timeout, TestCLITimeoutBoundsShimGrandchild).
const cliWaitDelay = 2 * time.Second

// newCLICommand is the only place a provider subprocess is constructed, so
// every CLI call (queries, preflights, subscription probes) gets the same
// cancellation contract: the whole process tree dies with the context, and
// Run() returns within cliWaitDelay of that even if a grandchild clings on.
func newCLICommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Cancel = func() error { return killTree(cmd) }
	cmd.WaitDelay = cliWaitDelay
	return cmd
}

// withDeadline makes a context-ended run say so. A killed child otherwise
// surfaces as "exit status 1", which reads as a CLI bug rather than a timeout.
func withDeadline(ctx context.Context, name string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w (%v)", name, ctxErr, err)
	}
	return err
}

// runCommandCombined runs a command and returns stdout+stderr together plus the
// exit error. For status probes (codex login status prints to stderr on
// success) where the stream split of runCommand hides the answer.
func runCommandCombined(ctx context.Context, name string, args ...string) (string, error) {
	cmd := newCLICommand(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		err = withDeadline(ctx, name, err)
	}
	return strings.TrimSpace(string(out)), err
}

// cmdOptions tunes runCommandWith. The zero value is runCommand's behaviour.
type cmdOptions struct {
	stdin    io.Reader
	extraEnv []string // KEY=VALUE pairs appended to the inherited environment; later wins
	dir      string   // working directory; "" inherits the parent's
	// keepStdoutOnErr returns a failing command's stdout instead of "". For
	// CLIs that write a structured error envelope to stdout and exit non-zero
	// (claude --output-format json does), where the envelope carries the
	// message the user needs. The error still includes stderr either way.
	keepStdoutOnErr bool
}

// runCommandEnv is runCommand with extra KEY=VALUE pairs appended to the
// child's environment (the parent environment is inherited; later entries win).
func runCommandEnv(ctx context.Context, name string, args []string, stdin io.Reader, extraEnv []string) (string, error) {
	return runCommandWith(ctx, name, args, cmdOptions{stdin: stdin, extraEnv: extraEnv})
}

// runCommandWith runs the command with the given options. Every other
// runCommand* is a wrapper over this; keep the exec logic here only.
func runCommandWith(ctx context.Context, name string, args []string, o cmdOptions) (string, error) {
	cmd := newCLICommand(ctx, name, args...)
	if len(o.extraEnv) > 0 {
		cmd.Env = append(os.Environ(), o.extraEnv...)
	}
	if o.dir != "" {
		cmd.Dir = o.dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if o.stdin != nil {
		cmd.Stdin = o.stdin
	}

	err := cmd.Run()
	if err != nil {
		out := ""
		if o.keepStdoutOnErr {
			out = strings.TrimSpace(stdout.String())
		}
		if ctx.Err() != nil {
			return out, withDeadline(ctx, name, err)
		}
		// Include stderr in error message for debugging
		if stderr.Len() > 0 {
			return out, fmt.Errorf("%s: %w\nstderr: %s", name, err, strings.TrimSpace(stderr.String()))
		}
		return out, fmt.Errorf("%s: %w", name, err)
	}

	return strings.TrimSpace(stdout.String()), nil
}
