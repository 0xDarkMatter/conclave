package providers

import (
	"fmt"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
)

// Registry manages provider instances
type Registry struct {
	config    *config.Config
	providers map[string]Provider
}

// NewRegistry creates a new provider registry
func NewRegistry(cfg *config.Config) *Registry {
	r := &Registry{
		config:    cfg,
		providers: make(map[string]Provider),
	}

	// Register all providers
	for _, p := range AllProviders() {
		r.providers[p.Name()] = p
	}

	return r
}

// AllProviders returns all known providers
func AllProviders() []Provider {
	return []Provider{
		NewGeminiProvider(),
		NewOpenAIProvider(),
		NewClaudeProvider(),
		NewPerplexityProvider(),
		NewGrokProvider(),
		NewGLMProvider(),
	}
}

// GetProvider returns a single provider by name
func (r *Registry) GetProvider(name string, modelOverrides map[string]string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}

	if !p.IsAvailable() {
		return nil, fmt.Errorf("provider %s not available (CLI not installed)", name)
	}

	// Wrap with model override if specified
	model := r.config.GetModel(name, modelOverrides[name])
	return &modelOverrideProvider{Provider: p, model: model}, nil
}

// GetProviders returns multiple providers by name
func (r *Registry) GetProviders(names []string, modelOverrides map[string]string) ([]Provider, error) {
	var result []Provider

	for _, name := range names {
		p, err := r.GetProvider(name, modelOverrides)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}

	return result, nil
}

// modelOverrideProvider wraps a provider with a custom model
type modelOverrideProvider struct {
	Provider
	model string
}

func (p *modelOverrideProvider) DefaultModel() string {
	if p.model != "" {
		return p.model
	}
	return p.Provider.DefaultModel()
}
