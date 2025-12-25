package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Check default providers
	if len(cfg.DefaultProviders) != 3 {
		t.Errorf("expected 3 default providers, got %d", len(cfg.DefaultProviders))
	}

	// Check default judge
	if cfg.DefaultJudge != "claude" {
		t.Errorf("expected default judge 'claude', got %s", cfg.DefaultJudge)
	}

	// Check timeout
	if cfg.TimeoutSeconds != 60 {
		t.Errorf("expected timeout 60, got %d", cfg.TimeoutSeconds)
	}

	// Check all models are set
	expectedModels := map[string]string{
		"gemini":     "gemini-3-pro-preview",
		"openai":     "gpt-5.2",
		"claude":     "claude-opus-4-5-20251101",
		"perplexity": "sonar-pro",
		"grok":       "grok-code-fast-1",
		"glm":        "zai-coding-plan/glm-4.7",
	}

	for provider, expectedModel := range expectedModels {
		if cfg.Models[provider] != expectedModel {
			t.Errorf("expected %s model %s, got %s", provider, expectedModel, cfg.Models[provider])
		}
	}

	// Check size limits
	if cfg.MaxFileSize != 102400 {
		t.Errorf("expected max file size 102400, got %d", cfg.MaxFileSize)
	}
	if cfg.MaxContextSize != 512000 {
		t.Errorf("expected max context size 512000, got %d", cfg.MaxContextSize)
	}
}

func TestGetModel(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name     string
		provider string
		override string
		expected string
	}{
		{
			name:     "default model",
			provider: "gemini",
			override: "",
			expected: "gemini-3-pro-preview",
		},
		{
			name:     "with override",
			provider: "gemini",
			override: "gemini-2.5-flash",
			expected: "gemini-2.5-flash",
		},
		{
			name:     "unknown provider",
			provider: "unknown",
			override: "",
			expected: "",
		},
		{
			name:     "unknown provider with override",
			provider: "unknown",
			override: "custom-model",
			expected: "custom-model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.GetModel(tt.provider, tt.override)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestLoadWithEnvOverrides(t *testing.T) {
	// Save and restore env
	origGemini := os.Getenv("CONCLAVE_GEMINI_MODEL")
	origClaude := os.Getenv("CONCLAVE_CLAUDE_MODEL")
	defer func() {
		os.Setenv("CONCLAVE_GEMINI_MODEL", origGemini)
		os.Setenv("CONCLAVE_CLAUDE_MODEL", origClaude)
	}()

	// Set test env vars
	os.Setenv("CONCLAVE_GEMINI_MODEL", "gemini-test-model")
	os.Setenv("CONCLAVE_CLAUDE_MODEL", "opus")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Models["gemini"] != "gemini-test-model" {
		t.Errorf("expected gemini model 'gemini-test-model', got %s", cfg.Models["gemini"])
	}

	if cfg.Models["claude"] != "opus" {
		t.Errorf("expected claude model 'opus', got %s", cfg.Models["claude"])
	}

	// Other models should be defaults
	if cfg.Models["openai"] != "gpt-5.2" {
		t.Errorf("expected openai model 'gpt-5.2', got %s", cfg.Models["openai"])
	}
}

func TestLoadWithoutConfig(t *testing.T) {
	// Load should work without a config file
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg == nil {
		t.Fatal("config should not be nil")
	}

	// Should have defaults
	if cfg.DefaultJudge != "claude" {
		t.Errorf("expected default judge 'claude', got %s", cfg.DefaultJudge)
	}
}
