package cache

import (
	"context"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// ModeCLI / ModeAPI are the mode component of the cache key. They are part of
// the key because the two paths send materially different requests for the
// same provider name: a CLI wrapper's coding-tuned system behaviour is not the
// same product as the raw API, so their answers must never be interchangeable.
const (
	ModeCLI = "cli"
	ModeAPI = "api"
)

// cachedProvider decorates a Provider with a read-through response cache.
//
// GUARD: this wrapper deliberately does NOT forward the optional Preflighter
// interface. A type assertion for Preflighter against the wrapper fails, so
// wrapping a provider before providers.RunPreflight would silently skip its
// auth check. Callers must wrap AFTER preflight — cmd/root.go does, and batch
// mode preflights unwrapped providers in cmd/root.go before the processor is
// built. If you ever make this type forward Preflight, make sure a cache hit
// still cannot mask a broken credential.
type cachedProvider struct {
	providers.Provider
	cache *Cache
	mode  string
}

// Wrap returns p backed by c. A nil cache returns p untouched, which is what
// makes the feature opt-in without a branch at every call site.
func Wrap(p providers.Provider, c *Cache, mode string) providers.Provider {
	if c == nil {
		return p
	}
	return &cachedProvider{Provider: p, cache: c, mode: mode}
}

// WrapAll is Wrap over a slice, preserving order.
func WrapAll(list []providers.Provider, c *Cache, mode string) []providers.Provider {
	if c == nil {
		return list
	}
	out := make([]providers.Provider, len(list))
	for i, p := range list {
		out[i] = Wrap(p, c, mode)
	}
	return out
}

// Query serves from the store when a fresh entry exists, otherwise calls
// through and stores the result. Cache faults are swallowed: the worst a
// broken store can do is cost a live query.
func (p *cachedProvider) Query(ctx context.Context, prompt, model string) (string, time.Duration, *providers.Metrics, error) {
	// The system prompt is "" until the Provider interface carries one; see Key.
	key := Key(p.mode, p.Provider.Name(), model, prompt, "")

	start := time.Now()
	if e, ok := p.cache.Get(key); ok {
		m := e.Metrics
		if m == nil {
			m = &providers.Metrics{}
		} else {
			// Copy: the stored pointer is decoded per read, but callers mutate
			// Metrics (the formatter writes CostUSD into it) and must not be
			// able to reach through to a shared value.
			cp := *m
			m = &cp
		}
		m.Cached = true
		// A cached answer cost nothing this run. Zeroing here keeps every cost
		// path honest without each of them having to special-case cache hits.
		m.CostUSD = 0
		return e.Response, time.Since(start), m, nil
	}

	resp, dur, metrics, err := p.Provider.Query(ctx, prompt, model)
	if err != nil || resp == "" {
		return resp, dur, metrics, err // never cache a failure or an empty answer
	}

	_ = p.cache.Put(&Entry{
		Key:       key,
		FetchedAt: time.Now().UTC(),
		Mode:      p.mode,
		Provider:  p.Provider.Name(),
		Model:     model,
		Response:  resp,
		Metrics:   metrics,
	})
	return resp, dur, metrics, err
}
