package cache

import (
	"context"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// transportProvider declares its own transport the way a registry-built
// provider does (ADR-012). It satisfies providers' unexported transporter
// interface structurally.
type transportProvider struct {
	transport providers.Transport
	calls     int
}

func (p *transportProvider) Name() string                   { return "claude" }
func (p *transportProvider) DefaultModel() string           { return "m" }
func (p *transportProvider) IsAvailable() bool              { return true }
func (p *transportProvider) Transport() providers.Transport { return p.transport }
func (p *transportProvider) Query(context.Context, string, string) (string, time.Duration, *providers.Metrics, error) {
	p.calls++
	return "answer", 0, &providers.Metrics{}, nil
}

// TestDeclaredTransportBeatsTheFallbackMode: a claude@cli answer under a -g
// run must be keyed "cli", or the same prompt on claude@api would be served
// the CLI's coding-tuned answer from disk.
func TestDeclaredTransportBeatsTheFallbackMode(t *testing.T) {
	c := New(t.TempDir(), time.Hour)

	cli := &transportProvider{transport: providers.TransportCLI}
	w := Wrap(cli, c, ModeAPI).(*cachedProvider)
	if w.mode != ModeCLI {
		t.Fatalf("mode = %q, want %q (the provider's own transport)", w.mode, ModeCLI)
	}

	// Populate under cli, then look up the same prompt as api: must miss.
	if _, _, _, err := w.Query(context.Background(), "p", "m"); err != nil {
		t.Fatal(err)
	}
	api := &transportProvider{transport: providers.TransportAPI}
	w2 := Wrap(api, c, ModeCLI)
	if _, _, m, _ := w2.Query(context.Background(), "p", "m"); m.Cached {
		t.Fatal("an API-transport query was served a CLI-transport answer")
	}
	if api.calls != 1 {
		t.Fatalf("api provider called %d times, want 1 (a live query)", api.calls)
	}
}

// TestFallbackModeAppliesOnlyToUndeclaredProviders pins the other half so
// batch injection and fakes keep working unchanged.
func TestFallbackModeAppliesOnlyToUndeclaredProviders(t *testing.T) {
	c := New(t.TempDir(), time.Hour)
	bare := &transportProvider{transport: providers.TransportDefault}
	if w := Wrap(bare, c, ModeAPI).(*cachedProvider); w.mode != ModeAPI {
		t.Fatalf("mode = %q, want the fallback %q", w.mode, ModeAPI)
	}
}

// TestModeConstantsMatchTransportStrings: the cache key's mode component and
// the transport words must stay the same two strings.
func TestModeConstantsMatchTransportStrings(t *testing.T) {
	if ModeCLI != string(providers.TransportCLI) || ModeAPI != string(providers.TransportAPI) {
		t.Fatalf("cache modes (%q, %q) drifted from transports (%q, %q)", ModeCLI, ModeAPI, providers.TransportCLI, providers.TransportAPI)
	}
}
