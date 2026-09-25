package cmd

import "testing"

// TestSetupNeedsATerminalBothWays: only stdin was checked. With a terminal on
// stdin but stdout redirected (`conclave ... > out.txt`, or --json), the
// wizard's prompts went into the output file and the run sat waiting for an
// answer to a question the user never saw.
func TestSetupNeedsATerminalBothWays(t *testing.T) {
	cases := []struct {
		in, out, json bool
		allowed       bool
	}{
		{true, true, false, true},
		{false, true, false, false},
		{true, false, false, false},
		{true, true, true, false},
	}
	for _, c := range cases {
		if got := setupBlockedBy(c.in, c.out, c.json) == ""; got != c.allowed {
			t.Errorf("stdinTTY=%v stdoutTTY=%v json=%v: allowed=%v, want %v", c.in, c.out, c.json, got, c.allowed)
		}
	}
}
