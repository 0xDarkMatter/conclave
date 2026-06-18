package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config holds the conclave configuration
type Config struct {
	DefaultProviders []string          `mapstructure:"default_providers"`
	DefaultJudge     string            `mapstructure:"default_judge"`
	TimeoutSeconds   int               `mapstructure:"timeout_seconds"`
	Models           map[string]string `mapstructure:"models"`
	CheapModels      map[string]string `mapstructure:"cheap_models"`
	MaxFileSize      int64             `mapstructure:"max_file_size"`
	MaxContextSize   int64             `mapstructure:"max_context_size"`
	WarnFileSize     int64             `mapstructure:"warn_file_size"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		DefaultProviders: []string{"gemini", "openai", "claude"},
		DefaultJudge:     "claude",
		TimeoutSeconds:   60,
		Models: map[string]string{
			"gemini":     "gemini-3.1-pro-preview",
			"openai":     "gpt-5.5",
			"claude":     "claude-opus-4-8",
			"perplexity": "sonar-pro",
			"grok":       "grok-4-1-fast-reasoning",
			"glm":        "glm-5.2",
		},
		CheapModels: map[string]string{
			"gemini":     "gemini-3-flash-preview",
			"openai":     "gpt-5-nano",
			"claude":     "claude-haiku-4-5-20251001",
			"perplexity": "sonar",
			"grok":       "grok-4-1-fast-non-reasoning",
			"glm":        "glm-4.6v-flashx",
		},
		MaxFileSize:    102400,  // 100KB
		MaxContextSize: 512000,  // 500KB
		WarnFileSize:   51200,   // 50KB
	}
}

// Load reads configuration from file and environment
func Load() (*Config, error) {
	cfg := DefaultConfig()

	// Set up Viper
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	// XDG config directory
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return cfg, nil // Use defaults if we can't find home
		}
		configDir = filepath.Join(home, ".config")
	}
	v.AddConfigPath(filepath.Join(configDir, "conclave"))

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

// GetCheapModel returns the cheap model for a provider
func (c *Config) GetCheapModel(provider string) string {
	if model, ok := c.CheapModels[provider]; ok {
		return model
	}
	return ""
}
