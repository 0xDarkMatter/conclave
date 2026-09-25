package providers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeminiCLIPromptNeverTouchesTheCommandLine defends against three failures
// of passing the prompt as `-p <prompt>` to gemini.cmd, which Go runs through
// cmd.exe on Windows (measured 2026-09-25):
//   - truncation: cmd.exe ends the argument at the first newline, so any -f or
//     stdin context reached gemini as the single line "<context>";
//   - expansion: %NAME% in a document was replaced with that env var's value
//     (an API key, say) and sent to Google;
//   - injection: an unbalanced quote followed by `& cmd` in a document ran cmd.
//
// The prompt now travels on stdin. The fake gemini echoes its argv and its
// stdin; no prompt text may appear in argv, and the stdin copy must be whole.
func TestGeminiCLIPromptNeverTouchesTheCommandLine(t *testing.T) {
	dir := t.TempDir()
	writeExec(t, filepath.Join(dir, "gemini"), "#!/bin/sh\necho \"ARGS=$*\"\ncat\n")
	writeExec(t, filepath.Join(dir, "gemini.cmd"), "@echo off\r\necho ARGS=%*\r\nfindstr /r \".*\"\r\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	prompt := "<context>\nline two of the file\n</context>\n\nquestion: 12\" pipes & echo INJECTED_CMD %PATH%"
	out, _, _, err := NewGeminiProvider().Query(context.Background(), prompt, "test-model")
	if err != nil {
		t.Fatalf("fake gemini failed: %v (output %q)", err, out)
	}

	var argsLine string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "ARGS=") {
			argsLine = line
		}
		if strings.HasPrefix(line, "INJECTED_CMD") {
			t.Fatalf("prompt text ran as a shell command: %q", line)
		}
	}
	if argsLine == "" {
		t.Fatalf("fake gemini did not report its argv; output %q", out)
	}
	for _, leaked := range []string{"line two", "question", "<context>"} {
		if strings.Contains(argsLine, leaked) {
			t.Fatalf("prompt text %q reached the command line: %q", leaked, argsLine)
		}
	}
	for _, want := range []string{"<context>", "line two of the file", "question: 12", "%PATH%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q did not reach gemini intact on stdin; got %q", want, out)
		}
	}
}
