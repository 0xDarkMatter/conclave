package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// mockProvider implements providers.Provider for testing
type mockProvider struct {
	name     string
	model    string
	response string
	err      error
	delay    time.Duration
}

func (m *mockProvider) Name() string         { return m.name }
func (m *mockProvider) DefaultModel() string { return m.model }
func (m *mockProvider) IsAvailable() bool    { return true }
func (m *mockProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return "", m.delay, ctx.Err()
		}
	}
	return m.response, m.delay, m.err
}

func TestOrchestratorRun(t *testing.T) {
	providerList := []providers.Provider{
		&mockProvider{name: "mock1", model: "model1", response: "response1"},
		&mockProvider{name: "mock2", model: "model2", response: "response2"},
	}

	orch := New(providerList, 30)
	results, err := orch.Run(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Check results are in order
	if results[0].Provider != "mock1" {
		t.Errorf("expected first provider 'mock1', got %s", results[0].Provider)
	}
	if results[1].Provider != "mock2" {
		t.Errorf("expected second provider 'mock2', got %s", results[1].Provider)
	}

	// Check responses
	if results[0].Response != "response1" {
		t.Errorf("expected response 'response1', got %s", results[0].Response)
	}
	if results[0].Status != "success" {
		t.Errorf("expected status 'success', got %s", results[0].Status)
	}
}

func TestOrchestratorRunWithError(t *testing.T) {
	providerList := []providers.Provider{
		&mockProvider{name: "mock1", model: "model1", response: "response1"},
		&mockProvider{name: "mock2", model: "model2", err: context.DeadlineExceeded},
	}

	orch := New(providerList, 30)
	results, err := orch.Run(context.Background(), "test prompt")

	// Should not return error if at least one provider succeeds
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[1].Status != "error" {
		t.Errorf("expected status 'error' for second provider, got %s", results[1].Status)
	}
}

func TestOrchestratorRunAllFail(t *testing.T) {
	providerList := []providers.Provider{
		&mockProvider{name: "mock1", model: "model1", err: context.DeadlineExceeded},
		&mockProvider{name: "mock2", model: "model2", err: context.Canceled},
	}

	orch := New(providerList, 30)
	_, err := orch.Run(context.Background(), "test prompt")

	// Should return error if all providers fail
	if err == nil {
		t.Error("expected error when all providers fail")
	}
}

func TestOrchestratorRunEmpty(t *testing.T) {
	orch := New(nil, 30)
	results, err := orch.Run(context.Background(), "test prompt")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestTotalDuration(t *testing.T) {
	responses := []providers.Response{
		{Duration: 1 * time.Second},
		{Duration: 3 * time.Second},
		{Duration: 2 * time.Second},
	}

	total := TotalDuration(responses)
	if total != 3*time.Second {
		t.Errorf("expected 3s, got %s", total)
	}
}

func TestTotalDurationEmpty(t *testing.T) {
	total := TotalDuration(nil)
	if total != 0 {
		t.Errorf("expected 0, got %s", total)
	}
}
