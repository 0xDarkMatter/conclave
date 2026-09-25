package judge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// TestParseVerdictBracesInsideStrings: brace counting ignored JSON strings, so
// a "}" in the reasoning (code, set notation, a template) closed the object
// early and a perfectly good verdict became PARSE_ERROR.
func TestParseVerdictBracesInsideStrings(t *testing.T) {
	resp := "Here is my verdict:\n{\"verdict\": \"Use a map {k: v}\", \"confidence\": \"high\", \"reasoning\": \"both say x } y\"}\nThanks."
	v, err := parseVerdict(resp)
	if err != nil {
		t.Fatalf("parseVerdict: %v", err)
	}
	if v.Result != "Use a map {k: v}" || v.Reasoning != "both say x } y" {
		t.Fatalf("got %+v", v)
	}
}

// TestParseVerdictSkipsProseBraces: only the FIRST "{" was ever tried, so any
// brace in the preamble ("the {x} case") hid the real object that follows.
func TestParseVerdictSkipsProseBraces(t *testing.T) {
	resp := "Considering the {edge} case first.\n{\"verdict\": \"A\", \"confidence\": \"medium\", \"reasoning\": \"r\"}"
	v, err := parseVerdict(resp)
	if err != nil {
		t.Fatalf("parseVerdict: %v", err)
	}
	if v.Result != "A" {
		t.Fatalf("got verdict %q, want A", v.Result)
	}
}

// TestParseVerdictRejectsAnEmptyVerdict: "{}" or an object without "verdict"
// unmarshalled cleanly and rendered as a successful, blank verdict.
func TestParseVerdictRejectsAnEmptyVerdict(t *testing.T) {
	for _, resp := range []string{`{}`, `{"confidence": "high"}`, `{"verdict": "  "}`} {
		if v, err := parseVerdict(resp); err == nil {
			t.Fatalf("%s accepted as verdict %+v", resp, v)
		}
	}
}

type fixedJudge struct{ out string }

func (f fixedJudge) Name() string         { return "fake" }
func (f fixedJudge) DefaultModel() string { return "m" }
func (f fixedJudge) IsAvailable() bool    { return true }
func (f fixedJudge) Query(context.Context, string, string) (string, time.Duration, *providers.Metrics, error) {
	return f.out, time.Millisecond, nil, nil
}

// TestParseErrorKeepsTheJudgesText: RawResponse is not serialised, so on a
// parse failure the judge's actual words (often a perfectly readable prose
// verdict) vanished from both human and --json output.
func TestParseErrorKeepsTheJudgesText(t *testing.T) {
	const prose = "Both answers agree the answer is 42."
	v, err := New(fixedJudge{out: prose}).Synthesize(context.Background(), "q",
		[]providers.Response{{Provider: "a", Status: "success", Response: "x"}}, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	if v.Result != "PARSE_ERROR" || !strings.Contains(v.Reasoning, prose) {
		t.Fatalf("verdict %q, reasoning %q: the judge's text is lost", v.Result, v.Reasoning)
	}
}

// TestBlindPromptHidesWhoFailed: blind mode anonymised the headers but pasted
// each failure's error text verbatim, and provider errors name themselves
// ("claude CLI failed", "gemini API error", the model id), so the judge could
// still tell who was who.
func TestBlindPromptHidesWhoFailed(t *testing.T) {
	prompt := BuildPrompt("q", []providers.Response{
		{Provider: "gemini", Model: "gemini-3.1-pro", Status: "success", Response: "fine"},
		{Provider: "claude", Model: "claude-opus-5-5", Status: "error", Error: "claude CLI failed: claude-opus-5-5 overloaded"},
	}, true)
	for _, leak := range []string{"claude", "gemini"} {
		if strings.Contains(strings.ToLower(prompt), leak) {
			t.Fatalf("blind prompt names %q:\n%s", leak, prompt)
		}
	}
}
