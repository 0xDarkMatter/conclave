// Tests for Consensus, each named for the aggregation bug it prevents
// (ADR-016's averaging rules). Every test here was watched failing against
// the stub Consensus before the implementation landed. Fixtures use exact
// binary fractions (0.5, 0.25, 0.75) wherever two values must tie or compare
// equal, so float wobble cannot flip a test; everywhere else comparisons
// carry a 1e-9 tolerance.

package decide

import (
	"encoding/json"
	"math"
	"math/rand"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

func almostEqual(a, b float64) bool { return math.Abs(a-b) <= 1e-9 }

func fp(v float64) *float64 { return &v }

func dec(answers map[string]providers.Answer) *providers.Decision {
	return &providers.Decision{Model: "test-model", Answers: answers}
}

func mustGet(t *testing.T, got map[string]QuestionConsensus, qid string) QuestionConsensus {
	t.Helper()
	qc, ok := got[qid]
	if !ok {
		t.Fatalf("question %q missing from consensus output", qid)
	}
	return qc
}

// invalidCount reads the JSON contract rather than the Go field so the
// regression tests fail semantically against the pre-fix struct instead of
// stopping at compile time before exercising the old aggregation paths.
func invalidCount(t *testing.T, qc QuestionConsensus) int {
	t.Helper()
	b, err := json.Marshal(qc)
	if err != nil {
		t.Fatalf("marshal consensus: %v", err)
	}
	var wire struct {
		Invalid int `json:"invalid"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatalf("unmarshal consensus: %v", err)
	}
	return wire.Invalid
}

// === SECTION: baseline aggregation contracts ============================

// TestConsensusChoiceAveragesNotVotes: A votes x with 0.9/0.1, B votes y
// with 0.4/0.6. A vote count would call this a 1-1 tie; ADR-016's
// probability averaging picks x because (0.9+0.4)/2 beats (0.1+0.6)/2.
func TestConsensusChoiceAveragesNotVotes(t *testing.T) {
	questions := map[string]providers.Question{
		"q": {Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex", "y": "why"}},
	}
	decisions := map[string]*providers.Decision{
		"a": dec(map[string]providers.Answer{"q": {
			Type:          providers.QuestionChoice,
			Choice:        "x",
			Probabilities: map[string]float64{"x": 0.9, "y": 0.1},
		}}),
		"b": dec(map[string]providers.Answer{"q": {
			Type:          providers.QuestionChoice,
			Choice:        "y",
			Probabilities: map[string]float64{"x": 0.4, "y": 0.6},
		}}),
	}

	qc := mustGet(t, Consensus(questions, decisions, 2), "q")

	if qc.Choice != "x" {
		t.Fatalf("choice = %q, want %q (averaged probabilities, not the vote split)", qc.Choice, "x")
	}
	if p := qc.Probabilities["x"]; !almostEqual(p, 0.65) {
		t.Fatalf("P(x) = %v, want 0.65 (the averaged probability)", p)
	}
	if p := qc.Probabilities["y"]; !almostEqual(p, 0.35) {
		t.Fatalf("P(y) = %v, want 0.35", p)
	}
	if qc.Votes["x"] != 1 || qc.Votes["y"] != 1 {
		t.Fatalf("votes = %v, want {x:1 y:1} (each decider's own pick)", qc.Votes)
	}
	if !qc.Contested {
		t.Fatal("contested = false, want true (the deciders' own picks differ)")
	}
	if qc.Succeeded != 2 || qc.Requested != 2 {
		t.Fatalf("succeeded/requested = %d/%d, want 2/2", qc.Succeeded, qc.Requested)
	}
}

// TestConsensusNoulSplitIsContested: 0.8 and 0.3 sit on opposite sides of
// the 0.5 boundary, so the mean 0.55 must still be flagged contested — the
// mean alone would hide that the panel disagrees about the boolean.
func TestConsensusNoulSplitIsContested(t *testing.T) {
	questions := map[string]providers.Question{
		"q": {Type: providers.QuestionNoul, Instructions: "is it urgent?"},
	}
	decisions := map[string]*providers.Decision{
		"a": dec(map[string]providers.Answer{"q": {Type: providers.QuestionNoul, Noul: fp(0.8)}}),
		"b": dec(map[string]providers.Answer{"q": {Type: providers.QuestionNoul, Noul: fp(0.3)}}),
	}

	qc := mustGet(t, Consensus(questions, decisions, 2), "q")

	if qc.Noul == nil || !almostEqual(*qc.Noul, 0.55) {
		t.Fatalf("noul = %v, want 0.55 (the mean)", qc.Noul)
	}
	if !qc.Contested {
		t.Fatal("contested = false, want true (one decider >= 0.5, one < 0.5)")
	}
	if qc.Succeeded != 2 {
		t.Fatalf("succeeded = %d, want 2", qc.Succeeded)
	}
}

// TestConsensusNoulHalfCountsAsYes: 0.5 is pinned to the yes side of the
// boundary, so 0.5 and 0.9 are both yes and the split rule must not fire.
func TestConsensusNoulHalfCountsAsYes(t *testing.T) {
	questions := map[string]providers.Question{
		"q": {Type: providers.QuestionNoul},
	}
	decisions := map[string]*providers.Decision{
		"a": dec(map[string]providers.Answer{"q": {Type: providers.QuestionNoul, Noul: fp(0.5)}}),
		"b": dec(map[string]providers.Answer{"q": {Type: providers.QuestionNoul, Noul: fp(0.9)}}),
	}

	qc := mustGet(t, Consensus(questions, decisions, 2), "q")

	if qc.Noul == nil || !almostEqual(*qc.Noul, 0.7) {
		t.Fatalf("noul = %v, want 0.7", qc.Noul)
	}
	if qc.Contested {
		t.Fatal("contested = true, want false (0.5 counts as yes: both deciders are yes)")
	}
}

// TestConsensusScoreExpectedVsArgmax: the averaged distribution peaks at
// index 2 but its mass leans low (expected 1.1), so rounding Expected lands
// on 1 — argmax and expected genuinely diverge, and each must be reported as
// itself, not derived from the other. The deciders' own argmaxes (0 and 1)
// differ by one step, so the question is also contested.
func TestConsensusScoreExpectedVsArgmax(t *testing.T) {
	questions := map[string]providers.Question{
		"q": {Type: providers.QuestionScore, Scale: []string{"low", "mid", "high"}},
	}
	decisions := map[string]*providers.Decision{
		"a": dec(map[string]providers.Answer{"q": {
			Type:          providers.QuestionScore,
			Score:         fp(0),
			Probabilities: map[string]float64{"0": 0.6, "1": 0.0, "2": 0.4},
		}}),
		"b": dec(map[string]providers.Answer{"q": {
			Type:          providers.QuestionScore,
			Score:         fp(1),
			Probabilities: map[string]float64{"0": 0.0, "1": 0.6, "2": 0.4},
		}}),
	}

	qc := mustGet(t, Consensus(questions, decisions, 2), "q")

	if qc.Score == nil || *qc.Score != 2 {
		t.Fatalf("score = %v, want 2 (argmax of the averaged distribution)", qc.Score)
	}
	if qc.Expected == nil || !almostEqual(*qc.Expected, 1.1) {
		t.Fatalf("expected = %v, want 1.1 (0*0.3 + 1*0.3 + 2*0.4)", qc.Expected)
	}
	for _, c := range []struct {
		label string
		want  float64
	}{{"0", 0.3}, {"1", 0.3}, {"2", 0.4}} {
		if p := qc.Probabilities[c.label]; !almostEqual(p, c.want) {
			t.Fatalf("P(%s) = %v, want %v", c.label, p, c.want)
		}
	}
	if qc.Votes["0"] != 1 || qc.Votes["1"] != 1 {
		t.Fatalf("votes = %v, want {0:1 1:1} (each decider's own argmax)", qc.Votes)
	}
	if !qc.Contested {
		t.Fatal("contested = false, want true (argmaxes 0 and 1 differ by one step)")
	}
}

// TestConsensusIdenticalAnswersAgreementOne: identical distributions have
// zero divergence, so agreement must be ~1 (the tolerance covers last-ulp
// wobble in the averaging before the entropy terms cancel).
func TestConsensusIdenticalAnswersAgreementOne(t *testing.T) {
	questions := map[string]providers.Question{
		"q": {Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex", "y": "why"}},
	}
	answer := func() providers.Answer {
		return providers.Answer{
			Type: providers.QuestionChoice, Choice: "x",
			Probabilities: map[string]float64{"x": 0.9, "y": 0.1},
		}
	}
	decisions := map[string]*providers.Decision{
		"a": dec(map[string]providers.Answer{"q": answer()}),
		"b": dec(map[string]providers.Answer{"q": answer()}),
	}

	qc := mustGet(t, Consensus(questions, decisions, 2), "q")

	if qc.Agreement == nil || math.Abs(*qc.Agreement-1) > 1e-12 {
		t.Fatalf("agreement = %v, want ~1.0 (identical distributions)", qc.Agreement)
	}
	if qc.Contested {
		t.Fatal("contested = true, want false (identical picks)")
	}
	if qc.Votes["x"] != 2 {
		t.Fatalf("votes = %v, want {x:2}", qc.Votes)
	}
}

// TestConsensusOppositeAnswersAgreementLow: disjoint supports are the JSD
// worst case (1 bit of divergence), so agreement bottoms out at ~0. It is
// reported, not thresholded — the plan leaves "too contested" to callers.
func TestConsensusOppositeAnswersAgreementLow(t *testing.T) {
	questions := map[string]providers.Question{
		"q": {Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex", "y": "why"}},
	}
	decisions := map[string]*providers.Decision{
		"a": dec(map[string]providers.Answer{"q": {
			Type:          providers.QuestionChoice,
			Choice:        "x",
			Probabilities: map[string]float64{"x": 1.0, "y": 0.0},
		}}),
		"b": dec(map[string]providers.Answer{"q": {
			Type:          providers.QuestionChoice,
			Choice:        "y",
			Probabilities: map[string]float64{"x": 0.0, "y": 1.0},
		}}),
	}

	qc := mustGet(t, Consensus(questions, decisions, 2), "q")

	if qc.Agreement == nil || *qc.Agreement >= 0.05 {
		t.Fatalf("agreement = %v, want < 0.05 (disjoint supports)", qc.Agreement)
	}
	if !qc.Contested {
		t.Fatal("contested = false, want true (opposite picks)")
	}
}

// TestConsensusSingleDeciderAgreementNil: divergence needs two things to
// diverge; with one usable distribution Agreement must be nil, not a fake 1.
func TestConsensusSingleDeciderAgreementNil(t *testing.T) {
	cases := []struct {
		name     string
		question providers.Question
		answer   providers.Answer
	}{
		{
			name:     "choice",
			question: providers.Question{Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex", "y": "why"}},
			answer:   providers.Answer{Type: providers.QuestionChoice, Choice: "x", Probabilities: map[string]float64{"x": 0.9, "y": 0.1}},
		},
		{
			name:     "score",
			question: providers.Question{Type: providers.QuestionScore, Scale: []string{"low", "high"}},
			answer:   providers.Answer{Type: providers.QuestionScore, Probabilities: map[string]float64{"0": 0.3, "1": 0.7}},
		},
		{
			name:     "noul",
			question: providers.Question{Type: providers.QuestionNoul},
			answer:   providers.Answer{Type: providers.QuestionNoul, Noul: fp(0.8)},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			questions := map[string]providers.Question{"q": c.question}
			decisions := map[string]*providers.Decision{
				"solo": dec(map[string]providers.Answer{"q": c.answer}),
			}
			qc := mustGet(t, Consensus(questions, decisions, 1), "q")
			if qc.Agreement != nil {
				t.Fatalf("agreement = %v, want nil (one distribution has no divergence)", *qc.Agreement)
			}
			if qc.Contested {
				t.Fatal("contested = true, want false (a single decider cannot disagree)")
			}
		})
	}
}

// TestConsensusMissingAnswerLowersSucceeded: Succeeded is per question, not
// per panel — a decider that skipped a question, or answered it with the
// wrong type, must not count there, and a question nobody answered must
// still appear with Succeeded 0.
func TestConsensusMissingAnswerLowersSucceeded(t *testing.T) {
	questions := map[string]providers.Question{
		"triage":  {Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex", "y": "why"}},
		"urgency": {Type: providers.QuestionNoul},
		"routing": {Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex", "y": "why"}},
	}
	decisions := map[string]*providers.Decision{
		// answers triage and urgency, skips routing entirely
		"a": dec(map[string]providers.Answer{
			"triage":  {Type: providers.QuestionChoice, Choice: "x", Probabilities: map[string]float64{"x": 0.9, "y": 0.1}},
			"urgency": {Type: providers.QuestionNoul, Noul: fp(0.8)},
		}),
		// answers triage with a noul-typed answer: mismatched, must be skipped
		"b": dec(map[string]providers.Answer{
			"triage": {Type: providers.QuestionNoul, Noul: fp(0.4)},
		}),
		// answers triage correctly
		"c": dec(map[string]providers.Answer{
			"triage": {Type: providers.QuestionChoice, Choice: "x", Probabilities: map[string]float64{"x": 0.7, "y": 0.3}},
		}),
	}

	got := Consensus(questions, decisions, 3)

	if qc := mustGet(t, got, "triage"); qc.Succeeded != 2 || invalidCount(t, qc) != 1 {
		t.Fatalf("triage succeeded/invalid = %d/%d, want 2/1 (a and c valid; b's noul-typed answer is malformed)", qc.Succeeded, invalidCount(t, qc))
	}
	if qc := mustGet(t, got, "urgency"); qc.Succeeded != 1 || invalidCount(t, qc) != 0 {
		t.Fatalf("urgency succeeded/invalid = %d/%d, want 1/0 (a only; missing answers are not malformed)", qc.Succeeded, invalidCount(t, qc))
	}
	qc := mustGet(t, got, "routing")
	if qc.Succeeded != 0 || invalidCount(t, qc) != 0 {
		t.Fatalf("routing succeeded/invalid = %d/%d, want 0/0 (nobody answered)", qc.Succeeded, invalidCount(t, qc))
	}
	if qc.Contested || qc.Agreement != nil || qc.Choice != "" {
		t.Fatalf("unanswered question carries aggregates: %+v", qc)
	}
	if qc.Requested != 3 {
		t.Fatalf("routing requested = %d, want 3 (echoed even with no answers)", qc.Requested)
	}
}

// TestConsensusTieBreaksDeterministically: ties resolve to the smallest key
// in sort order — labels lexicographically, score indices numerically (a
// string sort of the wire's index keys would put "10" before "9").
func TestConsensusTieBreaksDeterministically(t *testing.T) {
	choiceQuestions := map[string]providers.Question{
		"q": {Type: providers.QuestionChoice, Choices: map[string]string{"a": "eh", "b": "bee", "x": "ex", "y": "why"}},
	}
	choiceCases := []struct {
		name      string
		decisions map[string]*providers.Decision
		want      string
	}{
		{
			name: "two deciders average to an exact tie",
			decisions: map[string]*providers.Decision{
				"a": dec(map[string]providers.Answer{"q": {Type: providers.QuestionChoice,
					Probabilities: map[string]float64{"b": 0.75, "a": 0.25}}}),
				"b": dec(map[string]providers.Answer{"q": {Type: providers.QuestionChoice,
					Probabilities: map[string]float64{"b": 0.25, "a": 0.75}}}),
			},
			want: "a", // 0.5/0.5 tie: "a" sorts before "b"
		},
		{
			name: "single decider exact tie",
			decisions: map[string]*providers.Decision{
				"solo": dec(map[string]providers.Answer{"q": {Type: providers.QuestionChoice,
					Probabilities: map[string]float64{"y": 0.5, "x": 0.5}}}),
			},
			want: "x",
		},
	}
	for _, c := range choiceCases {
		t.Run(c.name, func(t *testing.T) {
			qc := mustGet(t, Consensus(choiceQuestions, c.decisions, len(c.decisions)), "q")
			if qc.Choice != c.want {
				t.Fatalf("choice = %q, want %q (tie broken by label sort order)", qc.Choice, c.want)
			}
		})
	}

	// score ties: numeric index order, not the string order of the wire keys
	scale := make([]string, 12) // indices 0..11, so "10" exists as a wire key
	for i := range scale {
		scale[i] = "s" + string(rune('0'+i))
	}
	scoreQuestions := map[string]providers.Question{
		"q": {Type: providers.QuestionScore, Scale: scale},
	}
	scoreCases := []struct {
		name      string
		answer    providers.Answer
		wantScore int
	}{
		{
			name: "tie at the bottom of the scale",
			answer: providers.Answer{Type: providers.QuestionScore,
				Probabilities: map[string]float64{"0": 0.5, "1": 0.5}},
			wantScore: 0,
		},
		{
			name: "double-digit index must not win a tie by string order",
			answer: providers.Answer{Type: providers.QuestionScore,
				Probabilities: map[string]float64{"9": 0.5, "10": 0.5}},
			wantScore: 9,
		},
	}
	for _, c := range scoreCases {
		t.Run(c.name, func(t *testing.T) {
			decisions := map[string]*providers.Decision{
				"solo": dec(map[string]providers.Answer{"q": c.answer}),
			}
			qc := mustGet(t, Consensus(scoreQuestions, decisions, 1), "q")
			if qc.Score == nil || *qc.Score != c.wantScore {
				t.Fatalf("score = %v, want %d (tie broken by numeric index)", qc.Score, c.wantScore)
			}
		})
	}
}

// === SECTION: adversarial validation regressions ========================

// TestConsensusThreeDeciderAgreementNormalizesGeneralizedJSD pins Lin's
// n-way upper bound: generalized JSD is divided by log2(n), preserving useful
// resolution between partial overlap and fully disjoint support.
func TestConsensusThreeDeciderAgreementNormalizesGeneralizedJSD(t *testing.T) {
	questions := map[string]providers.Question{
		"q": {Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex", "y": "why", "z": "zee"}},
	}
	decisions := map[string]*providers.Decision{
		"a": dec(map[string]providers.Answer{"q": {Type: providers.QuestionChoice, Probabilities: map[string]float64{"x": 1, "y": 0, "z": 0}}}),
		"b": dec(map[string]providers.Answer{"q": {Type: providers.QuestionChoice, Probabilities: map[string]float64{"x": 0, "y": 1, "z": 0}}}),
		"c": dec(map[string]providers.Answer{"q": {Type: providers.QuestionChoice, Probabilities: map[string]float64{"x": 0.5, "y": 0, "z": 0.5}}}),
	}

	qc := mustGet(t, Consensus(questions, decisions, 3), "q")
	const want = 0.289690082143
	if qc.Agreement == nil || math.Abs(*qc.Agreement-want) > 1e-12 {
		t.Fatalf("agreement = %v, want %.12f (1 - JSD/log2(3))", qc.Agreement, want)
	}
}

// TestConsensusRejectsUnknownChoiceLabels ensures the question criteria,
// rather than vendor output, define the only labels eligible for consensus.
func TestConsensusRejectsUnknownChoiceLabels(t *testing.T) {
	question := providers.Question{Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex", "y": "why"}}
	cases := []struct {
		name string
		bad  providers.Answer
	}{
		{
			name: "probability label",
			bad: providers.Answer{Type: providers.QuestionChoice, Choice: "x",
				Probabilities: map[string]float64{"x": 0.5, "injected": 0.5}},
		},
		{
			name: "declared choice",
			bad: providers.Answer{Type: providers.QuestionChoice, Choice: "injected",
				Probabilities: map[string]float64{"x": 1, "y": 0}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decisions := map[string]*providers.Decision{
				"bad":  dec(map[string]providers.Answer{"q": tc.bad}),
				"good": dec(map[string]providers.Answer{"q": {Type: providers.QuestionChoice, Choice: "y", Probabilities: map[string]float64{"x": 0, "y": 1}}}),
			}
			qc := mustGet(t, Consensus(map[string]providers.Question{"q": question}, decisions, 2), "q")
			if qc.Succeeded != 1 || invalidCount(t, qc) != 1 {
				t.Fatalf("succeeded/invalid = %d/%d, want 1/1", qc.Succeeded, invalidCount(t, qc))
			}
			if qc.Choice != "y" || qc.Probabilities["injected"] != 0 {
				t.Fatalf("choice/probabilities = %q/%v, want criteria-only y consensus", qc.Choice, qc.Probabilities)
			}
		})
	}
}

// TestConsensusNormalizesEachDeciderBeforeEqualWeightAverage distinguishes
// equal decider weight from pooling raw vendor mass.
func TestConsensusNormalizesEachDeciderBeforeEqualWeightAverage(t *testing.T) {
	questions := map[string]providers.Question{
		"q": {Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex", "y": "why"}},
	}
	decisions := map[string]*providers.Decision{
		"a": dec(map[string]providers.Answer{"q": {Type: providers.QuestionChoice, Probabilities: map[string]float64{"x": 9, "y": 1}}}),
		"b": dec(map[string]providers.Answer{"q": {Type: providers.QuestionChoice, Probabilities: map[string]float64{"x": 0, "y": 1}}}),
	}

	qc := mustGet(t, Consensus(questions, decisions, 2), "q")
	if qc.Choice != "y" || !almostEqual(qc.Probabilities["x"], 0.45) || !almostEqual(qc.Probabilities["y"], 0.55) {
		t.Fatalf("choice/probabilities = %q/%v, want y with {x:0.45 y:0.55}", qc.Choice, qc.Probabilities)
	}
}

// TestConsensusRejectsMalformedScoreKeys prevents aliases and invalid mass
// from being partially retained and renormalized into false certainty.
func TestConsensusRejectsMalformedScoreKeys(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{name: "noncanonical", key: "01"},
		{name: "nonnumeric", key: "low"},
		{name: "out of range", key: "2"},
	}
	questions := map[string]providers.Question{
		"q": {Type: providers.QuestionScore, Scale: []string{"low", "high"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answer := providers.Answer{Type: providers.QuestionScore, Probabilities: map[string]float64{"0": 0.1, tc.key: 0.9}}
			qc := mustGet(t, Consensus(questions, map[string]*providers.Decision{
				"bad": dec(map[string]providers.Answer{"q": answer}),
			}, 1), "q")
			if qc.Succeeded != 0 || invalidCount(t, qc) != 1 {
				t.Fatalf("succeeded/invalid = %d/%d, want 0/1", qc.Succeeded, invalidCount(t, qc))
			}
			if qc.Score != nil || qc.Probabilities != nil {
				t.Fatalf("malformed score contributed aggregate: %+v", qc)
			}
		})
	}
}

// TestConsensusRejectsMasslessAnswers ensures Succeeded means the answer
// actually supplied type-specific probability mass or a noul value.
func TestConsensusRejectsMasslessAnswers(t *testing.T) {
	questions := map[string]providers.Question{
		"choice": {Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex", "y": "why"}},
		"score":  {Type: providers.QuestionScore, Scale: []string{"low", "high"}},
		"noul":   {Type: providers.QuestionNoul},
	}
	decisions := map[string]*providers.Decision{
		"bad": dec(map[string]providers.Answer{
			"choice": {Type: providers.QuestionChoice, Choice: "x", Probabilities: map[string]float64{}},
			"score":  {Type: providers.QuestionScore, Probabilities: map[string]float64{"0": 0, "1": 0}},
			"noul":   {Type: providers.QuestionNoul, Noul: nil},
		}),
	}
	got := Consensus(questions, decisions, 1)
	for _, qid := range []string{"choice", "score", "noul"} {
		qc := mustGet(t, got, qid)
		if qc.Succeeded != 0 || invalidCount(t, qc) != 1 {
			t.Errorf("%s succeeded/invalid = %d/%d, want 0/1", qid, qc.Succeeded, invalidCount(t, qc))
		}
	}
}

// TestConsensusChoiceVotesUseDistributionArgmax pins voting to the same
// normalized evidence that drives the aggregate, not a vendor threshold.
func TestConsensusChoiceVotesUseDistributionArgmax(t *testing.T) {
	questions := map[string]providers.Question{
		"q": {Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex", "y": "why"}},
	}
	answer := providers.Answer{Type: providers.QuestionChoice, Choice: "x", Probabilities: map[string]float64{"x": 0.1, "y": 0.9}}
	decisions := map[string]*providers.Decision{
		"a": dec(map[string]providers.Answer{"q": answer}),
		"b": dec(map[string]providers.Answer{"q": answer}),
	}

	qc := mustGet(t, Consensus(questions, decisions, 2), "q")
	if qc.Votes["y"] != 2 || qc.Votes["x"] != 0 || qc.Contested {
		t.Fatalf("votes/contested = %v/%v, want {y:2}/false", qc.Votes, qc.Contested)
	}
}

// TestConsensusRejectsInvalidNumericValues protects JSON output and all
// probability math from NaN, infinities, negatives and out-of-range nouls.
func TestConsensusRejectsInvalidNumericValues(t *testing.T) {
	cases := []struct {
		name     string
		question providers.Question
		answer   providers.Answer
	}{
		{name: "choice NaN", question: providers.Question{Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex"}}, answer: providers.Answer{Type: providers.QuestionChoice, Probabilities: map[string]float64{"x": math.NaN()}}},
		{name: "choice negative", question: providers.Question{Type: providers.QuestionChoice, Choices: map[string]string{"x": "ex"}}, answer: providers.Answer{Type: providers.QuestionChoice, Probabilities: map[string]float64{"x": -0.1}}},
		{name: "score infinity", question: providers.Question{Type: providers.QuestionScore, Scale: []string{"low"}}, answer: providers.Answer{Type: providers.QuestionScore, Probabilities: map[string]float64{"0": math.Inf(1)}}},
		{name: "score negative", question: providers.Question{Type: providers.QuestionScore, Scale: []string{"low"}}, answer: providers.Answer{Type: providers.QuestionScore, Probabilities: map[string]float64{"0": -0.1}}},
		{name: "noul NaN", question: providers.Question{Type: providers.QuestionNoul}, answer: providers.Answer{Type: providers.QuestionNoul, Noul: fp(math.NaN())}},
		{name: "noul below zero", question: providers.Question{Type: providers.QuestionNoul}, answer: providers.Answer{Type: providers.QuestionNoul, Noul: fp(-0.1)}},
		{name: "noul above one", question: providers.Question{Type: providers.QuestionNoul}, answer: providers.Answer{Type: providers.QuestionNoul, Noul: fp(1.1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			qc := mustGet(t, Consensus(map[string]providers.Question{"q": tc.question}, map[string]*providers.Decision{
				"bad": dec(map[string]providers.Answer{"q": tc.answer}),
			}, 1), "q")
			if qc.Succeeded != 0 || invalidCount(t, qc) != 1 {
				t.Fatalf("succeeded/invalid = %d/%d, want 0/1", qc.Succeeded, invalidCount(t, qc))
			}
		})
	}
}

// TestConsensusNormalizationIsByteDeterministic reconstructs the same map in
// shuffled insertion orders and requires the complete JSON output to match.
func TestConsensusNormalizationIsByteDeterministic(t *testing.T) {
	questions := map[string]providers.Question{
		"q": {Type: providers.QuestionChoice, Choices: map[string]string{"a": "a", "b": "b", "c": "c", "d": "d"}},
	}
	labels := []string{"a", "b", "c", "d"}
	values := map[string]float64{"a": 0.7, "b": 0.1, "c": 0.1, "d": 0.1}
	rng := rand.New(rand.NewSource(1))
	var baseline []byte
	for i := 0; i < 1000; i++ {
		order := append([]string(nil), labels...)
		rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		probabilities := make(map[string]float64, len(order))
		for _, label := range order {
			probabilities[label] = values[label]
		}
		got, err := json.Marshal(Consensus(questions, map[string]*providers.Decision{
			"a": dec(map[string]providers.Answer{"q": {Type: providers.QuestionChoice, Probabilities: probabilities}}),
		}, 1))
		if err != nil {
			t.Fatalf("iteration %d marshal: %v", i, err)
		}
		if i == 0 {
			baseline = got
			continue
		}
		if string(got) != string(baseline) {
			t.Fatalf("iteration %d output differs\nfirst: %s\n got: %s", i, baseline, got)
		}
	}
}
