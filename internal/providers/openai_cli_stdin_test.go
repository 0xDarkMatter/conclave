package providers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodexReceivesMultiLinePromptIntact defends against the Windows npm-shim
// truncation: `codex` on PATH is codex.cmd, Go launches it through cmd.exe, and
// cmd.exe stops reading a positional argument at the first newline. Passing
// the prompt on stdin is the fix; this test installs a fake `codex` that echoes
// its stdin and asserts every line of a multi-line prompt comes back.
func TestCodexReceivesMultiLinePromptIntact(t *testing.T) {
	dir := t.TempDir()
	// A POSIX shim and a cmd.exe shim so the test runs on every CI leg. The
	// .cmd variant deliberately goes through cmd.exe, the exact path that
	// truncated real prompts, so a regression to positional args fails here.
	writeExec(t, filepath.Join(dir, "codex"), "#!/bin/sh\ncat\n")
	writeExec(t, filepath.Join(dir, "codex.cmd"), "@echo off\r\nfindstr /r \".*\"\r\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	prompt := "<context>\nline two of the prompt\nline three: the judge rubric\n"
	out, _, _, err := NewOpenAIProvider().Query(context.Background(), prompt, "test-model")
	if err != nil {
		t.Fatalf("fake codex failed: %v (output %q)", err, out)
	}
	for _, want := range []string{"<context>", "line two of the prompt", "line three: the judge rubric"} {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt line %q did not reach codex; got %q", want, out)
		}
	}
}

// writeExec writes an executable shim. Both the POSIX and the .cmd variant are
// always written; PATH resolution picks the right one per platform.
func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
