package providers

import (
	"context"
	"errors"
	"testing"
)

// chainDecorator is a minimal stand-in for any provider wrapper (model
// override, response cache, a future retry or logging decorator).
type chainDecorator struct {
	Provider
}

func (d *chainDecorator) Unwrap() Provider { return d.Provider }

type failingPreflighter struct {
	mockProvider
	checks int
}

func (p *failingPreflighter) Preflight(ctx context.Context) error {
	p.checks++
	return errors.New("not authenticated")
}

// TestPreflightFollowsADecoratorChain is the invariant that keeps auth checks
// from disappearing as wrappers accumulate. Embedding the Provider interface
// promotes only its four methods, so every decorator hides the optional
// Preflighter of whatever it wraps unless it implements Unwrap.
func TestPreflightFollowsADecoratorChain(t *testing.T) {
	inner := &failingPreflighter{mockProvider: mockProvider{name: "openai"}}

	cases := map[string]Provider{
		"bare":           inner,
		"one decorator":  &chainDecorator{Provider: inner},
		"two decorators": &chainDecorator{Provider: &chainDecorator{Provider: inner}},
		"model override": &modelOverrideProvider{Provider: inner, model: "x"},
		"override then wrap": &chainDecorator{
			Provider: &modelOverrideProvider{Provider: inner, model: "x"},
		},
	}

	for name, p := range cases {
		inner.checks = 0
		failures := RunPreflight(context.Background(), []Provider{p})
		if len(failures) != 1 {
			t.Errorf("%s: got %d failures, want 1 (the check was skipped)", name, len(failures))
			continue
		}
		if inner.checks != 1 {
			t.Errorf("%s: inner Preflight ran %d times, want 1", name, inner.checks)
		}
	}
}

// TestUnwrapPreflighterTerminates guards the bounded loop: a decorator cycle is
// a bug, but it must not hang the CLI before every query.
func TestUnwrapPreflighterTerminates(t *testing.T) {
	cyc := &chainDecorator{}
	cyc.Provider = cyc // deliberately pathological

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, ok := unwrapPreflighter(cyc); ok {
			t.Error("a cyclic decorator reported a Preflighter")
		}
	}()
	<-done
}

// TestProviderWithNoPreflighterIsSkipped: providers without an auth check must
// not be reported as failures.
func TestProviderWithNoPreflighterIsSkipped(t *testing.T) {
	p := &chainDecorator{Provider: &mockProvider{name: "plain"}}
	if failures := RunPreflight(context.Background(), []Provider{p}); len(failures) != 0 {
		t.Fatalf("got %d failures for a provider with no preflight", len(failures))
	}
}
