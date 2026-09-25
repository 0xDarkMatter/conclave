package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/judge"
)

// TestNoVerdictIsAFailedRun: a judge that errored already exited 1, but one
// that answered with unparseable text (verdict "PARSE_ERROR") exited 0, so a
// script checking $? took a run with no verdict as a success.
func TestNoVerdictIsAFailedRun(t *testing.T) {
	if err := verdictFailure("claude", &judge.Verdict{Result: "YES"}, nil); err != nil {
		t.Fatalf("a real verdict failed the run: %v", err)
	}
	if err := verdictFailure("claude", nil, nil); err != nil {
		t.Fatalf("--no-judge (no verdict, no error) failed the run: %v", err)
	}
	if err := verdictFailure("claude", nil, errors.New("boom")); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("judge error: got %v", err)
	}
	err := verdictFailure("claude", &judge.Verdict{Result: "PARSE_ERROR"}, nil)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("PARSE_ERROR verdict: got %v, want a failed run", err)
	}
}
