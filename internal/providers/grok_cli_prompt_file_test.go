package providers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGrokCLIPromptTravelsInAFile defends two argv failures of `grok -p
// <prompt>`: a prompt opening with "-" (markdown bullet, YAML front matter)
// is rejected by grok's parser ("unexpected argument '- ' found"), and a
// prompt past Windows' 32,767-character command line cannot start at all.
// grok has no stdin prompt mode, but it does take --prompt-file. The fake
// grok records its argv and copies the prompt file; the prompt must not be in
// argv, must arrive byte-for-byte, and the temp file must be gone afterwards.
func TestGrokCLIPromptTravelsInAFile(t *testing.T) {
	dir := t.TempDir()
	argvDump := filepath.Join(dir, "argv.txt")
	fileDump := filepath.Join(dir, "prompt-copy.txt")
	t.Setenv("GROK_FAKE_ARGV", argvDump)
	t.Setenv("GROK_FAKE_DUMP", fileDump)
	writeExec(t, filepath.Join(dir, "grok"), `#!/bin/sh
echo "$@" > "$GROK_FAKE_ARGV"
while [ $# -gt 0 ]; do
  if [ "$1" = "--prompt-file" ]; then cp "$2" "$GROK_FAKE_DUMP"; fi
  shift
done
echo '{"role":"assistant","content":"OK"}'
`)
	writeExec(t, filepath.Join(dir, "grok.cmd"), "@echo off\r\n"+
		"echo %* > \"%GROK_FAKE_ARGV%\"\r\n"+
		":loop\r\n"+
		"if \"%~1\"==\"\" goto done\r\n"+
		"if \"%~1\"==\"--prompt-file\" copy /y \"%~2\" \"%GROK_FAKE_DUMP%\" >nul\r\n"+
		"shift\r\n"+
		"goto loop\r\n"+
		":done\r\n"+
		"echo {\"role\":\"assistant\",\"content\":\"OK\"}\r\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	prompt := "- leading bullet that grok's parser reads as a flag\n" + strings.Repeat("body line of the prompt\n", 1500) + "PROMPT-END"
	out, _, _, err := NewGrokProvider().Query(context.Background(), prompt, "test-model")
	if err != nil {
		t.Fatalf("fake grok failed: %v (%q)", err, out)
	}
	if out != "OK" {
		t.Fatalf("assistant content not extracted, got %q", out)
	}

	argv, err := os.ReadFile(argvDump)
	if err != nil {
		t.Fatalf("fake grok did not record argv: %v", err)
	}
	if strings.Contains(string(argv), "leading bullet") || strings.Contains(string(argv), "PROMPT-END") {
		t.Fatalf("prompt text reached grok's argv: %.120q", argv)
	}
	got, err := os.ReadFile(fileDump)
	if err != nil {
		t.Fatalf("grok was not given a --prompt-file: %v (argv %q)", err, argv)
	}
	if string(got) != prompt {
		t.Fatalf("prompt file content differs from the prompt (%d vs %d bytes)", len(got), len(prompt))
	}
	fields := strings.Fields(string(argv))
	for i, f := range fields {
		if f == "--prompt-file" && i+1 < len(fields) {
			if _, statErr := os.Stat(strings.Trim(fields[i+1], `"`)); !os.IsNotExist(statErr) {
				t.Fatalf("temp prompt file %s was left behind", fields[i+1])
			}
		}
	}
}

// TestGrokCLIPreflightDoesNotDemandAPIKey defends the grok CLI route against
// the same mistake codex once had (Gotcha 7): the grok CLI carries its own
// login (grok.com or a deployment key), so gating it on XAI_API_KEY refused a
// working CLI with "XAI_API_KEY is not set" before any query ran.
func TestGrokCLIPreflightDoesNotDemandAPIKey(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("GROK_API_KEY", "")
	if failures := RunPreflight(context.Background(), []Provider{NewGrokProvider()}); len(failures) > 0 {
		t.Fatalf("grok CLI preflight failed without an API key: %v", failures[0].Error)
	}
}
