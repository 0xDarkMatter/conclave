package judge

import (
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

func TestParseVerdictCleanJSON(t *testing.T) {
	input := `{
		"verdict": "YES",
		"confidence": "high",
		"reasoning": "All models agree.",
		"agreements": ["point 1", "point 2"],
		"disagreements": [],
		"recommendations": ["do this"]
	}`

	verdict, err := parseVerdict(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if verdict.Result != "YES" {
		t.Errorf("expected verdict YES, got %s", verdict.Result)
	}
	if verdict.Confidence != "high" {
		t.Errorf("expected confidence high, got %s", verdict.Confidence)
	}
	if len(verdict.Agreements) != 2 {
		t.Errorf("expected 2 agreements, got %d", len(verdict.Agreements))
	}
}

func TestParseVerdictMarkdownWrapped(t *testing.T) {
	input := "Here's my analysis:\n\n```json\n" + `{
		"verdict": "UNSAFE",
		"confidence": "medium",
		"reasoning": "Security issues found.",
		"agreements": ["vuln exists"],
		"disagreements": ["severity"],
		"recommendations": ["fix it"]
	}` + "\n```\n\nHope that helps!"

	verdict, err := parseVerdict(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if verdict.Result != "UNSAFE" {
		t.Errorf("expected verdict UNSAFE, got %s", verdict.Result)
	}
}

func TestParseVerdictMarkdownWrappedNoLang(t *testing.T) {
	input := "```\n" + `{"verdict": "NO", "confidence": "low", "reasoning": "test", "agreements": [], "disagreements": [], "recommendations": []}` + "\n```"

	verdict, err := parseVerdict(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if verdict.Result != "NO" {
		t.Errorf("expected verdict NO, got %s", verdict.Result)
	}
}

func TestParseVerdictEmbeddedJSON(t *testing.T) {
	input := `I've analyzed the responses. Here is my verdict:

{"verdict": "UNCERTAIN", "confidence": "low", "reasoning": "Models disagree significantly.", "agreements": [], "disagreements": ["everything"], "recommendations": ["get more opinions"]}

Let me know if you need more details.`

	verdict, err := parseVerdict(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if verdict.Result != "UNCERTAIN" {
		t.Errorf("expected verdict UNCERTAIN, got %s", verdict.Result)
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "json code block",
			input:    "```json\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "plain code block",
			input:    "```\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "no code block",
			input:    `{"key": "value"}`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFindJSONObject(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple object",
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "nested object",
			input:    `prefix {"outer": {"inner": "value"}} suffix`,
			expected: `{"outer": {"inner": "value"}}`,
		},
		{
			name:     "no object",
			input:    "no json here",
			expected: "",
		},
		{
			name:     "with text before",
			input:    "Here is the result: {\"verdict\": \"YES\"}",
			expected: `{"verdict": "YES"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findJSONObject(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBuildPrompt(t *testing.T) {
	responses := []providers.Response{
		{
			Provider: "gemini",
			Model:    "gemini-2.5-pro",
			Status:   "success",
			Response: "The code looks safe.",
		},
		{
			Provider: "openai",
			Model:    "gpt-5.2",
			Status:   "success",
			Response: "I found a potential issue.",
		},
		{
			Provider: "perplexity",
			Model:    "sonar-pro",
			Status:   "error",
			Error:    "timeout",
		},
	}

	prompt := BuildPrompt("Is this code secure?", responses, false)

	// Check that the prompt contains expected elements
	if !contains(prompt, "Is this code secure?") {
		t.Error("prompt should contain the original query")
	}
	if !contains(prompt, "gemini (gemini-2.5-pro)") {
		t.Error("prompt should contain gemini provider info")
	}
	if !contains(prompt, "The code looks safe.") {
		t.Error("prompt should contain gemini response")
	}
	if !contains(prompt, "[Error: timeout]") {
		t.Error("prompt should contain error for failed provider")
	}
	if !contains(prompt, "IMPORTANT: Respond ONLY with valid JSON") {
		t.Error("prompt should contain JSON instruction")
	}
}

func TestBuildPromptBlind(t *testing.T) {
	responses := []providers.Response{
		{
			Provider: "gemini",
			Model:    "gemini-2.5-pro",
			Status:   "success",
			Response: "Answer from Gemini",
		},
		{
			Provider: "claude",
			Model:    "sonnet",
			Status:   "success",
			Response: "Answer from Claude",
		},
	}

	prompt := BuildPrompt("What is 2+2?", responses, true)

	// Blind mode should use Provider A, Provider B instead of provider names
	if !contains(prompt, "Provider A") {
		t.Error("blind prompt should contain 'Provider A'")
	}
	if !contains(prompt, "Provider B") {
		t.Error("blind prompt should contain 'Provider B'")
	}
	if contains(prompt, "gemini") {
		t.Error("blind prompt should NOT contain provider name 'gemini'")
	}
	if contains(prompt, "claude") {
		t.Error("blind prompt should NOT contain provider name 'claude'")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
