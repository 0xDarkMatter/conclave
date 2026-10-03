// Registry boundary tests for ADR-016. They pin that decision models resolve
// only through AllDeciders/GetDecider and never leak into ordinary provider
// selection, transport suffix handling, or --all API fan-out.
package providers

import (
	"strings"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
)

func TestDeciderTokenOnProviderPathPointsToDecide(t *testing.T) {
	registry := NewRegistry(config.DefaultConfig(), true, false)
	_, err := registry.GetProvider("clef", nil)
	if err == nil || !strings.Contains(err.Error(), `clef is a decision model; use "conclave decide clef ..."`) {
		t.Fatalf("error = %v", err)
	}
}

func TestDeciderRejectsTransportSuffix(t *testing.T) {
	_, err := GetDecider("clef@api")
	if err == nil || err.Error() != "clef@api: decision models are API-only and take no transport suffix" {
		t.Fatalf("error = %v", err)
	}
}

func TestDecidersNotInAllAPIProviders(t *testing.T) {
	deciders := map[string]bool{"jev": true, "clef": true, "clef-flash": true}
	for _, provider := range AllAPIProviders() {
		if deciders[provider.Name()] {
			t.Fatalf("decision model %q leaked into AllAPIProviders", provider.Name())
		}
	}
}

func TestAllDecidersRegistered(t *testing.T) {
	want := map[string]string{
		"jev":        "jev-latest",
		"clef":       "clef",
		"clef-flash": "clef-flash",
	}
	for _, decider := range AllDeciders() {
		model, ok := want[decider.Name()]
		if !ok {
			t.Errorf("unexpected decider %q", decider.Name())
			continue
		}
		if decider.DefaultModel() != model {
			t.Errorf("%s default model = %q, want %q", decider.Name(), decider.DefaultModel(), model)
		}
		delete(want, decider.Name())
	}
	if len(want) != 0 {
		t.Errorf("missing deciders: %v", want)
	}
}

func TestGetDeciderUnknownListsValidNames(t *testing.T) {
	_, err := GetDecider("unknown")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"jev", "clef", "clef-flash"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

// TestAllDecidersNamesRecognisedBySharedTable pins the finding-5 fix: a
// decider added to AllDeciders but missing from the shared name table would
// pass that list yet fall through GetProvider to "unknown provider". Walking
// the real constructors (not a literal name map, as the older tests above do)
// keeps the list and the table in step.
func TestAllDecidersNamesRecognisedBySharedTable(t *testing.T) {
	for _, decider := range AllDeciders() {
		if !isDeciderName(decider.Name()) {
			t.Errorf("isDeciderName(%q) = false; add it to deciderRegistry in registry.go", decider.Name())
		}
	}
}

// Jev reaches the same model through OpenRouter's Decisions API (probed live
// 2026-10-03); a machine with only an OpenRouter key must still have jev, and
// a Typesafe key must keep the direct route.
func TestJevRoutesDirectFirstThenOpenRouter(t *testing.T) {
	t.Setenv("CONCLAVE_JEV_BASE_URL", "")
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	if d := NewJevDecider(); d.baseURL != jevOpenRouterURL || d.apiKeyEnv != "OPENROUTER_API_KEY" || !d.IsAvailable() {
		t.Fatalf("OpenRouter-only: url=%q key=%q available=%v", d.baseURL, d.apiKeyEnv, d.IsAvailable())
	}
	t.Setenv("TYPESAFE_API_KEY", "ts-test")
	if d := NewJevDecider(); d.baseURL != jevDefaultURL || d.apiKeyEnv != "TYPESAFE_API_KEY" {
		t.Fatalf("both keys: url=%q key=%q; want the direct route", d.baseURL, d.apiKeyEnv)
	}
}
