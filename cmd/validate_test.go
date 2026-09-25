package cmd

import (
	"strings"
	"testing"
)

// TestNumericFlagsAreValidatedBeforeAnyWork defends against nonsense values
// that used to misbehave silently or badly (reproduced 2026-09-25):
//   - --workers 0 started no workers, so the feeder blocked forever;
//   - --workers -1 panicked in make(chan) and exited 2, the code reserved
//     for model drift (cmd/exit.go);
//   - -t 0 or negative expired every provider's context instantly and
//     reported "all providers failed" instead of a bad flag;
//   - --budget -1 was treated as uncapped without a word;
//   - --max-context 0 truncated stdin to a hidden 100KB default but refused
//     every -f file.
func TestNumericFlagsAreValidatedBeforeAnyWork(t *testing.T) {
	cases := []struct {
		name string
		set  func()
		want string
	}{
		{"workers zero", func() { flagWorkers = 0 }, "--workers"},
		{"workers negative", func() { flagWorkers = -1 }, "--workers"},
		{"timeout zero", func() { flagTimeout = 0 }, "--timeout"},
		{"timeout negative", func() { flagTimeout = -5 }, "--timeout"},
		{"budget negative", func() { flagBudget = -1 }, "--budget"},
		{"max-context zero", func() { flagMaxContext = 0 }, "--max-context"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flagWorkers, flagTimeout, flagBudget, flagMaxContext = 5, 60, 0, 500000
			t.Cleanup(func() { flagWorkers, flagTimeout, flagBudget, flagMaxContext = 5, 60, 0, 500000 })
			tc.set()
			err := runConclave(rootCmd, []string{"openai", "x"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want a usage error naming %s", err, tc.want)
			}
		})
	}
}
