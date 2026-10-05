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

// TestModelsListsWorkersAIFreeAllocationAsAFact: the 10,000 free Neurons a
// day that Workers AI gives every account were invisible, so a listed $0.24/M
// read as what every Clef call costs. The listing must show the allocation in
// the model's own input tokens and say which price decide's cost_usd and the
// frontier use, without claiming one for a decider that has none.
func TestModelsListsWorkersAIFreeAllocationAsAFact(t *testing.T) {
	t.Setenv("CONCLAVE_NO_PRICING", "1")

	var err error
	out := captureStdout(t, func() { err = runModels(modelsCmd, []string{"clef"}) })
	if err != nil {
		t.Fatalf("models clef: %v", err)
	}
	for _, want := range []string{"FREE/DAY", "458K in tok", "10,000 Neurons/day per Cloudflare account", "00:00 UTC", "metered price", "ADR-019"} {
		if !strings.Contains(out, want) {
			t.Errorf("models clef lacks %q:\n%s", want, out)
		}
	}

	out = captureStdout(t, func() { err = runModels(modelsCmd, []string{"jev"}) })
	if err != nil {
		t.Fatalf("models jev: %v", err)
	}
	if strings.Contains(out, "Neurons") || strings.Contains(out, "in tok") {
		t.Errorf("jev has no free allocation, but the listing claims one:\n%s", out)
	}
}
