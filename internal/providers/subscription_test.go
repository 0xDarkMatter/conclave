package providers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestSubscriptionLoggedInReadsCodexStatus pins the two codex phrasings that
// matter: "Logged in using ChatGPT" is a live plan, "Not logged in" is not, and
// the second must not match the first as a substring.
func TestSubscriptionLoggedInReadsCodexStatus(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   bool
	}{
		{"pro plan", "Logged in using ChatGPT", true},
		{"logged out", "Not logged in", false},
		{"garbage", "codex: unknown subcommand", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// codex writes the status line to stderr; the shim does the same.
			writeExec(t, filepath.Join(dir, "codex"), "#!/bin/sh\necho \""+tc.status+"\" 1>&2\n")
			writeExec(t, filepath.Join(dir, "codex.cmd"), "@echo off\r\necho "+tc.status+" 1>&2\r\n")
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			if got := SubscriptionLoggedIn(context.Background(), "openai"); got != tc.want {
				t.Fatalf("status %q: got %v, want %v", tc.status, got, tc.want)
			}
		})
	}
	if SubscriptionLoggedIn(context.Background(), "gemini") {
		t.Fatal("gemini has no subscription CLI and must report false")
	}
}
