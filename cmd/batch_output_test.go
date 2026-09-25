package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
)

// setBatchFlags points the batch globals at a temp run and restores them.
func setBatchFlags(t *testing.T, input, output string, resume bool, judge string) {
	t.Helper()
	oldBatch, oldOut, oldResume, oldJudge := flagBatch, flagOutput, flagResume, flagJudge
	oldSkip, oldGeneral, oldCheap := flagSkipPreflight, flagGeneral, flagCheap
	flagBatch, flagOutput, flagResume, flagJudge = input, output, resume, judge
	flagSkipPreflight, flagGeneral, flagCheap = true, true, true
	t.Cleanup(func() {
		flagBatch, flagOutput, flagResume, flagJudge = oldBatch, oldOut, oldResume, oldJudge
		flagSkipPreflight, flagGeneral, flagCheap = oldSkip, oldGeneral, oldCheap
	})
}

// TestFailedBatchSetupLeavesOutputAndCheckpointAlone defends --resume against
// a lost-results sequence: run 1 is interrupted, leaving out.jsonl plus a
// checkpoint listing its ids; run 2 omits --resume and fails during setup
// (a bad judge, a missing key). Run 2 used to truncate out.jsonl BEFORE setup
// while the checkpoint was only cleared inside setup, so a failed run 2 left
// an empty output next to a checkpoint that still listed run 1's ids, and a
// run 3 with --resume skipped them: those results were gone, exit 0.
func TestFailedBatchSetupLeavesOutputAndCheckpointAlone(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.jsonl")
	out := filepath.Join(dir, "out.jsonl")
	ckpt := out + ".checkpoint"
	mustWrite(t, in, `{"id":"A","prompt":"hi"}`+"\n")
	mustWrite(t, out, `{"id":"A","verdict":"YES"}`+"\n")
	mustWrite(t, ckpt, "A\n")
	t.Setenv("GEMINI_API_KEY", "fake-key-for-registry-only")

	setBatchFlags(t, in, out, false, "no-such-judge")
	err := runBatchMode(rootCmd, config.DefaultConfig(), []string{"gemini"}, "", nil, nil, nil)
	if err == nil {
		t.Fatal("expected setup to fail on an unknown judge")
	}
	if got := mustRead(t, out); !strings.Contains(got, `"id":"A"`) {
		t.Fatalf("a failed setup truncated the previous output; out.jsonl is now %q", got)
	}
	if got := mustRead(t, ckpt); !strings.Contains(got, "A") {
		t.Fatalf("a failed setup changed the checkpoint; it is now %q", got)
	}
}

// TestResumeNeedsARealOutputFile: the checkpoint lives next to -o, so with no
// -o (or -o -) --resume re-ran and re-paid for every item without a word, and
// -o - created a file literally named "-.checkpoint" in the cwd.
func TestResumeNeedsARealOutputFile(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.jsonl")
	mustWrite(t, in, `{"id":"A","prompt":"hi"}`+"\n")
	t.Setenv("GEMINI_API_KEY", "fake-key-for-registry-only")
	for _, o := range []string{"", "-"} {
		setBatchFlags(t, in, o, true, "no-such-judge")
		err := runBatchMode(rootCmd, config.DefaultConfig(), []string{"gemini"}, "", nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "--resume") {
			t.Fatalf("-o %q with --resume: got %v, want an error explaining --resume needs -o", o, err)
		}
	}
	if _, err := os.Stat("-.checkpoint"); err == nil {
		os.Remove("-.checkpoint")
		t.Fatal(`-o - created a file named "-.checkpoint"`)
	}
}

func mustWrite(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
