package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// TestModelOverrideKeyDropsTheTransportSuffix: "-m openai@cli:gpt-5.6-sol"
// and "-m openai:gpt-5.6-sol" must both land on the bare "openai" key the
// registry reads (ADR-012). A malformed suffix is kept verbatim so it matches
// nothing instead of aborting the parse.
func TestModelOverrideKeyDropsTheTransportSuffix(t *testing.T) {
	got := parseModelOverrides([]string{
		"openai@cli:gpt-5.6-sol",
		"claude:claude-opus-5",
		"deepseek/deepseek-v4@api:deepseek/deepseek-v4:free",
		"gemini@sdk:whatever",
		"no-colon",
	})
	want := map[string]string{
		"openai":               "gpt-5.6-sol",
		"claude":               "claude-opus-5",
		"deepseek/deepseek-v4": "deepseek/deepseek-v4:free",
		"gemini@sdk":           "whatever",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("override[%q] = %q, want %q", k, got[k], v)
		}
	}
}

type namedProvider string

func (n namedProvider) Name() string         { return string(n) }
func (n namedProvider) DefaultModel() string { return "m" }
func (n namedProvider) IsAvailable() bool    { return true }
func (n namedProvider) Query(context.Context, string, string) (string, time.Duration, *providers.Metrics, error) {
	return "", 0, nil, nil
}

// TestOutputProviderListUsesBareNames: execution.providers in --json comes
// from the resolved providers' names, never from the raw tokens.
func TestOutputProviderListUsesBareNames(t *testing.T) {
	got := providerNamesOf([]providers.Provider{namedProvider("gemini"), namedProvider("openai")})
	if len(got) != 2 || got[0] != "gemini" || got[1] != "openai" {
		t.Fatalf("providerNamesOf = %v", got)
	}
}
