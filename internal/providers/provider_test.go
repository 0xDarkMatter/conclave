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
		"claude":     "claude-opus-4-5-20251101",
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
	registry := NewRegistry(cfg, false, false) // CLI mode, not cheap

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
	registry := NewRegistry(cfg, false, false) // CLI mode, not cheap

	_, err := registry.GetProvider("unknown", nil)
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestRegistryGetProviders(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := NewRegistry(cfg, false, false) // CLI mode, not cheap

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

	if p.DefaultModel() != "claude-opus-4-5-20251101" {
		t.Errorf("expected model 'claude-opus-4-5-20251101', got %s", p.DefaultModel())
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

func TestKeyRotatorSingleKey(t *testing.T) {
	// Set up test env var
	t.Setenv("TEST_API_KEY", "key1")

	rotator := NewKeyRotator("TEST_API_KEY")

	if !rotator.HasKeys() {
		t.Error("expected HasKeys to be true")
	}

	if rotator.Count() != 1 {
		t.Errorf("expected 1 key, got %d", rotator.Count())
	}

	// Single key should always return the same key
	for i := 0; i < 5; i++ {
		if key := rotator.Next(); key != "key1" {
			t.Errorf("expected 'key1', got %s", key)
		}
	}
}

func TestKeyRotatorMultipleKeys(t *testing.T) {
	// Set up test env var with comma-separated keys
	t.Setenv("TEST_API_KEY", "key1,key2,key3")

	rotator := NewKeyRotator("TEST_API_KEY")

	if !rotator.HasKeys() {
		t.Error("expected HasKeys to be true")
	}

	if rotator.Count() != 3 {
		t.Errorf("expected 3 keys, got %d", rotator.Count())
	}

	// Should rotate through keys
	seen := make(map[string]int)
	for i := 0; i < 9; i++ {
		key := rotator.Next()
		seen[key]++
	}

	// Each key should be seen 3 times
	for _, key := range []string{"key1", "key2", "key3"} {
		if seen[key] != 3 {
			t.Errorf("expected key %s to be seen 3 times, got %d", key, seen[key])
		}
	}
}

func TestKeyRotatorWithSpaces(t *testing.T) {
	// Keys with whitespace should be trimmed
	t.Setenv("TEST_API_KEY", " key1 , key2 , key3 ")

	rotator := NewKeyRotator("TEST_API_KEY")

	if rotator.Count() != 3 {
		t.Errorf("expected 3 keys, got %d", rotator.Count())
	}

	// Verify keys are trimmed
	keys := make(map[string]bool)
	for i := 0; i < 3; i++ {
		keys[rotator.Next()] = true
	}

	for _, expected := range []string{"key1", "key2", "key3"} {
		if !keys[expected] {
			t.Errorf("expected key %s (trimmed), but not found", expected)
		}
	}
}

func TestKeyRotatorFallback(t *testing.T) {
	// Primary not set, fallback is set
	t.Setenv("TEST_API_KEY", "")
	t.Setenv("TEST_FALLBACK_KEY", "fallback_key")

	rotator := NewKeyRotator("TEST_API_KEY", "TEST_FALLBACK_KEY")

	if !rotator.HasKeys() {
		t.Error("expected HasKeys to be true with fallback")
	}

	if key := rotator.Next(); key != "fallback_key" {
		t.Errorf("expected 'fallback_key', got %s", key)
	}
}

func TestKeyRotatorEmpty(t *testing.T) {
	t.Setenv("TEST_API_KEY", "")

	rotator := NewKeyRotator("TEST_API_KEY")

	if rotator.HasKeys() {
		t.Error("expected HasKeys to be false")
	}

	if rotator.Count() != 0 {
		t.Errorf("expected 0 keys, got %d", rotator.Count())
	}

	if key := rotator.Next(); key != "" {
		t.Errorf("expected empty string, got %s", key)
	}
}
