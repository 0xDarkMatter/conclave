package output

import (
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// TestJSONCarriesTransportUnderTheBareProviderKey pins the additive contract
// of ADR-012: responses.<provider> is keyed by the BARE name (no "@cli"), and
// each entry says which transport it ran on. A consumer keyed on
// responses.openai must keep working when openai moves to openai@cli.
func TestJSONCarriesTransportUnderTheBareProviderKey(t *testing.T) {
	cat := pricing.NewCatalog([]pricing.Model{{ID: "google/gemini-test", InputPerM: 1, OutputPerM: 0}})
	out := renderJSONTo(t, New(Options{JSON: true, Pricing: cat}),
		Result{Query: "q", Providers: []string{"gemini", "openai", "claude"},
			Responses: []providers.Response{
				resp("gemini", "gemini-test", 1_000_000, 0),
				cliResp("openai", "gpt-test", 1_000_000, 0),
				cliResp("claude", "sonnet", 1_000_000, 0),
			}})

	for _, name := range []string{"gemini", "openai", "claude"} {
		if _, ok := out.Responses[name]; !ok {
			t.Fatalf("responses.%s missing; keys: %v", name, keysOf(out.Responses))
		}
	}
	for key := range out.Responses {
		if key != providers.BareName(key) {
			t.Fatalf("a transport suffix leaked into a JSON key: %q", key)
		}
	}
	if got := out.Responses["gemini"].Transport; got != "api" {
		t.Errorf("gemini transport = %q, want api", got)
	}
	if got := out.Responses["openai"].Transport; got != "cli" {
		t.Errorf("openai transport = %q, want cli", got)
	}
	// Only the API leg is priced; the CLI legs have no cost_usd at all.
	if out.Responses["gemini"].CostUSD == nil {
		t.Error("gemini (api) should carry cost_usd")
	}
	if out.Responses["openai"].CostUSD != nil || out.Responses["claude"].CostUSD != nil {
		t.Error("a CLI-transport response carried cost_usd")
	}
	if out.Meta.TotalCostPartial {
		t.Error("CLI legs must not mark the total as a floor")
	}
}

func keysOf(m map[string]ResponseJSON) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
