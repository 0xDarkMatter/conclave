package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config holds the conclave configuration
type Config struct {
	DefaultProviders []string          `mapstructure:"default_providers"`
	DefaultJudge     string            `mapstructure:"default_judge"`
	TimeoutSeconds   int               `mapstructure:"timeout_seconds"`
	Models           map[string]string `mapstructure:"models"`
	CheapModels      map[string]string `mapstructure:"cheap_models"`
	// Transports pins a provider to "cli" or "api" for bare tokens (ADR-012).
	// Precedence: a token's own @cli/@api suffix > this map > -g/-c. It exists
	// so a caller that always wants gemini metered and claude on the Max plan
	// need not spell the suffix on every invocation. Values are validated by
	// the registry, not here, so a typo fails the run with the provider named.
	Transports     map[string]string `mapstructure:"transports"`
	MaxFileSize    int64             `mapstructure:"max_file_size"`
	MaxContextSize int64             `mapstructure:"max_context_size"`
	WarnFileSize   int64             `mapstructure:"warn_file_size"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		DefaultProviders: []string{"gemini", "openai", "claude"},
		DefaultJudge:     "claude",
		TimeoutSeconds:   60,
		// Defaults are verified against the OpenRouter catalog by
		// `conclave models --check`; keep them to ids that feed lists, or
		// every run prints a drift warning. Mirror any change in
		// docs/MODEL_REGISTRY.md and the per-provider defaultModel fields.
		// Last verified live (CLI + API) 2026-09-08.
		Models: map[string]string{
			"gemini":     "gemini-3.1-pro-preview",
			"openai":     "gpt-5.6-sol",
			"claude":     "claude-opus-5",
			"perplexity": "sonar-pro",
			"grok":       "grok-4.7", // the only id the grok CLI accepts (grok models, 2026-09-25); API serves it too
			"glm":        "glm-5.3",
		},
		CheapModels: map[string]string{
			"gemini":     "gemini-3-flash-preview",
			"openai":     "gpt-5-nano",
			"claude":     "claude-haiku-4-5-20251001",
			"perplexity": "sonar",
			"grok":       "grok-build-0.1", // cheapest grok still listed; grok-4-1-fast-* work on xAI's API but are unlisted
			"glm":        "glm-5.3-flash",
		},
		Transports:     map[string]string{},
		MaxFileSize:    102400, // 100KB
		MaxContextSize: 512000, // 500KB
		WarnFileSize:   51200,  // 50KB
	}
}

// Load reads configuration from file and environment
func Load() (*Config, error) {
	cfg := DefaultConfig()

	// Set up Viper
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	// XDG config directory. With no resolvable home there is simply no config
	// FILE to read; the CONCLAVE_* environment overrides below must still
	// apply. Returning early here used to drop them all, including transport
	// pins (TestEnvOverridesApplyWhenHomeIsUnknown).
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			configDir = filepath.Join(home, ".config")
		}
	}
	if configDir != "" {
		v.AddConfigPath(filepath.Join(configDir, "conclave"))
	}

	// Environment variable overrides
	v.SetEnvPrefix("CONCLAVE")
	v.AutomaticEnv()

	// Read config file (ignore if not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	// Unmarshal into struct
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}

	// Override models from environment
	envModels := map[string]string{
		"CONCLAVE_GEMINI_MODEL":     "gemini",
		"CONCLAVE_OPENAI_MODEL":     "openai",
		"CONCLAVE_CLAUDE_MODEL":     "claude",
		"CONCLAVE_PERPLEXITY_MODEL": "perplexity",
		"CONCLAVE_GROK_MODEL":       "grok",
		"CONCLAVE_GLM_MODEL":        "glm",
	}
	for env, provider := range envModels {
		if val := os.Getenv(env); val != "" {
			cfg.Models[provider] = val
		}
	}

	// Override transports from environment (CONCLAVE_<PROVIDER>_TRANSPORT=cli|api)
	if cfg.Transports == nil {
		cfg.Transports = map[string]string{}
	}
	for _, provider := range []string{"gemini", "openai", "claude", "perplexity", "grok", "glm"} {
		env := "CONCLAVE_" + strings.ToUpper(provider) + "_TRANSPORT"
		if val := os.Getenv(env); val != "" {
			cfg.Transports[provider] = val
		}
	}

	// Override cheap models from environment
	envCheapModels := map[string]string{
		"CONCLAVE_CHEAP_GEMINI_MODEL":     "gemini",
		"CONCLAVE_CHEAP_OPENAI_MODEL":     "openai",
		"CONCLAVE_CHEAP_CLAUDE_MODEL":     "claude",
		"CONCLAVE_CHEAP_PERPLEXITY_MODEL": "perplexity",
		"CONCLAVE_CHEAP_GROK_MODEL":       "grok",
	}
	for env, provider := range envCheapModels {
		if val := os.Getenv(env); val != "" {
			cfg.CheapModels[provider] = val
		}
	}

	return cfg, nil
}

// GetModel returns the model for a provider, with optional override
func (c *Config) GetModel(provider string, override string) string {
	if override != "" {
		return override
	}
	if model, ok := c.Models[provider]; ok {
		return model
	}
	return ""
}

// GetTransport returns the configured transport for a provider ("cli", "api",
// or "" when unset). Not validated here; see Registry.GetProvider.
func (c *Config) GetTransport(provider string) string {
	if c == nil || c.Transports == nil {
		return ""
	}
	return strings.TrimSpace(c.Transports[provider])
}

// GetCheapModel returns the cheap model for a provider
func (c *Config) GetCheapModel(provider string) string {
	if model, ok := c.CheapModels[provider]; ok {
		return model
	}
	return ""
}
