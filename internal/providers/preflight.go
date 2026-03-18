package providers

import (
	"context"
	"sync"
	"time"
)

// RunPreflight checks auth for all providers that implement Preflighter.
// Returns failures only, or nil if all pass.
func RunPreflight(ctx context.Context, providerList []Provider) []PreflightResult {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
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

// unwrapPreflighter extracts a Preflighter from a provider,
// looking through the modelOverrideProvider wrapper if needed.
func unwrapPreflighter(p Provider) (Preflighter, bool) {
	if pf, ok := p.(Preflighter); ok {
		return pf, true
	}
	if mop, ok := p.(*modelOverrideProvider); ok {
		if pf, ok := mop.Provider.(Preflighter); ok {
			return pf, true
		}
	}
	return nil, false
}

func getRemediation(name string) string {
	switch name {
	case "claude":
		return "Run: claude auth login"
	case "gemini":
		return "Set GEMINI_API_KEY or GOOGLE_API_KEY env var"
	case "openai":
		return "Set OPENAI_API_KEY env var"
	case "grok":
		return "Set XAI_API_KEY env var"
	case "perplexity":
		return "Set PERPLEXITY_API_KEY env var"
	default:
		return "Check provider documentation"
	}
}
