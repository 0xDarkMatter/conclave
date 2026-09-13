package providers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// claudeMCPNoise is the exact line the claude CLI printed on STDOUT ahead of
// its JSON envelope in a live run on 2026-09-13
// (`conclave claude@cli "reply with exactly: OK" --no-judge --json`). A strict
// Unmarshal of the whole buffer failed on it and the caller received the noise
// plus the raw envelope as the "answer".
const claudeMCPNoise = "Client.listTools() called but server does not advertise tools capability - returning empty list"

const claudeEnvelope = `{"type":"result","duration_api_ms":1234,"result":"OK","total_cost_usd":0.0042,"usage":{"input_tokens":11,"output_tokens":2}}`

// claudeErrorEnvelope is the shape the CLI wrote to stdout (exit 1) for an
// unknown model on 2026-09-13, trimmed to the fields that matter. The
// human-readable message lives only in `result`.
const claudeErrorEnvelope = `{"duration_api_ms":0,"total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0},"is_error":true,"subtype":"success","api_error_status":404,"result":"There's an issue with the selected model (no-such-model-xyz). It may not exist or you may not have access to it.","type":"result"}`

// claudeAuthStatus is the pretty-printed multi-line JSON `claude auth status`
// prints (captured 2026-09-13, identifiers redacted).
const claudeAuthStatus = "{\n  \"loggedIn\": true,\n  \"authMethod\": \"claude.ai\",\n  \"apiProvider\": \"firstParty\",\n  \"subscriptionType\": \"max\"\n}"

// installFakeClaude puts a `claude` shim first on PATH that prints lines to
// stdout and exits with code. Both a POSIX and a cmd.exe shim are written so
// the test runs on every CI leg; PATH resolution picks the right one (see
// writeExec). The .cmd path goes through cmd.exe, which is what emits CRLF,
// so Windows also covers the CRLF variant for free.
func installFakeClaude(t *testing.T, code int, lines ...string) {
	t.Helper()
	dir := t.TempDir()
	var sh, cmd strings.Builder
	sh.WriteString("#!/bin/sh\n")
	cmd.WriteString("@echo off\r\n")
	for _, l := range lines {
		// sh: single-quote the line, escaping embedded quotes ("There's").
		sh.WriteString("printf '%s\\n' '" + strings.ReplaceAll(l, "'", `'\''`) + "'\n")
		cmd.WriteString("echo " + l + "\r\n")
	}
	if code != 0 {
		sh.WriteString("exit " + strconv.Itoa(code) + "\n")
		cmd.WriteString("exit /b " + strconv.Itoa(code) + "\r\n")
	}
	writeExec(t, filepath.Join(dir, "claude"), sh.String())
	writeExec(t, filepath.Join(dir, "claude.cmd"), cmd.String())
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestClaudeCLIIgnoresLeadingNoiseBeforeJSON installs a fake `claude` that
// reproduces the live output byte-for-byte and asserts the provider still
// returns only the `result` string, with metrics intact.
func TestClaudeCLIIgnoresLeadingNoiseBeforeJSON(t *testing.T) {
	installFakeClaude(t, 0, claudeMCPNoise, claudeEnvelope)

	out, _, metrics, err := NewClaudeProvider().Query(context.Background(), "reply with exactly: OK", "test-model")
	if err != nil {
		t.Fatalf("fake claude failed: %v (output %q)", err, out)
	}
	if out != "OK" {
		t.Fatalf("expected the envelope's result only, got %q", out)
	}
	if metrics == nil {
		t.Fatal("metrics were dropped along with the parse")
	}
	if metrics.InputTokens != 11 || metrics.OutputTokens != 2 || metrics.CostUSD != 0.0042 {
		t.Fatalf("metrics not extracted from the envelope: %+v", metrics)
	}
}

// TestClaudeCLIErrorEnvelopeSurfacesMessage: on an API error the CLI exits 1
// but still writes its envelope to stdout. The caller must get the message in
// `result` (the only human-readable diagnostic), not a bare "exit status 1",
// and must never get the message as a successful answer.
func TestClaudeCLIErrorEnvelopeSurfacesMessage(t *testing.T) {
	installFakeClaude(t, 1, claudeMCPNoise, claudeErrorEnvelope)

	out, _, metrics, err := NewClaudeProvider().Query(context.Background(), "hi", "no-such-model-xyz")
	if err == nil {
		t.Fatalf("exit 1 must be an error; got response %q", out)
	}
	if !strings.Contains(err.Error(), "issue with the selected model (no-such-model-xyz)") {
		t.Fatalf("error lost the envelope's result message: %v", err)
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("error lost the underlying exit status: %v", err)
	}
	if out != "" || metrics != nil {
		t.Fatalf("error path must not return a response or metrics; got %q, %+v", out, metrics)
	}
}

// TestClaudeCLIIsErrorWithExitZeroIsStillAnError: not observed in the wild
// (the CLI exits 1), but an envelope flagged is_error must never reach the
// judge as an answer even if the exit code lies.
func TestClaudeCLIIsErrorWithExitZeroIsStillAnError(t *testing.T) {
	installFakeClaude(t, 0, claudeErrorEnvelope)

	out, _, _, err := NewClaudeProvider().Query(context.Background(), "hi", "m")
	if err == nil {
		t.Fatalf("is_error envelope returned as success: %q", out)
	}
	if !strings.Contains(err.Error(), "issue with the selected model") {
		t.Fatalf("error lost the envelope's result message: %v", err)
	}
}

// TestClaudeCLIPlainTextFallsThrough: if the envelope is absent entirely
// (older CLI ignoring --output-format), the raw text is still returned rather
// than swallowed. Metrics are unavailable in that case.
func TestClaudeCLIPlainTextFallsThrough(t *testing.T) {
	installFakeClaude(t, 0, "just a plain answer")

	out, _, metrics, err := NewClaudeProvider().Query(context.Background(), "hi", "m")
	if err != nil {
		t.Fatal(err)
	}
	if out != "just a plain answer" || metrics != nil {
		t.Fatalf("got %q, %+v", out, metrics)
	}
}

// TestClaudePreflightToleratesStdoutNoise: `claude auth status` is
// pretty-printed multi-line JSON and shares Query's exposure to diagnostics on
// stdout. Both Preflight and the subscription probe read it through the same
// parser; a logged-out status must still be reported as such.
func TestClaudePreflightToleratesStdoutNoise(t *testing.T) {
	// The shim prints one line per argument; split the pretty JSON into lines.
	loggedIn := append([]string{claudeMCPNoise}, strings.Split(claudeAuthStatus, "\n")...)
	installFakeClaude(t, 0, loggedIn...)
	if err := NewClaudeProvider().Preflight(context.Background()); err != nil {
		t.Fatalf("Preflight rejected a logged-in status behind noise: %v", err)
	}
	if !SubscriptionLoggedIn(context.Background(), "claude") {
		t.Fatal("SubscriptionLoggedIn rejected a logged-in status behind noise")
	}

	loggedOut := append([]string{claudeMCPNoise}, strings.Split(strings.Replace(claudeAuthStatus, "true", "false", 1), "\n")...)
	installFakeClaude(t, 0, loggedOut...)
	if err := NewClaudeProvider().Preflight(context.Background()); err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("Preflight must report logged-out, got %v", err)
	}
	if SubscriptionLoggedIn(context.Background(), "claude") {
		t.Fatal("SubscriptionLoggedIn must report logged-out")
	}

	installFakeClaude(t, 0, claudeMCPNoise)
	if err := NewClaudeProvider().Preflight(context.Background()); err == nil || !strings.Contains(err.Error(), "could not parse") {
		t.Fatalf("Preflight must fail loudly with no status object, got %v", err)
	}
}

// TestParseClaudeJSONOutputLocatesEnvelope covers the shapes the locator must
// survive beyond the live incident: clean output, noise on both sides,
// structured (JSON-shaped) noise including a decoy with a "result" key, a
// brace inside the noise, braces and an escaped envelope inside the answer,
// pretty-printed and CRLF envelopes, a BOM, and no envelope.
func TestParseClaudeJSONOutputLocatesEnvelope(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantOK     bool
		wantResult string
		wantErr    bool
	}{
		{name: "clean envelope", input: claudeEnvelope, wantOK: true, wantResult: "OK"},
		{name: "leading noise line", input: claudeMCPNoise + "\n" + claudeEnvelope, wantOK: true, wantResult: "OK"},
		{name: "trailing noise line", input: claudeEnvelope + "\n" + claudeMCPNoise, wantOK: true, wantResult: "OK"},
		{name: "trailing noise without newline", input: claudeEnvelope + "garbage {", wantOK: true, wantResult: "OK"},
		{name: "noise on both sides", input: claudeMCPNoise + "\n" + claudeEnvelope + "\n[warn] shutting down mcp", wantOK: true, wantResult: "OK"},
		{name: "JSON-shaped warning before envelope", input: `{"level":"warn","msg":"no tools"}` + "\n" + claudeEnvelope, wantOK: true, wantResult: "OK"},
		{name: "decoy object with result key loses to typed envelope", input: `{"tool":"x","result":"decoy"}` + "\n" + claudeEnvelope, wantOK: true, wantResult: "OK"},
		{name: "nested decoy result loses to typed envelope", input: `{"a":{"result":"decoy"}}` + "\n" + claudeEnvelope, wantOK: true, wantResult: "OK"},
		{name: "unbalanced brace in noise", input: "config {broken\n" + claudeEnvelope, wantOK: true, wantResult: "OK"},
		{name: "brace inside quoted noise", input: `warn: "{" seen in config` + "\n" + claudeEnvelope, wantOK: true, wantResult: "OK"},
		{name: "untyped envelope (older CLI)", input: `{"result":"use {x} and {y}","usage":{"input_tokens":1,"output_tokens":1}}`, wantOK: true, wantResult: "use {x} and {y}"},
		{name: "answer is itself an escaped envelope", input: `{"type":"result","result":"{\"type\":\"result\",\"result\":\"inner\"}"}`, wantOK: true, wantResult: `{"type":"result","result":"inner"}`},
		{name: "answer contains the noise line", input: `{"type":"result","result":"` + claudeMCPNoise + `"}`, wantOK: true, wantResult: claudeMCPNoise},
		{name: "pretty-printed envelope", input: "{\n  \"type\": \"result\",\n  \"result\": \"OK\"\n}\n", wantOK: true, wantResult: "OK"},
		{name: "CRLF around envelope", input: claudeMCPNoise + "\r\n" + claudeEnvelope + "\r\n", wantOK: true, wantResult: "OK"},
		{name: "UTF-8 BOM prefix", input: "ï»¿" + claudeEnvelope, wantOK: true, wantResult: "OK"},
		{name: "unicode in result", input: `{"type":"result","result":"naïve — 日本語 ✓"}`, wantOK: true, wantResult: "naïve — 日本語 ✓"},
		{name: "error envelope decodes with is_error", input: claudeErrorEnvelope, wantOK: true, wantResult: "There's an issue with the selected model (no-such-model-xyz). It may not exist or you may not have access to it.", wantErr: true},
		{name: "result is null", input: `{"type":"result","result":null}`, wantOK: true, wantResult: ""},
		{name: "result is not a string and no other candidate", input: `{"result":42}`, wantOK: false},
		{name: "typed envelope with non-string result falls back to untyped candidate", input: `{"type":"result","result":42}` + "\n" + `{"result":"fallback"}`, wantOK: true, wantResult: "fallback"},
		{name: "no envelope at all", input: claudeMCPNoise, wantOK: false},
		{name: "only an open brace", input: "{", wantOK: false},
		{name: "truncated envelope", input: claudeEnvelope[:len(claudeEnvelope)-10], wantOK: false},
		{name: "empty", input: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseClaudeJSONOutput(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (result %+v)", ok, tt.wantOK, got)
			}
			if got.Result != tt.wantResult {
				t.Fatalf("result = %q, want %q", got.Result, tt.wantResult)
			}
			if got.IsError != tt.wantErr {
				t.Fatalf("is_error = %v, want %v", got.IsError, tt.wantErr)
			}
		})
	}
}

// TestParseClaudeJSONOutputMetricsSurviveNoise: the fix must not regress the
// metrics extraction the envelope was parsed for in the first place.
func TestParseClaudeJSONOutputMetricsSurviveNoise(t *testing.T) {
	got, ok := parseClaudeJSONOutput(claudeMCPNoise + "\n" + claudeEnvelope + "\n" + claudeMCPNoise)
	if !ok {
		t.Fatal("envelope not found")
	}
	if got.Usage.InputTokens != 11 || got.Usage.OutputTokens != 2 || got.TotalCostUSD != 0.0042 {
		t.Fatalf("metrics wrong: %+v", got)
	}
}

// TestFindJSONObjectBoundsToObject: the raw slice handed back must be exactly
// the object, not the object plus whatever followed it, or a later Unmarshal
// of that slice fails on the trailing bytes.
func TestFindJSONObjectBoundsToObject(t *testing.T) {
	raw, ok := findJSONObject("x {\"a\":1} trailing {\"b\":2}", func(json.RawMessage, map[string]json.RawMessage) bool { return true })
	if !ok || string(raw) != `{"a":1}` {
		t.Fatalf("got %q, %v", raw, ok)
	}
}

// TestFindJSONObjectLargeNoiseIsFast guards the worst case the scan comment
// promises: many braces and no acceptable object must still finish in
// negligible time (each candidate decode stops at its first bad token).
func TestFindJSONObjectLargeNoiseIsFast(t *testing.T) {
	noise := strings.Repeat("if (x) { y{ } z{{ ", 20000) // ~80k braces, `{ }` decodes but has no key
	if _, ok := findJSONObject(noise, func(_ json.RawMessage, f map[string]json.RawMessage) bool { _, ok := f["result"]; return ok }); ok {
		t.Fatal("no object expected")
	}
}
