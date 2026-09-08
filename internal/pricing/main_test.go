package pricing

import (
	"os"
	"testing"
)

// TestMain isolates this package's tests from the ambient environment.
//
// Load() honours CONCLAVE_NO_PRICING and CONCLAVE_PRICING_TTL, so a developer
// with either exported saw a confusing cascade of failures that had nothing to
// do with their change. That is a real trap now that `make check` documents
// CONCLAVE_NO_PRICING=1 as the way to skip the catalog step: setting it made
// the gate fail in the test step instead, pointing at the wrong thing.
//
// The tests here pass explicit Options (test URL, temp cache dir), so they are
// meant to be independent of ambient config; this makes that true.
func TestMain(m *testing.M) {
	for _, v := range []string{"CONCLAVE_NO_PRICING", "CONCLAVE_PRICING_TTL", "CONCLAVE_OPENROUTER_MODELS_URL"} {
		_ = os.Unsetenv(v)
	}
	os.Exit(m.Run())
}
