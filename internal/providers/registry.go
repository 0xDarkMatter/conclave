package providers

import (
	"fmt"
	"strings"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
)

// Slash routing (ADR-010): in API mode a provider token containing "/" is an
// OpenRouter model id and is constructed on demand by GetProvider rather than
// registered up front. AllAPIProviders deliberately does NOT include OpenRouter
// so --all never fans out to the whole catalog; OpenRouterListing exists only
// so --list-providers and AnyAvailable can show the row.
//
// Per-provider transport (ADR-012): the registry holds BOTH provider sets and
// picks one per token. A bare token follows the global mode (general); a
// "@cli" / "@api" suffix overrides it for that provider alone. The suffix is
// stripped here and never reaches Name(); see transport.go.

// Registry manages provider instances
type Registry struct {
	config  *config.Config
	cli     map[string]Provider // CLI transport, keyed by bare name
	api     map[string]Provider // API transport, keyed by bare name (+ slash tokens built on demand)
	general bool                // default transport for bare tokens: true = API, false = CLI
	cheap   bool                // true = use cheaper/faster models (API transport only)
}

// NewRegistry creates a new provider registry.
// general sets the transport bare tokens resolve to (true = API providers for
// general-purpose queries, false = CLI wrappers); a token's own @cli/@api
// suffix wins over it. cheap selects the cheaper/faster models, which exist
// only for the API transport.
func NewRegistry(cfg *config.Config, general bool, cheap bool) *Registry {
	return newRegistryWith(cfg, general, cheap, AllCLIProviders(), AllAPIProviders())
}

// newRegistryWith is NewRegistry over explicit provider sets. It exists so
// tests can exercise resolution with fakes that are always available; the
// real sets depend on installed binaries and configured keys.
func newRegistryWith(cfg *config.Config, general, cheap bool, cli, api []Provider) *Registry {
	r := &Registry{
		config:  cfg,
		cli:     make(map[string]Provider),
		api:     make(map[string]Provider),
		general: general,
		cheap:   cheap,
	}
	for _, p := range cli {
		r.cli[p.Name()] = p
	}
	for _, p := range api {
		r.api[p.Name()] = p
	}
	return r
}

// DefaultTransport is the transport a bare token resolves to in this registry.
func (r *Registry) DefaultTransport() Transport {
	if r.general {
		return TransportAPI
	}
	return TransportCLI
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
		// NewGLMAPIProvider(), // Disabled: account balance required (ADR-006)
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

// GetProvider returns a single provider by token ("<name>[@cli|@api]"). A
// name containing "/" is an OpenRouter model (API transport only) and is
// built on first use; see the slash routing note at the top of this file.
// The returned provider reports the BARE name and carries its transport for
// TransportOf. Model overrides are keyed by bare name.
func (r *Registry) GetProvider(token string, modelOverrides map[string]string) (Provider, error) {
	name, transport, err := ParseProviderToken(token)
	if err != nil {
		return nil, err
	}
	// Precedence: suffix > config transports > global mode. The config layer
	// is a per-provider default, so it sits above the panel-wide -g/-c and
	// below anything typed on this invocation.
	explicit := transport != TransportDefault
	if !explicit {
		switch cfgT := r.config.GetTransport(name); cfgT {
		case "":
		case string(TransportCLI), string(TransportAPI):
			transport, explicit = Transport(cfgT), true
			if transport == TransportCLI && IsOpenRouterModel(name) {
				return nil, fmt.Errorf("config transports.%s is %q, but %q is an OpenRouter model and OpenRouter is API-only (ADR-010)", name, cfgT, name)
			}
		default:
			return nil, fmt.Errorf("config transports.%s is %q; want \"cli\" or \"api\" (config.yaml or CONCLAVE_%s_TRANSPORT)", name, cfgT, strings.ToUpper(name))
		}
	}
	if !explicit {
		transport = r.DefaultTransport()
	}

	if name == OpenRouterListingName {
		return nil, fmt.Errorf("%q is not a provider: name an OpenRouter model as vendor/model, e.g. -g deepseek/deepseek-v4-pro", name)
	}
	if !IsOpenRouterModel(name) && strings.Contains(name, "/") {
		return nil, fmt.Errorf("malformed OpenRouter slug %q: expected vendor/model", name)
	}

	var p Provider
	var ok bool
	if IsOpenRouterModel(name) {
		// ParseProviderToken already rejected "@cli" on a slug; this is the
		// bare-token-in-CLI-mode case.
		if transport != TransportAPI {
			return nil, fmt.Errorf("provider %q is an OpenRouter model (vendor/model) and OpenRouter is API-only: add -g or write %s@api", name, name)
		}
		if p, ok = r.api[name]; !ok {
			op := NewOpenRouterAPIProvider(name)
			if !op.IsAvailable() {
				return nil, fmt.Errorf("provider %s not available (%s not set)", name, OpenRouterKeyEnv)
			}
			// Cache so a token used as both panel member and judge shares one
			// instance (and one keyring lookup).
			r.api[name] = op
			p = op
		}
	} else {
		set, other := r.cli, r.api
		if transport == TransportAPI {
			set, other = r.api, r.cli
		}
		if p, ok = set[name]; !ok {
			if _, known := other[name]; known {
				return nil, noTransportError(name, transport, explicit)
			}
			return nil, fmt.Errorf("unknown provider: %s", name)
		}
	}

	if !p.IsAvailable() {
		if transport == TransportAPI {
			return nil, fmt.Errorf("provider %s not available (API key not set)", name)
		}
		return nil, fmt.Errorf("provider %s not available (CLI not installed)", name)
	}

	// Model priority: explicit -m flag > cheap mode (API transport only) >
	// config defaults (CLI transport) > provider default.
	//
	// The cheap map is API-oriented (ids like gpt-5-nano), so a provider pinned
	// to @cli under -c gets its normal CLI default instead: a cheap API id fed
	// to a CLI wrapper is at best ignored and at worst rejected.
	var model string
	if override, ok := modelOverrides[name]; ok && override != "" {
		model = override
	} else if r.cheap && transport == TransportAPI {
		model = r.config.GetCheapModel(name)
	} else if transport == TransportCLI {
		model = r.config.GetModel(name, "")
	}
	// API transport without override or cheap mode: empty, provider uses its default.

	return &modelOverrideProvider{Provider: p, model: model, transport: transport}, nil
}

// noTransportError explains a provider that exists but not on the requested
// transport. Today that is only glm on the API side (ADR-006), but the shape
// is generic so a future one-sided provider is described correctly.
func noTransportError(name string, transport Transport, explicit bool) error {
	if transport == TransportAPI {
		reason := "it has no API implementation"
		if name == "glm" {
			reason = "its API mode is disabled (ADR-006: pay-as-you-go endpoint latency and balance)"
		}
		if explicit {
			return fmt.Errorf("provider %s@api: %s; use %s@cli (the Coding Plan endpoint) instead", name, reason, name)
		}
		return fmt.Errorf("provider %s is not available in API mode: %s; drop -g or write %s@cli", name, reason, name)
	}
	if explicit {
		return fmt.Errorf("provider %s@cli: it has no CLI implementation; use %s@api instead", name, name)
	}
	return fmt.Errorf("provider %s is not available in CLI mode: it has no CLI implementation; add -g or write %s@api", name, name)
}

// GetProviders returns multiple providers by token. The same bare name may
// appear only once: --json keys responses by provider name and the progress
// display does too, so "claude@cli,claude@api" would silently drop one leg.
// Refusing it is the honest answer until provider identity carries the
// transport, which ADR-012 deliberately decided it does not.
func (r *Registry) GetProviders(tokens []string, modelOverrides map[string]string) ([]Provider, error) {
	var result []Provider
	firstToken := make(map[string]string, len(tokens))

	for _, token := range tokens {
		p, err := r.GetProvider(token, modelOverrides)
		if err != nil {
			return nil, err
		}
		if prev, dup := firstToken[p.Name()]; dup {
			return nil, fmt.Errorf("provider %s is listed twice (%s and %s); outputs are keyed by provider name, so each provider may appear once per panel", p.Name(), prev, strings.TrimSpace(token))
		}
		firstToken[p.Name()] = strings.TrimSpace(token)
		result = append(result, p)
	}

	return result, nil
}

// modelOverrideProvider wraps a provider with a custom model and records the
// transport it was resolved with. It is the ONLY place the transport lives
// after resolution; Name() stays the bare provider name by design (ADR-012).
type modelOverrideProvider struct {
	Provider
	model     string
	transport Transport
}

// Unwrap exposes the decorated provider so preflight can find an optional
// Preflighter that embedding does not promote. See unwrapPreflighter.
func (p *modelOverrideProvider) Unwrap() Provider { return p.Provider }

// Transport reports how this provider was resolved; see TransportOf.
func (p *modelOverrideProvider) Transport() Transport { return p.transport }

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
