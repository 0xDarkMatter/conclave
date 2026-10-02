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
