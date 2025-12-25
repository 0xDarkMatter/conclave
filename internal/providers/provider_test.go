package providers

import (
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
)

func TestAllProvidersRegistered(t *testing.T) {
	providers := AllProviders()

	if len(providers) != 6 {
		t.Errorf("expected 6 providers, got %d", len(providers))
	}

	expectedNames := map[string]bool{
		"gemini":     false,
		"openai":     false,
		"claude":     false,
		"perplexity": false,
		"grok":       false,
		"glm":        false,
	}

	for _, p := range providers {
		if _, ok := expectedNames[p.Name()]; !ok {
			t.Errorf("unexpected provider: %s", p.Name())
		}
		expectedNames[p.Name()] = true
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("missing provider: %s", name)
		}
	}
}

func TestProviderDefaultModels(t *testing.T) {
	expectedModels := map[string]string{
		"gemini":     "gemini-3-pro-preview",
		"openai":     "gpt-5.2",
		"claude":     "opus",
		"perplexity": "sonar-pro",
		"grok":       "grok-code-fast-1",
		"glm":        "zai-coding-plan/glm-4.7",
	}

	providers := AllProviders()

	for _, p := range providers {
		expected, ok := expectedModels[p.Name()]
		if !ok {
			continue
		}
		if p.DefaultModel() != expected {
			t.Errorf("%s: expected model %s, got %s", p.Name(), expected, p.DefaultModel())
		}
	}
}

func TestRegistryGetProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := NewRegistry(cfg, false) // CLI mode

	// Test getting existing provider
	p, err := registry.GetProvider("gemini", nil)
	if err != nil {
		// May fail if gemini CLI not installed, that's okay
		t.Skipf("gemini not available: %v", err)
	}

	if p.Name() != "gemini" {
		t.Errorf("expected provider name 'gemini', got %s", p.Name())
	}
}

func TestRegistryGetProviderUnknown(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := NewRegistry(cfg, false) // CLI mode

	_, err := registry.GetProvider("unknown", nil)
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestRegistryGetProviders(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := NewRegistry(cfg, false) // CLI mode

	// This will only work if the CLIs are installed
	// So we just test that it doesn't panic
	_, _ = registry.GetProviders([]string{"gemini", "claude"}, nil)
}

func TestModelOverrideProvider(t *testing.T) {
	base := NewGeminiProvider()
	override := &modelOverrideProvider{
		Provider: base,
		model:    "custom-model",
	}

	if override.Name() != "gemini" {
		t.Errorf("expected name 'gemini', got %s", override.Name())
	}

	if override.DefaultModel() != "custom-model" {
		t.Errorf("expected model 'custom-model', got %s", override.DefaultModel())
	}
}

func TestModelOverrideProviderEmpty(t *testing.T) {
	base := NewGeminiProvider()
	override := &modelOverrideProvider{
		Provider: base,
		model:    "", // Empty override
	}

	// Should fall back to base provider's default
	if override.DefaultModel() != base.DefaultModel() {
		t.Errorf("expected model %s, got %s", base.DefaultModel(), override.DefaultModel())
	}
}

func TestGeminiProvider(t *testing.T) {
	p := NewGeminiProvider()

	if p.Name() != "gemini" {
		t.Errorf("expected name 'gemini', got %s", p.Name())
	}

	if p.DefaultModel() != "gemini-3-pro-preview" {
		t.Errorf("expected model 'gemini-3-pro-preview', got %s", p.DefaultModel())
	}
}

func TestOpenAIProvider(t *testing.T) {
	p := NewOpenAIProvider()

	if p.Name() != "openai" {
		t.Errorf("expected name 'openai', got %s", p.Name())
	}

	if p.DefaultModel() != "gpt-5.2" {
		t.Errorf("expected model 'gpt-5.2', got %s", p.DefaultModel())
	}
}

func TestClaudeProvider(t *testing.T) {
	p := NewClaudeProvider()

	if p.Name() != "claude" {
		t.Errorf("expected name 'claude', got %s", p.Name())
	}

	if p.DefaultModel() != "opus" {
		t.Errorf("expected model 'opus', got %s", p.DefaultModel())
	}
}

func TestPerplexityProvider(t *testing.T) {
	p := NewPerplexityProvider()

	if p.Name() != "perplexity" {
		t.Errorf("expected name 'perplexity', got %s", p.Name())
	}

	if p.DefaultModel() != "sonar-pro" {
		t.Errorf("expected model 'sonar-pro', got %s", p.DefaultModel())
	}
}

func TestGrokProvider(t *testing.T) {
	p := NewGrokProvider()

	if p.Name() != "grok" {
		t.Errorf("expected name 'grok', got %s", p.Name())
	}

	if p.DefaultModel() != "grok-code-fast-1" {
		t.Errorf("expected model 'grok-code-fast-1', got %s", p.DefaultModel())
	}
}

func TestGLMProvider(t *testing.T) {
	p := NewGLMProvider()

	if p.Name() != "glm" {
		t.Errorf("expected name 'glm', got %s", p.Name())
	}

	if p.DefaultModel() != "zai-coding-plan/glm-4.7" {
		t.Errorf("expected model 'zai-coding-plan/glm-4.7', got %s", p.DefaultModel())
	}
}
