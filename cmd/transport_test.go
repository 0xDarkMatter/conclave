package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// captureStderr runs fn while redirecting os.Stderr and returns what was written.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan struct{})
	var buf bytes.Buffer
	go func() { _, _ = io.Copy(&buf, r); close(done) }()
	fn()
	w.Close()
	os.Stderr = old
	<-done
	return buf.String()
}

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

// transportNamed is a namedProvider that declares a transport, the way a
// registry-built provider does.
type transportNamed struct {
	namedProvider
	transport providers.Transport
}

func (p transportNamed) Transport() providers.Transport { return p.transport }

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

// TestJudgeOnOtherTransportIsStillPreflighted: with -g gemini,claude@cli and
// the default judge "claude" (API), the panel's claude and the judge's claude
// are different credentials. withJudge must not treat the CLI leg as the
// judge, or the API judge's auth check would be skipped and the panel would
// run and then fail at synthesis.
func TestJudgeOnOtherTransportIsStillPreflighted(t *testing.T) {
	panel := []providers.Provider{
		transportNamed{"gemini", providers.TransportAPI},
		transportNamed{"claude", providers.TransportCLI},
	}
	apiJudge := transportNamed{"claude", providers.TransportAPI}
	if got := withJudge(panel, apiJudge); len(got) != 3 {
		t.Fatalf("API judge was deduplicated against the CLI panel member; checked list has %d entries, want 3", len(got))
	}
	cliJudge := transportNamed{"claude", providers.TransportCLI}
	if got := withJudge(panel, cliJudge); len(got) != 2 {
		t.Fatalf("same name and transport should deduplicate; got %d entries, want 2", len(got))
	}
}

// TestConfigPinIsAnnouncedOnlyWhenItChangedTheOutcome: a config transports pin
// is the one input that can move billing without appearing in the command
// line, so the run must say so, and must say so only when the pin actually
// redirected a bare token. Suffixed tokens and pins that agree with the global
// mode stay silent, or the note becomes noise nobody reads.
func TestConfigPinIsAnnouncedOnlyWhenItChangedTheOutcome(t *testing.T) {
	flagGeneral, flagQuiet, flagRaw = true, false, false
	t.Cleanup(func() { flagGeneral = false })
	cfg := config.DefaultConfig()
	cfg.Transports = map[string]string{"claude": "cli", "gemini": "api"}

	panel := []providers.Provider{
		transportNamed{"claude", providers.TransportCLI}, // pinned away from -g: announce
		transportNamed{"gemini", providers.TransportAPI}, // pin agrees with -g: silent
		transportNamed{"openai", providers.TransportAPI}, // no pin: silent
	}
	out := captureStderr(t, func() { warnConfigTransportPins(cfg, []string{"claude", "gemini", "openai"}, panel) })
	if !strings.Contains(out, "transports.claude") || !strings.Contains(out, "claude@api") {
		t.Fatalf("pin that changed claude's transport was not announced:\n%s", out)
	}
	if strings.Contains(out, "gemini") || strings.Contains(out, "openai") {
		t.Fatalf("a pin that changed nothing was announced:\n%s", out)
	}

	// A suffix on the token is the user's own choice; no note.
	out = captureStderr(t, func() { warnConfigTransportPins(cfg, []string{"claude@cli"}, panel[:1]) })
	if out != "" {
		t.Fatalf("suffixed token produced a config-pin note:\n%s", out)
	}
}
