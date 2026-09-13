package providers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
)

// fakeProvider is always available and remembers nothing; the registry tests
// below are about RESOLUTION (which set, which model, which transport), not
// about talking to anything.
type fakeProvider struct {
	name  string
	model string
}

func (f *fakeProvider) Name() string         { return f.name }
func (f *fakeProvider) DefaultModel() string { return f.model }
func (f *fakeProvider) IsAvailable() bool    { return true }
func (f *fakeProvider) Query(context.Context, string, string) (string, time.Duration, *Metrics, error) {
	return "", 0, nil, nil
}

// mixedRegistry mirrors the real shape: glm exists on the CLI side only
// (ADR-006), everything else on both.
func mixedRegistry(general, cheap bool) *Registry {
	cli := []Provider{
		&fakeProvider{"gemini", "gemini-cli-default"},
		&fakeProvider{"openai", "codex-default"},
		&fakeProvider{"claude", "sonnet"},
		&fakeProvider{"glm", "zai-coding-plan/glm-5.2"},
	}
	api := []Provider{
		&fakeProvider{"gemini", "gemini-api-default"},
		&fakeProvider{"openai", "gpt-api-default"},
		&fakeProvider{"claude", "claude-api-default"},
	}
	return newRegistryWith(config.DefaultConfig(), general, cheap, cli, api)
}

func TestParseProviderToken(t *testing.T) {
	tests := []struct {
		token   string
		name    string
		want    Transport
		errWant string // substring; "" = no error
	}{
		{"openai", "openai", TransportDefault, ""},
		{"openai@cli", "openai", TransportCLI, ""},
		{"openai@api", "openai", TransportAPI, ""},
		{" claude@cli ", "claude", TransportCLI, ""},
		{"deepseek/deepseek-v4@api", "deepseek/deepseek-v4", TransportAPI, ""},
		{"deepseek/deepseek-v4@cli", "", TransportDefault, "API-only"},
		{"openai@sdk", "", TransportDefault, "unknown transport suffix"},
		{"openai@", "", TransportDefault, "unknown transport suffix"},
		{"@cli", "", TransportDefault, "missing provider name"},
		{"openai@CLI", "", TransportDefault, "unknown transport suffix"}, // case-sensitive on purpose: one spelling
	}
	for _, tc := range tests {
		t.Run(tc.token, func(t *testing.T) {
			name, tr, err := ParseProviderToken(tc.token)
			if tc.errWant != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errWant) {
					t.Fatalf("err = %v, want one containing %q", err, tc.errWant)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tc.name || tr != tc.want {
				t.Fatalf("got (%q, %q), want (%q, %q)", name, tr, tc.name, tc.want)
			}
		})
	}
}

// TestSuffixNeverReachesProviderName is the contract every output surface
// depends on: --json keys, progress lines and the judge label all use
// Name(), so a leaked "@cli" would fork the schema.
func TestSuffixNeverReachesProviderName(t *testing.T) {
	r := mixedRegistry(true, false)
	for _, token := range []string{"openai@cli", "openai@api", "openai"} {
		p, err := r.GetProvider(token, nil)
		if err != nil {
			t.Fatalf("%s: %v", token, err)
		}
		if p.Name() != "openai" {
			t.Fatalf("%s resolved to Name() %q; the suffix leaked", token, p.Name())
		}
	}
}

// TestGlobalGeneralDoesNotOverrideExplicitCli: -g sets the default for bare
// tokens and nothing more. A claude@cli beside it must run on the CLI (the
// Praxis case: Max-plan claude in a -g panel).
func TestGlobalGeneralDoesNotOverrideExplicitCli(t *testing.T) {
	r := mixedRegistry(true, false)
	p, err := r.GetProvider("claude@cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := TransportOf(p); got != TransportCLI {
		t.Fatalf("transport = %q, want cli", got)
	}
	// The CLI set's instance answered, with the CLI-side config default.
	if p.DefaultModel() != config.DefaultConfig().Models["claude"] {
		t.Fatalf("model = %q, want the CLI config default", p.DefaultModel())
	}
}

// TestExplicitApiWorksWithoutGlobalGeneral is the mirror: gemini@api in a
// CLI-mode run reaches the API set without -g.
func TestExplicitApiWorksWithoutGlobalGeneral(t *testing.T) {
	r := mixedRegistry(false, false)
	p, err := r.GetProvider("gemini@api", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := TransportOf(p); got != TransportAPI {
		t.Fatalf("transport = %q, want api", got)
	}
	if p.DefaultModel() != "gemini-api-default" {
		t.Fatalf("model = %q, want the API provider's own default", p.DefaultModel())
	}
}

// TestBareTokenFollowsTheGlobalMode pins today's behaviour for unsuffixed
// tokens so the feature is purely additive.
func TestBareTokenFollowsTheGlobalMode(t *testing.T) {
	for _, general := range []bool{false, true} {
		p, err := mixedRegistry(general, false).GetProvider("openai", nil)
		if err != nil {
			t.Fatal(err)
		}
		want := TransportCLI
		if general {
			want = TransportAPI
		}
		if got := TransportOf(p); got != want {
			t.Fatalf("general=%v: transport = %q, want %q", general, got, want)
		}
	}
}

// TestMixedPanelResolvesEachTokenIndependently is the headline invocation:
// gemini@api,openai@cli,claude@cli in ONE registry.
func TestMixedPanelResolvesEachTokenIndependently(t *testing.T) {
	r := mixedRegistry(false, false)
	list, err := r.GetProviders([]string{"gemini@api", "openai@cli", "claude@cli"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []Transport{TransportAPI, TransportCLI, TransportCLI}
	for i, p := range list {
		if got := TransportOf(p); got != want[i] {
			t.Errorf("%s: transport = %q, want %q", p.Name(), got, want[i])
		}
	}
	if list[0].DefaultModel() != "gemini-api-default" {
		t.Errorf("gemini@api should use the API provider's default, got %q", list[0].DefaultModel())
	}
}

// TestModelOverrideAppliesRegardlessOfSuffix: overrides are keyed by bare
// name, so "-m openai:x" reaches openai@cli and openai@api alike.
func TestModelOverrideAppliesRegardlessOfSuffix(t *testing.T) {
	overrides := map[string]string{"openai": "gpt-5.6-sol"}
	for _, token := range []string{"openai", "openai@cli", "openai@api"} {
		p, err := mixedRegistry(true, false).GetProvider(token, overrides)
		if err != nil {
			t.Fatalf("%s: %v", token, err)
		}
		if p.DefaultModel() != "gpt-5.6-sol" {
			t.Errorf("%s: model = %q, want the -m override", token, p.DefaultModel())
		}
	}
}

// TestCheapDoesNotFeedApiModelsToAnExplicitCli: -c implies API for bare
// tokens, but a @cli provider under -c must get its normal CLI default, not an
// API-oriented cheap id the CLI wrapper cannot use.
func TestCheapDoesNotFeedApiModelsToAnExplicitCli(t *testing.T) {
	cfg := config.DefaultConfig()
	r := mixedRegistry(true, true)

	bare, err := r.GetProvider("openai", nil)
	if err != nil {
		t.Fatal(err)
	}
	if bare.DefaultModel() != cfg.CheapModels["openai"] {
		t.Fatalf("bare token under -c: model = %q, want the cheap model", bare.DefaultModel())
	}

	cli, err := r.GetProvider("openai@cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cli.DefaultModel() != cfg.Models["openai"] {
		t.Fatalf("openai@cli under -c: model = %q, want the CLI default %q", cli.DefaultModel(), cfg.Models["openai"])
	}
	if TransportOf(cli) != TransportCLI {
		t.Fatal("openai@cli under -c must still run on the CLI")
	}
}

// TestApiSuffixOnApiDisabledProviderNamesTheADR: glm has no API side
// (ADR-006). The error must say why and point at the working spelling.
func TestApiSuffixOnApiDisabledProviderNamesTheADR(t *testing.T) {
	_, err := mixedRegistry(false, false).GetProvider("glm@api", nil)
	if err == nil {
		t.Fatal("glm@api resolved; glm has no API implementation")
	}
	for _, want := range []string{"ADR-006", "glm@cli"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q; got: %s", want, err)
		}
	}
	// Same provider under bare -g: still an error, still pointing at @cli.
	_, err = mixedRegistry(true, false).GetProvider("glm", nil)
	if err == nil || !strings.Contains(err.Error(), "glm@cli") {
		t.Errorf("bare glm under -g should suggest glm@cli; got: %v", err)
	}
}

// TestCliSuffixOnSlashTokenIsRefused: OpenRouter has no CLI (ADR-010).
func TestCliSuffixOnSlashTokenIsRefused(t *testing.T) {
	t.Setenv(OpenRouterKeyEnv, "test-key")
	_, err := NewRegistry(config.DefaultConfig(), true, false).GetProvider("deepseek/deepseek-v4@cli", nil)
	if err == nil {
		t.Fatal("a slash token with @cli resolved")
	}
	for _, want := range []string{"OpenRouter", "API-only", "@api"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q; got: %s", want, err)
		}
	}
}

// TestApiSuffixOnSlashTokenNeedsNoGlobalGeneral: the explicit transport is
// the authorisation -g used to provide.
func TestApiSuffixOnSlashTokenNeedsNoGlobalGeneral(t *testing.T) {
	t.Setenv(OpenRouterKeyEnv, "test-key")
	p, err := NewRegistry(config.DefaultConfig(), false, false).GetProvider("deepseek/deepseek-v4@api", nil)
	if err != nil {
		t.Fatalf("slash@api in CLI mode: %v", err)
	}
	if p.Name() != "deepseek/deepseek-v4" || TransportOf(p) != TransportAPI {
		t.Fatalf("got name %q transport %q", p.Name(), TransportOf(p))
	}
}

// TestUnknownSuffixIsAnErrorNotAnUnknownProvider: "openai@sdk" must not fall
// through to a misleading "unknown provider: openai@sdk".
func TestUnknownSuffixIsAnErrorNotAnUnknownProvider(t *testing.T) {
	_, err := mixedRegistry(false, false).GetProvider("openai@sdk", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown transport suffix") {
		t.Fatalf("got %v, want an unknown-suffix error", err)
	}
}

// TestTransportOfSurvivesDecorators: the transport must be recoverable
// through the same Unwrap chain preflight uses, or the cache would key a
// wrapped provider on the fallback mode.
func TestTransportOfSurvivesDecorators(t *testing.T) {
	p, err := mixedRegistry(true, false).GetProvider("claude@cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &modelOverrideProvider{Provider: p, model: "x"} // an outer decorator with no transport of its own
	if got := TransportOf(wrapped); got != TransportCLI {
		t.Fatalf("transport through a decorator = %q, want cli", got)
	}
	if got := TransportOf(&fakeProvider{"openai", "m"}); got != TransportDefault {
		t.Fatalf("a bare provider must report unknown, got %q", got)
	}
}

func TestBareName(t *testing.T) {
	for in, want := range map[string]string{
		"openai":       "openai",
		"openai@cli":   "openai",
		"openai@sdk":   "openai@sdk", // malformed: returned as-is, rejected elsewhere
		"a/b@api":      "a/b",
		"  claude@api": "claude",
	} {
		if got := BareName(in); got != want {
			t.Errorf("BareName(%q) = %q, want %q", in, got, want)
		}
	}
}
