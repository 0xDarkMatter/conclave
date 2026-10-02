// Tests for the `conclave decide` renderers. The JSON golden pins the
// envelope shape from docs/PLAN-decision-models.md byte for byte; regenerate
// it with `go test ./internal/decide -run RenderJSON -update` only when the
// shape change is intended, and update the plan's example in the same commit.
package decide

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func f64(v float64) *float64 { return &v }

// planExample is the plan's --json scenario: clef answered, jev failed.
func planExample() (map[string]providers.Question, map[string]DeciderResult) {
	qs := map[string]providers.Question{
		"department": {Type: providers.QuestionChoice, Instructions: "Route it.",
			Choices: map[string]string{"billing": "b", "technical": "t", "sales": "s"}},
		"frustration": {Type: providers.QuestionScore, Instructions: "How upset?",
			Scale: []string{"Calm", "Frustrated", "Angry"}},
		"is_urgent": {Type: providers.QuestionNoul, Instructions: "Urgent?"},
	}
	results := map[string]DeciderResult{
		"clef": {
			Model: "@cf/cloudflare/clef", Status: StatusSuccess, DurationMs: 212,
			Answers: map[string]providers.Answer{
				"department": {Type: providers.QuestionChoice, Choice: "technical", Confidence: f64(0.78),
					Probabilities: map[string]float64{"technical": 0.85, "sales": 0, "billing": 0.15}},
				"frustration": {Type: providers.QuestionScore, Score: f64(1), Confidence: f64(1),
					Legend:        map[string]string{"0": "Calm", "1": "Frustrated", "2": "Angry"},
					Probabilities: map[string]float64{"0": 0, "1": 1, "2": 0}},
				"is_urgent": {Type: providers.QuestionNoul, Noul: f64(1)},
			},
			Metrics: &providers.Metrics{InputTokens: 392, OutputTokens: 65, CostUSD: 0.000094},
			Priced:  true,
		},
		"jev": {Status: StatusError, DurationMs: 41, Error: "HTTP 401: invalid key"},
	}
	return qs, results
}

// TestRenderJSONMatchesPlanShape (render_json_matches_plan_shape): the
// envelope is consumed by scripts; a renamed or re-nested key breaks them
// silently, so the exact bytes are pinned with a fixed elapsed time.
func TestRenderJSONMatchesPlanShape(t *testing.T) {
	qs, results := planExample()
	env := BuildEnvelope(qs, results, 215*time.Millisecond)
	var buf bytes.Buffer
	if err := RenderJSON(&buf, env); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "decide_envelope.golden.json")
	if *updateGolden {
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	// Normalise line endings: a Windows checkout with autocrlf must not fail
	// a byte-identical envelope.
	got := strings.ReplaceAll(buf.String(), "\r\n", "\n")
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("envelope drifted from %s:\n%s", golden, got)
	}
}

// TestRenderQuietPrintsOneConsensusAnswerPerLine: -q is the scripting form;
// it must be exactly id=value, sorted, with no styling escapes.
func TestRenderQuietPrintsOneConsensusAnswerPerLine(t *testing.T) {
	qs, results := planExample()
	var buf bytes.Buffer
	if err := RenderQuiet(&buf, BuildEnvelope(qs, results, 0)); err != nil {
		t.Fatal(err)
	}
	want := "department=technical\nfrustration=1\nis_urgent=1\n"
	if buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}
