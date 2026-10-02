// Tests for Consensus, each named for the aggregation bug it prevents
// (ADR-016's averaging rules). Every test here was watched failing against
// the stub Consensus before the implementation landed. Fixtures use exact
// binary fractions (0.5, 0.25, 0.75) wherever two values must tie or compare
// equal, so float wobble cannot flip a test; everywhere else comparisons
// carry a 1e-9 tolerance.

package decide

import (
	"math"
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

	if qc := mustGet(t, got, "triage"); qc.Succeeded != 2 {
		t.Fatalf("triage succeeded = %d, want 2 (a and c; b's noul-typed answer is skipped)", qc.Succeeded)
	}
	if qc := mustGet(t, got, "urgency"); qc.Succeeded != 1 {
		t.Fatalf("urgency succeeded = %d, want 1 (a only)", qc.Succeeded)
	}
	qc := mustGet(t, got, "routing")
	if qc.Succeeded != 0 {
		t.Fatalf("routing succeeded = %d, want 0 (nobody answered)", qc.Succeeded)
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
