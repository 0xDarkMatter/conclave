package cmd

import "testing"

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
