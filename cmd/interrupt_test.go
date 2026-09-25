package cmd

import (
	"errors"
	"testing"
)

// TestInterruptedRunExits130: Ctrl-C killed the process outright, so the
// deferred cleanup of grok prompt files and claude's temp working directory
// never ran and those leaked on every interrupted run. The first Ctrl-C now
// cancels the run instead, and it must still END as an interruption (130,
// the SIGINT convention), whatever error the cancelled work returned.
func TestInterruptedRunExits130(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		interrupted bool
		want        int
	}{
		{"clean", nil, false, 0},
		{"plain error", errors.New("x"), false, 1},
		{"coded error", withExitCode(ExitDrift, errors.New("drift")), false, ExitDrift},
		{"interrupted, work failed", errors.New("all providers failed: context canceled"), true, ExitInterrupted},
		{"interrupted, judge failed", withExitCode(ExitDrift, errors.New("x")), true, ExitInterrupted},
	}
	for _, c := range cases {
		if got := exitCodeFor(c.err, c.interrupted); got != c.want {
			t.Errorf("%s: exit %d, want %d", c.name, got, c.want)
		}
	}
	if ExitInterrupted != 130 {
		t.Fatalf("ExitInterrupted = %d, want 130", ExitInterrupted)
	}
}
