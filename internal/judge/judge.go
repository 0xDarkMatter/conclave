package judge

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// Verdict holds the judge's synthesis
type Verdict struct {
	Result          string        `json:"verdict"`
	Confidence      string        `json:"confidence"`
	Reasoning       string        `json:"reasoning"`
	Agreements      []string      `json:"agreements"`
	Disagreements   []string      `json:"disagreements"`
	Recommendations []string      `json:"recommendations"`
	JudgeProvider   string        `json:"judge_provider"`
	JudgeModel      string        `json:"judge_model"`
	JudgeDuration   time.Duration `json:"judge_duration_ms"`
	JudgeTokens     int           `json:"judge_tokens"`
	RawResponse     string        `json:"-"` // For debugging
}

// Judge synthesizes verdicts from multiple provider responses
type Judge struct {
	provider providers.Provider
}

// New creates a new judge
func New(provider providers.Provider) *Judge {
	return &Judge{provider: provider}
}

// Synthesize creates a verdict from provider responses
func (j *Judge) Synthesize(ctx context.Context, query string, responses []providers.Response, timeoutSeconds int, blind bool) (*Verdict, error) {
	prompt := BuildPrompt(query, responses, blind)

	// Create timeout context
	judgeCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	model := j.provider.DefaultModel()
	response, duration, metrics, err := j.provider.Query(judgeCtx, prompt, model)
	if err != nil {
		return nil, err
	}

	// Calculate total tokens
	var totalTokens int
	if metrics != nil {
		totalTokens = metrics.InputTokens + metrics.OutputTokens
	}

	verdict, err := parseVerdict(response)
	if err != nil {
		// Return partial verdict with raw response
		return &Verdict{
			Result:        "PARSE_ERROR",
			Confidence:    "low",
			Reasoning:     "Failed to parse judge response: " + err.Error(),
			RawResponse:   response,
			JudgeProvider: j.provider.Name(),
			JudgeModel:    model,
			JudgeDuration: duration,
			JudgeTokens:   totalTokens,
		}, nil
	}

	verdict.JudgeProvider = j.provider.Name()
	verdict.JudgeModel = model
	verdict.JudgeDuration = duration
	verdict.JudgeTokens = totalTokens
	verdict.RawResponse = response

	return verdict, nil
}

// parseVerdict extracts JSON from the judge response
func parseVerdict(response string) (*Verdict, error) {
	// Try to parse directly first
	var verdict Verdict
	if err := json.Unmarshal([]byte(response), &verdict); err == nil {
		return &verdict, nil
	}

	// Try to extract JSON from markdown code blocks
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		// Try to find JSON object in response
		jsonStr = findJSONObject(response)
	}

	if jsonStr == "" {
		return nil, json.Unmarshal([]byte(response), &verdict) // Return original error
	}

	if err := json.Unmarshal([]byte(jsonStr), &verdict); err != nil {
		return nil, err
	}

	return &verdict, nil
}

// extractJSON extracts JSON from markdown code blocks
func extractJSON(s string) string {
	// Match ```json ... ``` or ``` ... ```
	re := regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(\\{.*?\\})\\s*```")
	matches := re.FindStringSubmatch(s)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// findJSONObject finds a JSON object in a string
func findJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		return ""
	}

	// Find matching closing brace
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}

	return ""
}
