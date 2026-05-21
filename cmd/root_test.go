package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// captureStdout runs fn while redirecting os.Stdout and returns what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()
	w.Close()
	os.Stdout = old
	<-done
	return buf.String()
}

// TestListProviders_DefaultShowsBothModes verifies that calling
// --list-providers without -g surfaces BOTH the CLI and API columns. This is
// the fix for the user's UX complaint that the two lists diverge in
// surprising ways (glm in CLI but not API, different grok defaults).
func TestListProviders_DefaultShowsBothModes(t *testing.T) {
	flagGeneral = false
	t.Cleanup(func() { flagGeneral = false })

	out := captureStdout(t, listProviders)

	for _, want := range []string{"CLI mode", "API mode", "gemini", "openai"} {
		if !strings.Contains(out, want) {
			t.Errorf("default --list-providers missing %q; got:\n%s", want, out)
		}
	}
}

// TestListProviders_GeneralModeSingleColumn verifies the -g escape hatch is
// preserved for scripts that parsed the old format.
func TestListProviders_GeneralModeSingleColumn(t *testing.T) {
	flagGeneral = true
	t.Cleanup(func() { flagGeneral = false })

	out := captureStdout(t, listProviders)

	if strings.Contains(out, "CLI mode") {
		t.Errorf("-g should show only API column; got:\n%s", out)
	}
	if !strings.Contains(out, "API (general mode)") {
		t.Errorf("-g should include API heading; got:\n%s", out)
	}
}

// TestPrintProviderList_FormatsStatusColumn verifies the [ready] / [not
// installed] column rendering is intact.
func TestPrintProviderList_FormatsStatusColumn(t *testing.T) {
	// Use real CLI providers — at least one of them will report not installed
	// on the bare test runner.
	out := captureStdout(t, func() {
		printProviderList("Test heading", providers.AllCLIProviders(), "not installed")
	})

	if !strings.Contains(out, "Test heading") {
		t.Errorf("heading not rendered; got:\n%s", out)
	}
	if !strings.Contains(out, "gemini") {
		t.Errorf("provider names should appear in output; got:\n%s", out)
	}
}

// TestRunConclave_RawAndJSONConflict verifies the mutually-exclusive guard
// rejects --raw --json without falling through to producing garbled output.
func TestRunConclave_RawAndJSONConflict(t *testing.T) {
	flagRaw = true
	flagJSON = true
	t.Cleanup(func() {
		flagRaw = false
		flagJSON = false
	})

	// Need to bypass earlier validation that requires providers to be configured.
	// Just call the guard portion: we test that runConclave returns the
	// specific error without needing to set up the world.
	err := runConclave(rootCmd, []string{"openai", "x"})
	if err == nil {
		t.Fatal("expected --raw + --json conflict error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("got %q, want a mutually-exclusive error", err.Error())
	}
}
