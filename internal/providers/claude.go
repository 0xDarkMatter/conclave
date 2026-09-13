package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClaudeProvider implements the Claude Code CLI
type ClaudeProvider struct {
	baseProvider
}

// NewClaudeProvider creates a new Claude provider
func NewClaudeProvider() *ClaudeProvider {
	return &ClaudeProvider{
		baseProvider: baseProvider{
			name:         "claude",
			defaultModel: "claude-opus-5",
			command:      "claude",
		},
	}
}

// claudeJSONOutput represents Claude's JSON output structure
type claudeJSONOutput struct {
	Type         string  `json:"type"`     // "result" on the final envelope
	IsError      bool    `json:"is_error"` // true when Result is an error message, not an answer
	Result       string  `json:"result"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Preflight checks Claude CLI auth status without consuming tokens.
func (p *ClaudeProvider) Preflight(ctx context.Context) error {
	output, err := runCommand(ctx, "claude", []string{"auth", "status"}, nil)
	if err != nil {
		return fmt.Errorf("claude auth check failed: %w", err)
	}

	loggedIn, err := parseClaudeAuthStatus(output)
	if err != nil {
		return err
	}
	if !loggedIn {
		return fmt.Errorf("claude is not logged in")
	}
	return nil
}

// claudeIsolationArgs keep a panel query independent of wherever conclave
// happens to be invoked. Without them `claude --print` behaves like an
// interactive session started in the caller's directory: it loads that
// project's settings and hooks, and every configured MCP server. Observed
// 2026-09-13: a bare "hi" answered with a description of the caller's git
// worktree, citing a startup hook, and MCP handshakes added seconds plus the
// stdout noise that broke JSON parsing in the first place. For a judge panel
// that is answer contamination. Each flag is load-bearing (ADR-013):
//   - --strict-mcp-config with no --mcp-config: zero MCP servers.
//   - --setting-sources user: no project/local settings, hence no hooks.
//   - --no-session-persistence: a panel query is not a resumable session.
//
// Deliberately NOT --bare: it does all of the above but also disables OAuth,
// which would push claude off the subscription onto an API key (Gotcha 7).
// The caller's CLAUDE.md is skipped by running in claudeWorkDir instead.
var claudeIsolationArgs = []string{
	"--strict-mcp-config",
	"--setting-sources", "user",
	"--no-session-persistence",
}

// claudeWorkDir is the neutral working directory for `claude --print`: an
// empty directory conclave owns, so CLAUDE.md auto-discovery finds nothing.
// Falls back to the caller's cwd only if the directory cannot be created;
// the isolation flags still apply then. Explicit context reaches the panel
// through -f / stdin, never through the cwd.
func claudeWorkDir() string {
	dir := filepath.Join(os.TempDir(), "conclave-claude-cwd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}

// Query executes a prompt using Claude CLI with JSON output for metrics
// Command: claude --print "{prompt}" --model {model} --output-format json <isolation flags>
func (p *ClaudeProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()
	args := append([]string{
		"--print", prompt,
		"--model", model,
		"--output-format", "json",
	}, claudeIsolationArgs...)
	// keepStdoutOnErr: on an API error (unknown model, 404, quota) the CLI
	// exits 1 but still writes its envelope to stdout, and that envelope's
	// `result` is the only human-readable message. runCommand would discard
	// it and leave the caller with "exit status 1".
	output, err := runCommandWith(ctx, "claude", args, cmdOptions{dir: claudeWorkDir(), keepStdoutOnErr: true})
	duration := time.Since(start)

	if err != nil {
		if env, ok := parseClaudeJSONOutput(output); ok && env.IsError && env.Result != "" {
			return "", duration, nil, fmt.Errorf("%s: %w", env.Result, err)
		}
		return "", duration, nil, err
	}

	// Parse JSON to extract response and metrics. The envelope is located, not
	// assumed: the claude CLI prints MCP diagnostics on STDOUT ahead of it
	// ("Client.listTools() called but server does not advertise tools
	// capability - returning empty list", 2026-09-13), and a strict Unmarshal
	// of the whole buffer then fails, leaking the raw noise + envelope to the
	// caller as the "answer". Pinned by TestClaudeCLIIgnoresLeadingNoiseBeforeJSON.
	result, ok := parseClaudeJSONOutput(output)
	if !ok {
		// No envelope anywhere (e.g. --output-format ignored): return raw output
		return output, duration, nil, nil
	}
	if result.IsError {
		// Exit 0 with is_error is not observed today (the CLI exits 1), but an
		// error message must never be handed downstream as a judged answer.
		return "", duration, nil, fmt.Errorf("claude: %s", result.Result)
	}

	metrics := &Metrics{
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		CostUSD:      result.TotalCostUSD,
	}

	return result.Result, duration, metrics, nil
}

// parseClaudeJSONOutput finds the claude CLI's result envelope inside output
// that may carry non-JSON noise around it and decodes it.
//
// Two passes over the candidates from findJSONObject: first the object with
// "type":"result" (what the CLI emits today, and unambiguous), then any object
// carrying a "result" key (older envelopes, and the shape the tests pin).
// The preference order matters: an MCP log line that happens to be a JSON
// object with a "result" field must lose to the real envelope. A candidate
// that matches a pass but does not decode into claudeJSONOutput (a non-string
// "result") is rejected inside the scan so the next candidate is tried,
// rather than aborting the pass.
//
// Returns ok=false when no such object exists anywhere in output.
func parseClaudeJSONOutput(output string) (claudeJSONOutput, bool) {
	isEnvelope := func(fields map[string]json.RawMessage) bool {
		return string(fields["type"]) == `"result"`
	}
	hasResult := func(fields map[string]json.RawMessage) bool {
		_, ok := fields["result"]
		return ok
	}
	var result claudeJSONOutput
	for _, matches := range []func(map[string]json.RawMessage) bool{isEnvelope, hasResult} {
		_, ok := findJSONObject(output, func(raw json.RawMessage, fields map[string]json.RawMessage) bool {
			if !matches(fields) {
				return false
			}
			result = claudeJSONOutput{}
			return json.Unmarshal(raw, &result) == nil
		})
		if ok {
			return result, true
		}
	}
	return claudeJSONOutput{}, false
}

// parseClaudeAuthStatus reads `claude auth status` output, which is
// pretty-printed multi-line JSON and shares Query's exposure to stray
// diagnostics on stdout. Shared by Preflight and SubscriptionLoggedIn so the
// two cannot drift.
func parseClaudeAuthStatus(output string) (bool, error) {
	raw, ok := findJSONObject(output, func(_ json.RawMessage, fields map[string]json.RawMessage) bool {
		_, ok := fields["loggedIn"]
		return ok
	})
	if !ok {
		return false, fmt.Errorf("could not parse claude auth status: no loggedIn object in %q", truncate(output, 200))
	}
	var status struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return false, fmt.Errorf("could not parse claude auth status: %w", err)
	}
	return status.LoggedIn, nil
}

// findJSONObject scans output for JSON objects and returns the raw bytes of
// the first one that accept approves. accept sees both the raw object and its
// top-level fields, so it can reject on shape (missing key) or on a failed
// typed decode and let the scan continue.
//
// Every '{' is tried as a candidate start, so noise before, after, or between
// objects is skipped, as is an unbalanced brace inside a log line. A candidate
// is only handed to accept if it decodes as a complete object; nested objects
// are reached only after their parent was rejected, since the parent's '{'
// comes first. json.Decoder rather than Unmarshal so bytes after the object
// (another noise line, CRLF) do not fail the decode; InputOffset bounds the
// returned slice to the object itself.
//
// Cost is linear in the common case (the first '{' is the envelope). The worst
// case, output with many braces and no acceptable object, is bounded by one
// failed decode per brace, each of which stops at the first bad token.
func findJSONObject(output string, accept func(json.RawMessage, map[string]json.RawMessage) bool) (json.RawMessage, bool) {
	for i := 0; i < len(output); i++ {
		start := strings.IndexByte(output[i:], '{')
		if start < 0 {
			break
		}
		i += start

		dec := json.NewDecoder(strings.NewReader(output[i:]))
		var fields map[string]json.RawMessage
		if err := dec.Decode(&fields); err != nil {
			continue
		}
		raw := json.RawMessage(output[i : i+int(dec.InputOffset())])
		if !accept(raw, fields) {
			continue
		}
		return raw, true
	}
	return nil, false
}

// truncate shortens s for error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
