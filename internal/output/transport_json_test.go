package output

import (
	"strings"
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

// TestMixedPanelTagsEachBlockWithItsTransport: a person reading the styled
// view of a mixed panel must be able to see which leg ran where without
// inferring it from the presence of a dollar figure.
func TestMixedPanelTagsEachBlockWithItsTransport(t *testing.T) {
	r := Result{
		Query:     "q",
		Providers: []string{"gemini", "openai"},
		Responses: []providers.Response{
			resp("gemini", "gemini-test", 10, 0),
			cliResp("openai", "gpt-test", 10, 0),
		},
	}
	out := renderStyledTo(t, New(Options{}), r)
	for _, want := range []string{"via api", "via cli"} {
		if !strings.Contains(out, want) {
			t.Errorf("mixed panel output lacks %q:\n%s", want, out)
		}
	}
}

// TestSingleTransportPanelShowsNoTag pins the other half: an all-CLI or
// all-API run keeps today's header exactly, so nobody's terminal output
// changes for a command line that has not changed.
func TestSingleTransportPanelShowsNoTag(t *testing.T) {
	r := Result{
		Query:     "q",
		Providers: []string{"gemini", "openai"},
		Responses: []providers.Response{
			cliResp("gemini", "gemini-test", 10, 0),
			cliResp("openai", "gpt-test", 10, 0),
		},
	}
	out := renderStyledTo(t, New(Options{}), r)
	if strings.Contains(out, "via cli") || strings.Contains(out, "via api") {
		t.Fatalf("single-transport panel was tagged:\n%s", out)
	}
	if !panelIsMixed([]providers.Response{{Transport: "cli"}, {Transport: ""}, {Transport: "api"}}) {
		t.Error("cli + api (with an unknown in between) should count as mixed")
	}
	if panelIsMixed([]providers.Response{{Transport: ""}, {Transport: "api"}}) {
		t.Error("an unknown transport must not make a panel look mixed")
	}
}
