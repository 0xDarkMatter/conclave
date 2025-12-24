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
	// Gemini
	"gemini-2.5-pro":   "Gemini 2.5 Pro",
	"gemini-2.5-flash": "Gemini 2.5 Flash",
	"gemini-2.0-pro":   "Gemini 2.0 Pro",
	// OpenAI
	"gpt-5.2":     "GPT-5.2",
	"gpt-4o":      "GPT-4o",
	"o1":          "o1",
	"o1-mini":     "o1-mini",
	"o3":          "o3",
	// Claude
	"sonnet":      "Claude Sonnet",
	"opus":        "Claude Opus",
	"haiku":       "Claude Haiku",
	// Perplexity
	"sonar-pro":   "Sonar Pro",
	"sonar":       "Sonar",
	// Grok
	"grok-3":            "Grok 3",
	"grok-code-fast-1":  "Grok Code Fast",
	"grok-4-latest":     "Grok 4",
	// GLM
	"zai-coding-plan/glm-4.7": "GLM-4.7",
	"glm-4":                    "GLM-4",
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
