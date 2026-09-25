package providers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaudeCLIPromptOnStdinInAFreshEmptyDir defends two things.
//
// The prompt travels on stdin. As an argument it failed two ways: Windows caps
// a command line at 32,767 characters, and the default judge's prompt is every
// panel answer concatenated, so a long panel was paid for and then the judge
// could not even start ("The filename or extension is too long"); and a prompt
// opening with "-" (a markdown bullet, YAML front matter) reads as a flag.
//
// The cwd is a fresh empty directory per query, removed afterwards. ADR-013
// promises "an empty directory conclave owns"; a fixed shared path was only
// empty until something wrote a CLAUDE.md into it, after which every panel and
// judge query on the machine would auto-discover it.
func TestClaudeCLIPromptOnStdinInAFreshEmptyDir(t *testing.T) {
	dir := t.TempDir()
	writeExec(t, filepath.Join(dir, "claude"),
		"#!/bin/sh\nprintf 'ARG:%s\\n' \"$@\"\necho \"CWD=$(pwd)\"\necho LS-BEGIN\nls -A\necho LS-END\ncat\n")
	writeExec(t, filepath.Join(dir, "claude.cmd"),
		"@echo off\r\nfor %%a in (%*) do echo ARG:%%~a\r\necho CWD=%CD%\r\necho LS-BEGIN\r\ndir /b /a 2>nul\r\necho LS-END\r\nfindstr /r \".*\"\r\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var b strings.Builder
	b.WriteString("- leading bullet that looks like a flag\n")
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&b, "PROMPT-LINE-%04d padding padding padding\n", i)
	}
	b.WriteString("PROMPT-END")
	prompt := b.String() // ~41K chars, past the 32,767 argv limit

	out, _, _, err := NewClaudeProvider().Query(context.Background(), prompt, "m")
	if err != nil {
		t.Fatalf("fake claude failed: %v", err)
	}

	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	var cwd string
	inLS := false
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "ARG:") && (strings.Contains(l, "PROMPT-LINE") || strings.Contains(l, "leading bullet")):
			t.Fatalf("prompt text reached argv: %.80q", l)
		case strings.HasPrefix(l, "CWD="):
			cwd = strings.TrimPrefix(l, "CWD=")
		case l == "LS-BEGIN":
			inLS = true
		case l == "LS-END":
			inLS = false
		case inLS && strings.TrimSpace(l) != "":
			t.Fatalf("claude's working directory was not empty; found %q", l)
		}
	}
	if !strings.Contains(out, "- leading bullet") || !strings.Contains(out, "PROMPT-LINE-0999") || !strings.Contains(out, "PROMPT-END") {
		t.Fatalf("prompt did not arrive whole on stdin (len out %d)", len(out))
	}
	if cwd == "" || !strings.Contains(cwd, "conclave-claude-cwd") {
		t.Fatalf("claude did not run in a conclave-owned directory: %q", cwd)
	}
	if _, statErr := os.Stat(cwd); !os.IsNotExist(statErr) {
		t.Fatalf("the per-query directory %s was not removed after the query", cwd)
	}
}
