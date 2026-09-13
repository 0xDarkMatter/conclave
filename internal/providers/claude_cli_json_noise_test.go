package providers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// claudeMCPNoise is the exact line the claude CLI printed on STDOUT ahead of
// its JSON envelope in a live run on 2026-09-13
// (`conclave claude@cli "reply with exactly: OK" --no-judge --json`). A strict
// Unmarshal of the whole buffer failed on it and the caller received the noise
// plus the raw envelope as the "answer".
const claudeMCPNoise = "Client.listTools() called but server does not advertise tools capability - returning empty list"

const claudeEnvelope = `{"type":"result","duration_api_ms":1234,"result":"OK","total_cost_usd":0.0042,"usage":{"input_tokens":11,"output_tokens":2}}`

// TestClaudeCLIIgnoresLeadingNoiseBeforeJSON installs a fake `claude` that
// reproduces that output byte-for-byte and asserts the provider still returns
// only the `result` string, with metrics intact.
func TestClaudeCLIIgnoresLeadingNoiseBeforeJSON(t *testing.T) {
	dir := t.TempDir()
	// POSIX shim and cmd.exe shim so the test runs on every CI leg; PATH
	// resolution picks the right one per platform (see writeExec).
	writeExec(t, filepath.Join(dir, "claude"),
		"#!/bin/sh\nprintf '%s\n' '"+claudeMCPNoise+"'\nprintf '%s\n' '"+claudeEnvelope+"'\n")
	writeExec(t, filepath.Join(dir, "claude.cmd"),
		"@echo off\r\necho "+claudeMCPNoise+"\r\necho "+claudeEnvelope+"\r\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

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

// TestParseClaudeJSONOutputLocatesEnvelope covers the shapes the locator must
// survive beyond the live incident: clean output, noise on both sides,
// structured (JSON-shaped) noise, a brace inside the noise, and no envelope.
func TestParseClaudeJSONOutputLocatesEnvelope(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantOK     bool
		wantResult string
	}{
		{"clean envelope", claudeEnvelope, true, "OK"},
		{"leading noise line", claudeMCPNoise + "\n" + claudeEnvelope, true, "OK"},
		{"trailing noise line", claudeEnvelope + "\n" + claudeMCPNoise, true, "OK"},
		{"noise on both sides", claudeMCPNoise + "\n" + claudeEnvelope + "\n[warn] shutting down mcp", true, "OK"},
		{"JSON-shaped warning before envelope", `{"level":"warn","msg":"no tools"}` + "\n" + claudeEnvelope, true, "OK"},
		{"unbalanced brace in noise", "config {broken\n" + claudeEnvelope, true, "OK"},
		{"result containing braces", `{"result":"use {x} and {y}","usage":{"input_tokens":1,"output_tokens":1}}`, true, "use {x} and {y}"},
		{"no envelope at all", claudeMCPNoise, false, ""},
		{"empty", "", false, ""},
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
		})
	}
}
