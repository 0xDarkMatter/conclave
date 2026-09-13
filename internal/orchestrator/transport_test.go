package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// transportFake declares a transport the way a registry-built provider does.
type transportFake struct {
	name      string
	transport providers.Transport
	fail      bool
}

func (p *transportFake) Name() string                   { return p.name }
func (p *transportFake) DefaultModel() string           { return "m" }
func (p *transportFake) IsAvailable() bool              { return true }
func (p *transportFake) Transport() providers.Transport { return p.transport }
func (p *transportFake) Query(context.Context, string, string) (string, time.Duration, *providers.Metrics, error) {
	if p.fail {
		return "", 0, nil, errors.New("boom")
	}
	return "ok", 0, &providers.Metrics{}, nil
}

// TestResponseRecordsTheTransportItRanOn: both the success and the error
// shape carry Transport, because pricing and the JSON consumer read it off
// the response (ADR-012). An undeclared provider yields "" (unknown).
func TestResponseRecordsTheTransportItRanOn(t *testing.T) {
	results, _ := New([]providers.Provider{
		&transportFake{name: "gemini", transport: providers.TransportAPI},
		&transportFake{name: "openai", transport: providers.TransportCLI, fail: true},
		&transportFake{name: "claude"},
	}, 5).Run(context.Background(), "p")

	want := []string{"api", "cli", ""}
	for i, r := range results {
		if r.Transport != want[i] {
			t.Errorf("%s: transport = %q, want %q", r.Provider, r.Transport, want[i])
		}
	}
	if results[1].Status != "error" {
		t.Fatal("the failing provider should be an error response (and still carry its transport)")
	}
}
