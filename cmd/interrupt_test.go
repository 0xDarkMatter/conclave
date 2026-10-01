package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/batch"
)

// TestInterruptedBatchNeverExitsClean: exitCodeFor turns an interruption into
// 130 only when the command returned an error, and the batch summary returned
// one only when items were left UNDISPATCHED. Ctrl-C after the last item was
// dispatched (any batch no bigger than --workers) left Skipped at 0, so the
// run exited 0 over a partial output file.
func TestInterruptedBatchNeverExitsClean(t *testing.T) {
	cases := []struct {
		name  string
		stats batch.Stats
		lost  bool // the output is partial
	}{
		{"all dispatched, one cut short", batch.Stats{Total: 5, Completed: 5, Cancelled: true, CutShort: 1}, true},
		{"undispatched items", batch.Stats{Total: 5, Completed: 2, Skipped: 3, Cancelled: true}, true},
		{"interrupted, every in-flight item finished", batch.Stats{Total: 2, Completed: 2, Succeeded: 2, Cancelled: true}, false},
	}
	for _, c := range cases {
		var err error
		out := captureStderr(t, func() { err = batchInterruptedError(&c.stats) })
		if err == nil {
			t.Errorf("%s: no error, so an interrupted batch would exit 0", c.name)
			continue
		}
		if partial := strings.Contains(out, "partial"); partial != c.lost {
			t.Errorf("%s: summary says partial=%v, want %v:\n%s", c.name, partial, c.lost, out)
		}
	}
	if err := batchInterruptedError(&batch.Stats{Total: 3, Completed: 3, Succeeded: 3}); err != nil {
		t.Fatalf("a clean batch reported an interruption: %v", err)
	}
}

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
