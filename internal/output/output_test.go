package output

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/context"
	"github.com/0xDarkMatter/conclave-cli/internal/judge"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

func TestJSONOutput(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	formatter := New(Options{JSON: true})
	result := Result{
		Query:     "test query",
		Providers: []string{"gemini", "openai"},
		JudgeName: "claude",
		Responses: []providers.Response{
			{
				Provider: "gemini",
				Model:    "gemini-2.5-pro",
				Status:   "success",
				Response: "test response",
				Duration: 2 * time.Second,
			},
		},
		Verdict: &judge.Verdict{
			Result:          "YES",
			Confidence:      "high",
			Reasoning:       "test reasoning",
			Agreements:      []string{"point 1"},
			Disagreements:   []string{},
			Recommendations: []string{"rec 1"},
			JudgeDuration:   1 * time.Second,
		},
	}

	err := formatter.Render(result)
	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify it's valid JSON
	var jsonOut JSONOutput
	if err := json.Unmarshal([]byte(output), &jsonOut); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if jsonOut.Query != "test query" {
		t.Errorf("expected query 'test query', got %s", jsonOut.Query)
	}

	if jsonOut.Version != "1.0" {
		t.Errorf("expected version '1.0', got %s", jsonOut.Version)
	}

	if jsonOut.Verdict.Result != "YES" {
		t.Errorf("expected verdict 'YES', got %s", jsonOut.Verdict.Result)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"short", 10, "short"},
		{"this is a long string", 10, "this is..."},
		{"exact", 5, "exact"},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.max)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, result, tt.expected)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{500, "500B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.input)
		if result != tt.expected {
			t.Errorf("formatBytes(%d) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		input    string
		width    int
		expected string
	}{
		{"short text", 20, "short text"},
		{"this is a longer text that needs wrapping", 20, "this is a longer\ntext that needs\nwrapping"},
		{"", 20, ""},
	}

	for _, tt := range tests {
		result := wrapText(tt.input, tt.width)
		if result != tt.expected {
			t.Errorf("wrapText(%q, %d) = %q, want %q", tt.input, tt.width, result, tt.expected)
		}
	}
}

func TestMin(t *testing.T) {
	if min(1, 2) != 1 {
		t.Error("min(1, 2) should be 1")
	}
	if min(5, 3) != 3 {
		t.Error("min(5, 3) should be 3")
	}
	if min(4, 4) != 4 {
		t.Error("min(4, 4) should be 4")
	}
}

func TestNewFormatter(t *testing.T) {
	opts := Options{
		JSON:    true,
		Verbose: true,
		Brief:   false,
		Quiet:   false,
	}

	f := New(opts)
	if f == nil {
		t.Error("New() should not return nil")
	}
}

func TestResultWithContext(t *testing.T) {
	ctx := &context.Context{
		Sources: []context.Source{
			{Name: "test.go", SizeBytes: 100},
		},
		TotalBytes: 100,
	}

	result := Result{
		Query:     "test",
		Context:   ctx,
		Providers: []string{"gemini"},
	}

	if result.Context.TotalBytes != 100 {
		t.Errorf("expected total bytes 100, got %d", result.Context.TotalBytes)
	}
}
