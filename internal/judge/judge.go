package judge

import (
	"context"
	"encoding/json"
	"errors"
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
	// JudgeTransport is "cli" or "api" (ADR-012): the judge is priced only
	// when it ran on the API, exactly like a panel member. Empty = unknown.
	JudgeTransport string `json:"judge_transport,omitempty"`
	RawResponse    string `json:"-"` // For debugging
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
			Result:     "PARSE_ERROR",
			Confidence: "low",
			// RawResponse is not serialised, so the judge's own words go in
			// Reasoning: a prose verdict is still useful to the reader.
			Reasoning:      "Failed to parse judge response: " + err.Error() + "\n\nJudge output:\n" + response,
			RawResponse:    response,
			JudgeProvider:  j.provider.Name(),
			JudgeModel:     model,
			JudgeDuration:  duration,
			JudgeTokens:    totalTokens,
			JudgeTransport: string(providers.TransportOf(j.provider)),
		}, nil
	}

	verdict.JudgeProvider = j.provider.Name()
	verdict.JudgeModel = model
	verdict.JudgeDuration = duration
	verdict.JudgeTokens = totalTokens
	verdict.JudgeTransport = string(providers.TransportOf(j.provider))
	verdict.RawResponse = response

	return verdict, nil
}

// parseVerdict extracts the verdict object from the judge response.
//
// Judges wrap JSON in prose and code fences, and their reasoning routinely
// contains braces (code, set notation). So every "{" is a candidate, braces
// are matched string-aware, and the first candidate that unmarshals AND has a
// non-empty "verdict" wins. An object without a verdict is not a verdict:
// accepting "{}" rendered a blank synthesis as success.
func parseVerdict(response string) (*Verdict, error) {
	candidates := []string{response}
	if fenced := extractJSON(response); fenced != "" {
		candidates = append(candidates, fenced)
	}
	for i := 0; i < len(response); i++ {
		if response[i] != '{' {
			continue
		}
		if obj := findJSONObject(response[i:]); obj != "" {
			candidates = append(candidates, obj)
		}
	}

	// Report the most useful failure: "valid JSON but no verdict" beats the
	// syntax error from trying to unmarshal the surrounding prose.
	var syntaxErr, emptyErr error
	for _, c := range candidates {
		var verdict Verdict
		if err := json.Unmarshal([]byte(c), &verdict); err != nil {
			if syntaxErr == nil {
				syntaxErr = err
			}
			continue
		}
		if strings.TrimSpace(verdict.Result) == "" {
			emptyErr = errors.New(`judge JSON has no "verdict" field`)
			continue
		}
		return &verdict, nil
	}
	if emptyErr != nil {
		return nil, emptyErr
	}
	return nil, syntaxErr
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

// findJSONObject returns the first balanced {...} in s. Braces inside JSON
// strings (and escaped quotes within them) do not count, so a "}" in the
// judge's reasoning cannot end the object early.
func findJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		return ""
	}

	depth := 0
	inString, escaped := false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
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
