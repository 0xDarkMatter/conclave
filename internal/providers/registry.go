package providers

import (
	"fmt"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
)

// Registry manages provider instances
type Registry struct {
	config    *config.Config
	providers map[string]Provider
	general   bool // true = API mode, false = CLI mode
	cheap     bool // true = use cheaper/faster models
}

// NewRegistry creates a new provider registry
// If general is true, uses API-based providers for general-purpose queries
// If cheap is true, uses cheaper/faster models for all providers
func NewRegistry(cfg *config.Config, general bool, cheap bool) *Registry {
	r := &Registry{
		config:    cfg,
		providers: make(map[string]Provider),
		general:   general,
		cheap:     cheap,
	}

	// Register providers based on mode
	var providerList []Provider
	if general {
		providerList = AllAPIProviders()
	} else {
		providerList = AllCLIProviders()
	}

	for _, p := range providerList {
		r.providers[p.Name()] = p
	}

	return r
}

// AllProviders returns all known providers (CLI mode, for backwards compatibility)
func AllProviders() []Provider {
	return AllCLIProviders()
}

// AllCLIProviders returns CLI-based providers (coding-focused)
func AllCLIProviders() []Provider {
	return []Provider{
		NewGeminiProvider(),
		NewOpenAIProvider(),
		NewClaudeProvider(),
		NewPerplexityProvider(),
		NewGrokProvider(),
		NewGLMProvider(),
	}
}

// AllAPIProviders returns API-based providers (general-purpose)
func AllAPIProviders() []Provider {
	return []Provider{
		NewGeminiAPIProvider(),
		NewOpenAIAPIProvider(),
		NewAnthropicAPIProvider(),
		NewPerplexityAPIProvider(),
		NewGrokAPIProvider(),
		// NewGLMAPIProvider(), // Disabled: account balance required
	}
}

// AnyAvailable returns true if at least one provider is available
func AnyAvailable(general bool) bool {
	var providerList []Provider
	if general {
		providerList = AllAPIProviders()
	} else {
		providerList = AllCLIProviders()
	}

	for _, p := range providerList {
		if p.IsAvailable() {
			return true
		}
	}
	return false
}

// GetProvider returns a single provider by name
func (r *Registry) GetProvider(name string, modelOverrides map[string]string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}

	if !p.IsAvailable() {
		if r.general {
			return nil, fmt.Errorf("provider %s not available (API key not set)", name)
		}
		return nil, fmt.Errorf("provider %s not available (CLI not installed)", name)
	}

	// Get model - priority: explicit -m flag > cheap mode > config defaults > provider default
	var model string
	if override, ok := modelOverrides[name]; ok && override != "" {
		model = override // Explicit override from -m flag (highest priority)
	} else if r.cheap {
		model = r.config.GetCheapModel(name) // Cheap mode: use cheap models
	} else if !r.general {
		model = r.config.GetModel(name, "") // CLI mode: use config defaults
	}
	// For API mode without explicit override or cheap mode: model stays empty, provider uses its default

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

// providerCompanies maps provider names to company names
var providerCompanies = map[string]string{
	"gemini":     "Google",
	"openai":     "OpenAI",
	"claude":     "Anthropic",
	"perplexity": "Perplexity",
	"grok":       "xAI",
	"glm":        "Zhipu",
}

// modelDisplayNames maps raw model names to formatted display names
var modelDisplayNames = map[string]string{
	// Gemini (CLI)
	"gemini-2.5-pro":   "Gemini 2.5 Pro",
	"gemini-2.5-flash": "Gemini 2.5 Flash",
	"gemini-2.0-pro":   "Gemini 2.0 Pro",
	// Gemini (API)
	"gemini-2.0-flash":       "Gemini 2.0 Flash",
	"gemini-2.5-flash-lite":  "Gemini 2.5 Flash Lite",
	"gemini-3-flash-preview": "Gemini 3 Flash",
	"gemini-3-pro-preview":   "Gemini 3 Pro",
	// OpenAI
	"gpt-5.2":     "GPT-5.2",
	"gpt-5-nano":  "GPT-5 Nano",
	"gpt-4o":      "GPT-4o",
	"gpt-4o-mini": "GPT-4o Mini",
	"o1":          "o1",
	"o1-mini":     "o1-mini",
	"o3":          "o3",
	// Claude (CLI)
	"sonnet": "Claude Sonnet",
	"opus":   "Claude Opus",
	"haiku":  "Claude Haiku",
	// Claude (API)
	"claude-opus-4-5-20251101":   "Claude Opus 4.5",
	"claude-sonnet-4-5-20250929": "Claude Sonnet 4.5",
	"claude-haiku-4-5-20251015":  "Claude Haiku 4.5",
	// Perplexity
	"sonar-pro":           "Sonar Pro",
	"sonar":               "Sonar",
	"sonar-reasoning":    "Sonar Reasoning",
	"sonar-reasoning-pro": "Sonar Reasoning Pro",
	// Grok (CLI)
	"grok-3":           "Grok 3",
	"grok-code-fast-1": "Grok Code Fast",
	"grok-4-latest":    "Grok 4",
	// Grok (API)
	"grok-4-1-fast-reasoning":     "Grok 4.1 Fast",
	"grok-4-1-fast-non-reasoning": "Grok 4.1 Fast NR",
	// GLM (CLI)
	"zai-coding-plan/glm-4.7": "GLM-4.7",
	"glm-4":                   "GLM-4",
	// GLM (API)
	"glm-4.7":        "GLM-4.7",
	"glm-4.6v-flashx": "GLM-4.6V FlashX",
}

// DisplayName returns a formatted name: {Company} {Model}
func DisplayName(provider, model string) string {
	company := providerCompanies[provider]
	if company == "" {
		company = provider
	}

	// Try to get formatted model name
	displayModel := modelDisplayNames[model]
	if displayModel == "" {
		displayModel = model // fallback to raw model name
	}

	return fmt.Sprintf("%s %s", company, displayModel)
}
