package providers

import (
	"context"
	"sync"
	"time"
)

// RunPreflight checks auth for all providers that implement Preflighter.
// Returns failures only, or nil if all pass.
func RunPreflight(ctx context.Context, providerList []Provider) []PreflightResult {
	// 5s, not 2s: claude and codex preflights spawn a Node/Rust process each,
	// and cold starts on Windows regularly exceed 2s (observed 2026-09-08).
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var results []PreflightResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, p := range providerList {
		pf, ok := unwrapPreflighter(p)
		if !ok {
			continue
		}

		wg.Add(1)
		go func(provider Provider, checker Preflighter) {
			defer wg.Done()
			err := checker.Preflight(ctx)

			if err != nil {
				mu.Lock()
				results = append(results, PreflightResult{
					Provider:    provider.Name(),
					OK:          false,
					Error:       err,
					Remediation: getRemediation(provider.Name()),
				})
				mu.Unlock()
			}
		}(p, pf)
	}

	wg.Wait()
	return results
}

// unwrapper is implemented by every provider decorator so preflight can reach
// the real provider underneath. Embedding the Provider interface only promotes
// the four methods Provider declares, so a decorator never inherits the
// OPTIONAL Preflighter interface from what it wraps. Without this, decorating a
// provider silently disables its auth check — a failure that shows up as a
// mysterious 401 mid-query rather than a clear preflight error.
type unwrapper interface{ Unwrap() Provider }

// unwrapPreflighter extracts a Preflighter from a provider, following any chain
// of decorators (model override, response cache, whatever comes next) down to
// the provider that actually implements the check. The loop is bounded because
// a decorator cycle would be a bug, not a reason to hang.
func unwrapPreflighter(p Provider) (Preflighter, bool) {
	for depth := 0; p != nil && depth < 8; depth++ {
		if pf, ok := p.(Preflighter); ok {
			return pf, true
		}
		u, ok := p.(unwrapper)
		if !ok {
			break
		}
		p = u.Unwrap()
	}
	return nil, false
}

func getRemediation(name string) string {
	switch name {
	case "claude":
		return "Run: claude auth login"
	case "gemini":
		return "Set GEMINI_API_KEY or GOOGLE_API_KEY env var (gemini-cli's free OAuth tier was retired; a key is required in both modes)"
	case "openai":
		return "Run: codex login (CLI mode) or set OPENAI_API_KEY env var (API mode)"
	case "grok":
		return "Set XAI_API_KEY env var"
	case "perplexity":
		return "Set PERPLEXITY_API_KEY env var"
	default:
		if IsOpenRouterModel(name) {
			return "Set OPENROUTER_API_KEY (or: conclave keyring set OPENROUTER_API_KEY) and check credit at https://openrouter.ai/credits"
		}
		return "Check provider documentation"
	}
}
