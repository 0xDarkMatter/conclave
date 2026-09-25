package cmd

import (
	"strings"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
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
	// Set through cobra, as the command line does: a flag the user typed is
	// "Changed", which is what stops config.yaml from filling it in.
	cases := []struct {
		name, flag, value, want string
	}{
		{"workers zero", "workers", "0", "--workers"},
		{"workers negative", "workers", "-1", "--workers"},
		{"timeout zero", "timeout", "0", "--timeout"},
		{"timeout negative", "timeout", "-5", "--timeout"},
		{"budget negative", "budget", "-1", "--budget"},
		{"max-context zero", "max-context", "0", "--max-context"},
	}
	// Isolate from the real user's config.yaml / CONCLAVE_TIMEOUT: config now
	// fills in unset flags before validation runs.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CONCLAVE_TIMEOUT", "")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flagWorkers, flagTimeout, flagBudget, flagMaxContext = 5, 60, 0, 500000
			t.Cleanup(func() {
				flagWorkers, flagTimeout, flagBudget, flagMaxContext = 5, 60, 0, 500000
				rootCmd.Flags().Lookup(tc.flag).Changed = false
			})
			if err := rootCmd.Flags().Set(tc.flag, tc.value); err != nil {
				t.Fatalf("set --%s=%s: %v", tc.flag, tc.value, err)
			}
			err := runConclave(rootCmd, []string{"openai", "x"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want a usage error naming %s", err, tc.want)
			}
		})
	}
}

// TestConfigDefaultsApplyUnlessTheFlagWasGiven: README documents
// default_judge and timeout_seconds in config.yaml, and max_context_size is a
// config key, but nothing read them; the flag defaults won every time. The
// documented precedence is flag > environment > config file > built-in.
func TestConfigDefaultsApplyUnlessTheFlagWasGiven(t *testing.T) {
	oldT, oldJ, oldM := flagTimeout, flagJudge, flagMaxContext
	t.Cleanup(func() {
		flagTimeout, flagJudge, flagMaxContext = oldT, oldJ, oldM
		for _, n := range []string{"timeout", "judge", "max-context"} {
			rootCmd.Flags().Lookup(n).Changed = false
		}
	})
	cfg := config.DefaultConfig()
	cfg.TimeoutSeconds, cfg.DefaultJudge, cfg.MaxContextSize = 7, "gemini", 1234

	flagTimeout, flagJudge, flagMaxContext = 60, "claude", 500000
	applyConfigDefaults(rootCmd, cfg)
	if flagTimeout != 7 || flagJudge != "gemini" || flagMaxContext != 1234 {
		t.Fatalf("config values not applied: timeout=%d judge=%q max-context=%d", flagTimeout, flagJudge, flagMaxContext)
	}

	_ = rootCmd.Flags().Set("timeout", "9")
	_ = rootCmd.Flags().Set("judge", "openai")
	_ = rootCmd.Flags().Set("max-context", "999")
	applyConfigDefaults(rootCmd, cfg)
	if flagTimeout != 9 || flagJudge != "openai" || flagMaxContext != 999 {
		t.Fatalf("an explicit flag lost to config: timeout=%d judge=%q max-context=%d", flagTimeout, flagJudge, flagMaxContext)
	}
}

// TestSurplusPositionalArgsAreRejected: Args had no upper bound, and runConclave
// reads only args[1] (or args[0] with --all). An unquoted prompt was cut to its
// first word, `--cache 6h` turned "6h" into a silently ignored argument, and
// `--all gemini "the prompt"` sent the word "gemini" as the prompt.
func TestSurplusPositionalArgsAreRejected(t *testing.T) {
	cases := []struct {
		name  string
		flags map[string]string
		args  []string
		ok    bool
	}{
		{"providers + prompt", nil, []string{"gemini", "hi"}, true},
		{"unquoted prompt", nil, []string{"gemini", "what", "is", "this"}, false},
		{"stray cache ttl", nil, []string{"gemini", "hi", "6h"}, false},
		{"--all with one arg", map[string]string{"all": "true"}, []string{"hi"}, true},
		{"--all with providers too", map[string]string{"all": "true"}, []string{"gemini", "hi"}, false},
		{"--batch providers only", map[string]string{"batch": "in.jsonl"}, []string{"gemini"}, true},
		{"--batch providers + prompt", map[string]string{"batch": "in.jsonl"}, []string{"gemini", "p"}, true},
		{"--batch surplus", map[string]string{"batch": "in.jsonl"}, []string{"gemini", "p", "x"}, false},
		{"--batch --all prompt", map[string]string{"batch": "in.jsonl", "all": "true"}, []string{"p"}, true},
		{"--batch --all surplus", map[string]string{"batch": "in.jsonl", "all": "true"}, []string{"gemini", "p"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.flags {
				if err := rootCmd.Flags().Set(k, v); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				flagAll, flagBatch = false, ""
				rootCmd.Flags().Lookup("all").Changed = false
				rootCmd.Flags().Lookup("batch").Changed = false
			})
			err := rootCmd.Args(rootCmd, tc.args)
			if tc.ok && err != nil {
				t.Fatalf("valid args %q rejected: %v", tc.args, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("surplus args %q accepted", tc.args)
			}
		})
	}
}
