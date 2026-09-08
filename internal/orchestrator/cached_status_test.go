package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/cache"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// statusProvider answers successfully and counts calls, so a second query can
// be proven to have come from the cache.
type statusProvider struct {
	calls int
}

func (p *statusProvider) Name() string         { return "openai" }
func (p *statusProvider) DefaultModel() string { return "gpt-test" }
func (p *statusProvider) IsAvailable() bool    { return true }
func (p *statusProvider) Query(ctx context.Context, prompt, model string) (string, time.Duration, *providers.Metrics, error) {
	p.calls++
	return "an answer", time.Millisecond, &providers.Metrics{InputTokens: 5, OutputTokens: 7}, nil
}

// TestCachedHitKeepsStatusSuccess pins a contract downstream consumers depend
// on: a cache hit is a SUCCESSFUL response that happens to have been served
// from disk. Cachedness is signalled only by the separate Cached field.
//
// The adversary is a well-meaning change that gives cache hits their own
// status value. Anything treating a non-"success" status as a panel failure
// would then read a fully healthy run as a degraded one, and a consumer that
// synthesises a verdict from panel health would emit a confident wrong answer
// from nothing but a warm cache.
func TestCachedHitKeepsStatusSuccess(t *testing.T) {
	c := cache.New(t.TempDir(), time.Hour)
	prov := &statusProvider{}
	wrapped := cache.Wrap(prov, c, cache.ModeAPI)
	orch := New([]providers.Provider{wrapped}, 30)

	first, err := orch.Run(context.Background(), "a prompt")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if first[0].Status != "success" {
		t.Fatalf("live response status = %q, want success", first[0].Status)
	}
	if first[0].Cached {
		t.Fatal("a live response must not be marked cached")
	}

	second, err := orch.Run(context.Background(), "a prompt")
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("provider called %d times; the second run was not a cache hit", prov.calls)
	}

	// The contract, both halves.
	if second[0].Status != "success" {
		t.Fatalf("cached response status = %q, want \"success\": a consumer reading status would see a phantom panel failure", second[0].Status)
	}
	if !second[0].Cached {
		t.Fatal("cached response is not marked Cached, so the hit is invisible")
	}
	if second[0].Response != "an answer" {
		t.Fatalf("cached response body = %q", second[0].Response)
	}
	if second[0].Error != "" {
		t.Fatalf("cached response carries an error: %q", second[0].Error)
	}
}
