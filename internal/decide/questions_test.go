package decide

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// triageJSON is testdata/triage.yaml in the other supported format, with the
// question ids deliberately in a different order than the YAML file: if the
// loader is order-sensitive the agreement test below catches it.
const triageJSON = `{
  "is_urgent":   {"type": "noul", "instructions": "Must this be answered within an hour?"},
  "frustration": {"type": "score", "instructions": "How frustrated is the customer?",
                  "criteria": ["Calm", "Frustrated", "Angry"]},
  "department":  {"type": "choice", "instructions": "Which team should handle this ticket?",
                  "criteria": {"billing": "Invoice, refund or payment dispute",
                               "technical": "Bug, outage or integration failure",
                               "sales": "Plan and pricing questions"}}
}`

// triageCanonical is the canonical form of the triage set, hand-written so a
// drift in field order, key sorting or scale order breaks the test instead of
// silently re-keying the cache. Grammar: ids sorted, fields type/instructions/
// criteria in that order, choice labels sorted, scale order preserved, criteria
// omitted for noul.
const triageCanonical = `{"department":{"type":"choice","instructions":"Which team should handle this ticket?","choices":{"billing":"Invoice, refund or payment dispute","sales":"Plan and pricing questions","technical":"Bug, outage or integration failure"}},"frustration":{"type":"score","instructions":"How frustrated is the customer?","scale":["Calm","Frustrated","Angry"]},"is_urgent":{"type":"noul","instructions":"Must this be answered within an hour?"}}`

// triageSet is the triage.yaml content as literals, so tests can compare
// loaded files against a known set without going through the loader itself.
func triageSet() map[string]providers.Question {
	return map[string]providers.Question{
		"department": {
			Type:         providers.QuestionChoice,
			Instructions: "Which team should handle this ticket?",
			Choices: map[string]string{
				"billing":   "Invoice, refund or payment dispute",
				"technical": "Bug, outage or integration failure",
				"sales":     "Plan and pricing questions",
			},
		},
		"frustration": {
			Type:         providers.QuestionScore,
			Instructions: "How frustrated is the customer?",
			Scale:        []string{"Calm", "Frustrated", "Angry"},
		},
		"is_urgent": {
			Type:         providers.QuestionNoul,
			Instructions: "Must this be answered within an hour?",
		},
	}
}

// TestLoadQuestionsYAMLAndJSONAgree: the same question set must decode equally
// from both supported formats — a user pasting the JSON twin of a YAML file
// must not get a different panel.
func TestLoadQuestionsYAMLAndJSONAgree(t *testing.T) {
	fromYAML, err := LoadQuestions("testdata/triage.yaml")
	if err != nil {
		t.Fatalf("loading the YAML fixture: %v", err)
	}
	jsonPath := filepath.Join(t.TempDir(), "triage.json")
	if err := os.WriteFile(jsonPath, []byte(triageJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	fromJSON, err := LoadQuestions(jsonPath)
	if err != nil {
		t.Fatalf("loading the JSON twin: %v", err)
	}
	if !reflect.DeepEqual(fromYAML, fromJSON) {
		t.Fatalf("YAML and JSON twins disagree:\nYAML: %#v\nJSON: %#v", fromYAML, fromJSON)
	}
	if !reflect.DeepEqual(fromYAML, triageSet()) {
		t.Fatalf("testdata/triage.yaml decoded to the wrong set: %#v", fromYAML)
	}
}

// TestLoadQuestionsCriteriaShapeByType pins the wire polymorphism: criteria is
// a mapping for choice (-> Choices), an ordered sequence for score (-> Scale,
// index = score value), and absent for noul (neither field set).
func TestLoadQuestionsCriteriaShapeByType(t *testing.T) {
	qs, err := LoadQuestions("testdata/triage.yaml")
	if err != nil {
		t.Fatal(err)
	}
	choice := qs["department"]
	if choice.Type != providers.QuestionChoice || len(choice.Choices) != 3 ||
		choice.Choices["billing"] != "Invoice, refund or payment dispute" || choice.Scale != nil {
		t.Fatalf("choice question decoded wrong: %#v", choice)
	}
	score := qs["frustration"]
	if score.Type != providers.QuestionScore || !reflect.DeepEqual(score.Scale, []string{"Calm", "Frustrated", "Angry"}) ||
		score.Choices != nil {
		t.Fatalf("score question decoded wrong (scale order is semantic): %#v", score)
	}
	noul := qs["is_urgent"]
	if noul.Type != providers.QuestionNoul || noul.Choices != nil || noul.Scale != nil {
		t.Fatalf("noul question decoded wrong: %#v", noul)
	}
}

// TestLoadQuestionsBadFileNamesQuestion: every parse failure must name the
// file AND the offending question id, because a questions file can hold 64 of
// them and "cannot parse file" would send the user hunting.
func TestLoadQuestionsBadFileNamesQuestion(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		content  string
		want     []string // substrings the error must contain
	}{
		{
			name:     "choice criteria as a list",
			filename: "bad.yaml",
			content:  "severity:\n  type: choice\n  instructions: x\n  criteria: [a, b]\n",
			want:     []string{"bad.yaml", "severity", "choice"},
		},
		{
			name:     "score criteria as a mapping",
			filename: "bad.yaml",
			content:  "mood:\n  type: score\n  instructions: x\n  criteria:\n    a: b\n",
			want:     []string{"bad.yaml", "mood", "score"},
		},
		{
			name:     "unknown type",
			filename: "bad.yaml",
			content:  "tone:\n  type: vibe\n  instructions: x\n",
			want:     []string{"bad.yaml", "tone", "vibe"},
		},
		{
			name:     "noul with criteria",
			filename: "bad.yaml",
			content:  "ok:\n  type: noul\n  instructions: x\n  criteria: [a]\n",
			want:     []string{"bad.yaml", "ok", "noul"},
		},
		{
			name:     "top level is not a mapping of questions",
			filename: "bad.yaml",
			content:  "- one\n- two\n",
			want:     []string{"bad.yaml"},
		},
		{
			name:     "unsupported extension",
			filename: "questions.txt",
			content:  "{}\n",
			want:     []string{"questions.txt", ".txt"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.filename)
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadQuestions(path)
			if err == nil {
				t.Fatalf("parsing %s succeeded; want an error naming the file and question", tc.filename)
			}
			for _, sub := range tc.want {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error %q does not mention %q", err.Error(), sub)
				}
			}
		})
	}
}

// TestAskQuestionIsOneNoulNamedQ pins the --ask shorthand from the Phase 1
// command spec: exactly one noul question, id "q" — consensus and --json key
// answers by that id.
func TestAskQuestionIsOneNoulNamedQ(t *testing.T) {
	qs := AskQuestion("Is this urgent?")
	if len(qs) != 1 {
		t.Fatalf("--ask built %d questions, want 1", len(qs))
	}
	q, ok := qs["q"]
	if !ok || q.Type != providers.QuestionNoul || q.Instructions != "Is this urgent?" {
		t.Fatalf("--ask built the wrong question: %#v", q)
	}
}

// TestCanonicalQuestionsIgnoresMapOrder is the cache-key contract (ADR-016 on
// top of ADR-011): the same question set built in different insertion orders
// must canonicalise to the same string — otherwise identical decide calls
// would miss the cache at random — while a changed instruction must change it,
// or different calls would collide.
func TestCanonicalQuestionsIgnoresMapOrder(t *testing.T) {
	a := triageSet()
	b := make(map[string]providers.Question, len(a))
	for _, id := range []string{"is_urgent", "department", "frustration"} {
		b[id] = triageSet()[id]
	}
	if got, want := CanonicalQuestions(a), CanonicalQuestions(b); got != want {
		t.Fatalf("map insertion order changed the canonical form:\n%q\n%q", got, want)
	}
	if got := CanonicalQuestions(a); got != triageCanonical {
		t.Fatalf("canonical form drifted from the pinned grammar:\n got %s\nwant %s", got, triageCanonical)
	}
	// The loaded fixture must canonicalise identically to the literals: loader
	// and canonical form are one pipeline into the cache key.
	loaded, err := LoadQuestions("testdata/triage.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if CanonicalQuestions(loaded) != triageCanonical {
		t.Fatalf("testdata/triage.yaml canonicalises differently than the literal set")
	}
	mutated := triageSet()
	q := mutated["is_urgent"]
	q.Instructions = "Must this be answered within the hour?"
	mutated["is_urgent"] = q
	if CanonicalQuestions(mutated) == CanonicalQuestions(a) {
		t.Fatal("changing one instruction left the canonical form unchanged: different decisions would share a cache key")
	}
}
