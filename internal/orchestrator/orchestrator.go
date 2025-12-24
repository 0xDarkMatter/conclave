package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// ProgressCallback is called when a provider starts or completes
// tokens is the total token count (input + output) when completed, 0 otherwise
type ProgressCallback func(provider string, started bool, duration time.Duration, tokens int, err error)

// Orchestrator manages parallel provider execution
type Orchestrator struct {
	providers []providers.Provider
	timeout   time.Duration
	onProgress ProgressCallback
}

// New creates a new orchestrator
func New(providerList []providers.Provider, timeoutSeconds int) *Orchestrator {
	return &Orchestrator{
		providers: providerList,
		timeout:   time.Duration(timeoutSeconds) * time.Second,
	}
}

// WithProgress sets a progress callback
func (o *Orchestrator) WithProgress(cb ProgressCallback) *Orchestrator {
	o.onProgress = cb
	return o
}

// Run executes the prompt against all providers in parallel
func (o *Orchestrator) Run(ctx context.Context, prompt string) ([]providers.Response, error) {
	results := make([]providers.Response, len(o.providers))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstError error

	for i, p := range o.providers {
		wg.Add(1)
		go func(idx int, provider providers.Provider) {
			defer wg.Done()

			// Notify start
			if o.onProgress != nil {
				o.onProgress(provider.Name(), true, 0, 0, nil)
			}

			// Create timeout context for this provider
			providerCtx, cancel := context.WithTimeout(ctx, o.timeout)
			defer cancel()

			model := provider.DefaultModel()
			response, duration, metrics, err := provider.Query(providerCtx, prompt, model)

			mu.Lock()
			defer mu.Unlock()

			// Calculate total tokens
			var totalTokens int
			if metrics != nil {
				totalTokens = metrics.InputTokens + metrics.OutputTokens
			}

			// Notify completion
			if o.onProgress != nil {
				o.onProgress(provider.Name(), false, duration, totalTokens, err)
			}

			if err != nil {
				results[idx] = providers.Response{
					Provider: provider.Name(),
					Model:    model,
					Status:   "error",
					Error:    err.Error(),
					Duration: duration,
				}
				if firstError == nil {
					firstError = fmt.Errorf("%s: %w", provider.Name(), err)
				}
			} else {
				results[idx] = providers.Response{
					Provider: provider.Name(),
					Model:    model,
					Status:   "success",
					Response: response,
					Duration: duration,
					Metrics:  metrics,
				}
			}
		}(i, p)
	}

	wg.Wait()

	// Count successful responses
	successCount := 0
	for _, r := range results {
		if r.Status == "success" {
			successCount++
		}
	}

	// If all providers failed, return an error
	if successCount == 0 && len(o.providers) > 0 {
		return results, fmt.Errorf("all providers failed")
	}

	return results, nil
}

// TotalDuration returns the total execution time
func TotalDuration(responses []providers.Response) time.Duration {
	var max time.Duration
	for _, r := range responses {
		if r.Duration > max {
			max = r.Duration
		}
	}
	return max
}
