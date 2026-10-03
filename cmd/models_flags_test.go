package cmd

import (
	"strings"
	"testing"
)

// TestModelsFlagCombinationsThatWouldBeIgnored: --json returned before --check
// ran, so `conclave models --check --json` in CI dumped the catalog and exited
// 0 whatever the drift. A provider argument was likewise ignored by both.
func TestModelsFlagCombinationsThatWouldBeIgnored(t *testing.T) {
	cases := []struct {
		name        string
		json, check bool
		args        []string
		ok          bool
	}{
		{"plain", false, false, nil, true},
		{"provider", false, false, []string{"claude"}, true},
		{"json", true, false, nil, true},
		{"check", false, true, nil, true},
		{"json+check", true, true, nil, false},
		{"json+provider", true, false, []string{"claude"}, false},
		{"check+provider", false, true, []string{"claude"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flagModelsJSON, flagModelsCheck = tc.json, tc.check
			t.Cleanup(func() { flagModelsJSON, flagModelsCheck = false, false })
			err := validateModelsFlags(tc.args)
			if tc.ok != (err == nil) {
				t.Fatalf("validateModelsFlags(json=%v check=%v args=%v) = %v", tc.json, tc.check, tc.args, err)
			}
		})
	}
}

// models_decider_rows_print_without_catalog: decider prices come from the hand
// table, so `CONCLAVE_NO_PRICING=1 conclave models jev` must print the jev row
// and exit 0, and the unfiltered listing must still show the DECISION MODELS
// section even though it exits 3 for the missing chat-provider catalog.
func TestModelsDeciderRowsPrintWithoutCatalog(t *testing.T) {
	t.Setenv("CONCLAVE_NO_PRICING", "1")

	var err error
	out := captureStdout(t, func() { err = runModels(modelsCmd, []string{"jev"}) })
	if err != nil {
		t.Fatalf("models jev without a catalog: %v", err)
	}
	if !strings.Contains(out, "DECISION MODELS") || !strings.Contains(out, "jev") {
		t.Fatalf("models jev printed no decider row:\n%s", out)
	}

	out = captureStdout(t, func() { err = runModels(modelsCmd, nil) })
	if code := exitCodeFor(err, false); code != ExitCatalogUnavailable {
		t.Fatalf("unfiltered listing without a catalog: exit %d (%v), want %d", code, err, ExitCatalogUnavailable)
	}
	if !strings.Contains(out, "clef-flash") {
		t.Fatalf("unfiltered listing dropped the decider rows:\n%s", out)
	}
}
