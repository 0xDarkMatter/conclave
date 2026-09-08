package providers

import (
	"fmt"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
)

// Slash routing (ADR-010): in API mode a provider token containing "/" is an
// OpenRouter model id and is constructed on demand by GetProvider rather than
// registered up front. AllAPIProviders deliberately does NOT include OpenRouter
// so --all never fans out to the whole catalog; OpenRouterListing exists only
// so --list-providers and AnyAvailable can show the row.

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

// OpenRouterListing returns the non-routable "openrouter" placeholder row for
// --list-providers (API column only). Ready iff OPENROUTER_API_KEY resolves.
func OpenRouterListing() Provider {
	return NewOpenRouterAPIProvider("")
}

// AnyAvailable returns true if at least one provider is available. In API
// mode an OpenRouter key alone counts: every vendor/model token is usable.
func AnyAvailable(general bool) bool {
	var providerList []Provider
	if general {
		providerList = append(AllAPIProviders(), OpenRouterListing())
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

// GetProvider returns a single provider by name. A name containing "/" is an
// OpenRouter model (API mode only) and is built on first use; see the slash
// routing note at the top of this file.
func (r *Registry) GetProvider(name string, modelOverrides map[string]string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok && IsOpenRouterModel(name) {
		if !r.general {
			return nil, fmt.Errorf("provider %q is an OpenRouter model (vendor/model) and OpenRouter is API-only: add -g", name)
		}
		op := NewOpenRouterAPIProvider(name)
		if !op.IsAvailable() {
			return nil, fmt.Errorf("provider %s not available (%s not set)", name, OpenRouterKeyEnv)
		}
		// Cache so a token used as both panel member and judge shares one
		// instance (and one keyring lookup).
		r.providers[name] = op
		p, ok = op, true
	}
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
	"gemini-3.1-pro-preview": "Gemini 3.1 Pro",
	// OpenAI
	"gpt-5.6-sol":   "GPT-5.6 Sol",
	"gpt-5.6-terra": "GPT-5.6 Terra",
	"gpt-5.6-luna":  "GPT-5.6 Luna",
	"gpt-5.5":       "GPT-5.5",
	"gpt-5.2":       "GPT-5.2",
	"gpt-5-nano":    "GPT-5 Nano",
	"gpt-4o":        "GPT-4o",
	"gpt-4o-mini":   "GPT-4o Mini",
	"o1":            "o1",
	"o1-mini":       "o1-mini",
	"o3":            "o3",
	// Claude (CLI)
	"sonnet": "Claude Sonnet",
	"opus":   "Claude Opus",
	"haiku":  "Claude Haiku",
	// Claude (API)
	"claude-opus-5":              "Claude Opus 5",
	"claude-sonnet-5":            "Claude Sonnet 5",
	"claude-fable-5-1":           "Claude Fable 5.1",
	"claude-opus-4-8":            "Claude Opus 4.8",
	"claude-opus-4-5-20251101":   "Claude Opus 4.5",
	"claude-sonnet-4-5-20250929": "Claude Sonnet 4.5",
	"claude-haiku-4-5-20251001":  "Claude Haiku 4.5", // matches the cheap-model id in config.go
	// Perplexity
	"sonar-pro":           "Sonar Pro",
	"sonar":               "Sonar",
	"sonar-reasoning":     "Sonar Reasoning",
	"sonar-reasoning-pro": "Sonar Reasoning Pro",
	// Grok (CLI)
	"grok-3":           "Grok 3",
	"grok-code-fast-1": "Grok Code Fast",
	"grok-4-latest":    "Grok 4",
	// Grok (API)
	"grok-4.6":                    "Grok 4.6",
	"grok-4.20":                   "Grok 4.20",
	"grok-build-0.1":              "Grok Build 0.1",
	"grok-4-1-fast-reasoning":     "Grok 4.1 Fast",
	"grok-4-1-fast-non-reasoning": "Grok 4.1 Fast NR",
	// GLM (CLI)
	"zai-coding-plan/glm-5.2": "GLM-5.2",
	"zai-coding-plan/glm-4.7": "GLM-4.7",
	"glm-4":                   "GLM-4",
	// GLM (API)
	"glm-5.3":         "GLM-5.3",
	"glm-5.3-flash":   "GLM-5.3 Flash",
	"glm-5.2":         "GLM-5.2",
	"glm-4.7":         "GLM-4.7",
	"glm-4.6v-flashx": "GLM-4.6V FlashX",
}

// openRouterNamer resolves an OpenRouter slug to its catalog label (e.g.
// "deepseek/deepseek-v4" -> "DeepSeek: DeepSeek V4"). Set by cmd once the
// pricing catalog is loaded; nil (or a miss) falls back to the raw slug. A
// hook rather than an import keeps providers independent of pricing.
var openRouterNamer func(id string) (string, bool)

// SetOpenRouterNamer installs the slug -> display-name resolver used by
// DisplayName for slash-routed tokens. Safe to call with nil.
func SetOpenRouterNamer(fn func(id string) (string, bool)) {
	openRouterNamer = fn
}

// DisplayName returns a formatted name: {Company} {Model}. For an OpenRouter
// slash token the provider IS the model, so the catalog label (or the raw
// slug) is returned and the model argument is ignored.
func DisplayName(provider, model string) string {
	if IsOpenRouterModel(provider) {
		if openRouterNamer != nil {
			if name, ok := openRouterNamer(provider); ok && name != "" {
				return name
			}
		}
		return provider
	}

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
