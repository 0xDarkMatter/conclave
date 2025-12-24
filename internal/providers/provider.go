package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

// Metrics holds optional usage metrics from providers
type Metrics struct {
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CacheTokens  int     `json:"cache_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
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
	cmd := exec.CommandContext(ctx, name, args...)

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
