package providers

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// mockProvider implements Provider without Preflighter
type mockProvider struct {
	name string
}

func (m *mockProvider) Name() string         { return m.name }
func (m *mockProvider) DefaultModel() string { return "test-model" }
func (m *mockProvider) IsAvailable() bool    { return true }
func (m *mockProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	return "", 0, nil, nil
}

// mockPreflightProvider implements both Provider and Preflighter
type mockPreflightProvider struct {
	mockProvider
	err error
}

func (m *mockPreflightProvider) Preflight(ctx context.Context) error {
	return m.err
}

// mockSlowPreflighter blocks until context is cancelled
type mockSlowPreflighter struct {
	mockProvider
}

func (m *mockSlowPreflighter) Preflight(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestRunPreflightNoPreflighters(t *testing.T) {
	providers := []Provider{
		&mockProvider{name: "a"},
		&mockProvider{name: "b"},
	}
	failures := RunPreflight(context.Background(), providers)
	if len(failures) != 0 {
		t.Errorf("expected no failures, got %d", len(failures))
	}
}

func TestRunPreflightAllPass(t *testing.T) {
	providers := []Provider{
		&mockPreflightProvider{mockProvider: mockProvider{name: "a"}, err: nil},
		&mockPreflightProvider{mockProvider: mockProvider{name: "b"}, err: nil},
	}
	failures := RunPreflight(context.Background(), providers)
	if len(failures) != 0 {
		t.Errorf("expected no failures, got %d", len(failures))
	}
}

func TestRunPreflightSomeFail(t *testing.T) {
	providers := []Provider{
		&mockPreflightProvider{mockProvider: mockProvider{name: "good"}, err: nil},
		&mockPreflightProvider{mockProvider: mockProvider{name: "bad"}, err: fmt.Errorf("not authenticated")},
	}
	failures := RunPreflight(context.Background(), providers)
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(failures))
	}
	if failures[0].Provider != "bad" {
		t.Errorf("expected provider 'bad', got %q", failures[0].Provider)
	}
	if failures[0].OK {
		t.Error("expected OK to be false")
	}
}

func TestRunPreflightTimeout(t *testing.T) {
	providers := []Provider{
		&mockSlowPreflighter{mockProvider: mockProvider{name: "slow"}},
	}
	start := time.Now()
	failures := RunPreflight(context.Background(), providers)
	elapsed := time.Since(start)

	if len(failures) != 1 {
		t.Fatalf("expected 1 failure from timeout, got %d", len(failures))
	}
	if elapsed > 3*time.Second {
		t.Errorf("expected timeout within ~2s, took %v", elapsed)
	}
}

func TestRunPreflightWrapped(t *testing.T) {
	inner := &mockPreflightProvider{
		mockProvider: mockProvider{name: "wrapped"},
		err:          fmt.Errorf("key missing"),
	}
	wrapped := &modelOverrideProvider{Provider: inner, model: "custom-model"}

	failures := RunPreflight(context.Background(), []Provider{wrapped})
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure through wrapper, got %d", len(failures))
	}
	if failures[0].Provider != "wrapped" {
		t.Errorf("expected provider 'wrapped', got %q", failures[0].Provider)
	}
}

func TestGetRemediation(t *testing.T) {
	tests := []struct {
		provider string
		contains string
	}{
		{"claude", "claude auth login"},
		{"gemini", "GEMINI_API_KEY"},
		{"openai", "OPENAI_API_KEY"},
		{"grok", "XAI_API_KEY"},
		{"perplexity", "PERPLEXITY_API_KEY"},
		{"unknown", "documentation"},
	}
	for _, tt := range tests {
		got := getRemediation(tt.provider)
		if got == "" {
			t.Errorf("getRemediation(%q) returned empty", tt.provider)
		}
		if !containsStr(got, tt.contains) {
			t.Errorf("getRemediation(%q) = %q, want it to contain %q", tt.provider, got, tt.contains)
		}
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && contains(s, substr))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
