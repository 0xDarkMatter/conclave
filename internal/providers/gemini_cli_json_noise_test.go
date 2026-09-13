package providers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestGeminiCLIIgnoresStdoutNoiseAroundJSON is the gemini twin of
// TestClaudeCLIIgnoresLeadingNoiseBeforeJSON. gemini-cli has not been caught
// doing this yet; the test exists because the previous parser had the exact
// shape that failed for claude (strict Unmarshal of the whole buffer, raw
// fallthrough), and a diagnostic line on stdout would have handed the noise
// back as the answer.
func TestGeminiCLIIgnoresStdoutNoiseAroundJSON(t *testing.T) {
	const noise = "[WARN] Loaded cached credentials."
	const envelope = `{"response":"OK","stats":{"models":{"gemini-test":{"tokens":{"prompt":7,"candidates":1,"total":8,"cached":2}}}}}`

	dir := t.TempDir()
	writeExec(t, filepath.Join(dir, "gemini"), "#!/bin/sh\necho '"+noise+"'\necho '"+envelope+"'\necho 'bye'\n")
	writeExec(t, filepath.Join(dir, "gemini.cmd"), "@echo off\r\necho "+noise+"\r\necho "+envelope+"\r\necho bye\r\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, _, metrics, err := NewGeminiProvider().Query(context.Background(), "reply with exactly: OK", "gemini-test")
	if err != nil {
		t.Fatalf("fake gemini failed: %v (%q)", err, out)
	}
	if out != "OK" {
		t.Fatalf("expected the envelope's response only, got %q", out)
	}
	if metrics == nil || metrics.InputTokens != 7 || metrics.OutputTokens != 1 || metrics.CacheTokens != 2 {
		t.Fatalf("metrics not extracted from the envelope: %+v", metrics)
	}
}
