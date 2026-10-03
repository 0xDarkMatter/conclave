// Consensus aggregates a decision panel's typed answers per question
// (ADR-016: equal-weight probability averaging — never an LLM judge, never
// confidence-weighted, because each vendor's calibration is its own claim).
//
// Invariants: pure and deterministic (no I/O; deciders accumulate in name
// order because float addition is not associative; ties break by sorted key);
// the provider contract in internal/providers/decider.go is imported, never
// redefined; a decider is skipped per question when its answer is missing or
// malformed, and Succeeded counts only validated payloads; Agreement uses
// Lin's equal-weight generalised JSD normalised by log2(panel size), nil under
// two usable distributions.
//
// Reasoning: docs/adr/ADR-016-decision-models-are-a-separate-provider-class.md
// and docs/PLAN-decision-models.md ("Consensus"). The package doc is NOT here:
// a sibling lane owns doc.go; the blank line below keeps this a file contract.

package decide

import (
	"math"
	"sort"
	"strconv"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// QuestionConsensus is the aggregated answer for one question id: the
// "consensus" block of `conclave decide --json` (docs/PLAN-decision-models.md).
// Only the fields matching Type are meaningful; pointers separate "the
// aggregate says 0" from "nobody answered".
type QuestionConsensus struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`        // choice: argmax of averaged probabilities
	Score         *int               `json:"score,omitempty"`         // score: argmax index of averaged distribution
	Expected      *float64           `json:"expected,omitempty"`      // score: probability-weighted mean index
	Noul          *float64           `json:"noul,omitempty"`          // noul: mean value
	Probabilities map[string]float64 `json:"probabilities,omitempty"` // averaged distribution (choice/score)
	Votes         map[string]int     `json:"votes,omitempty"`         // each decider's own argmax, counted
	Agreement     *float64           `json:"agreement,omitempty"`     // 1 - JSD/log2(n) across deciders; nil when <2 succeeded
	Contested     bool               `json:"contested"`
	Succeeded     int                `json:"succeeded"`
	Invalid       int                `json:"invalid,omitempty"` // present answers rejected as malformed
	Requested     int                `json:"requested"`
}

// === SECTION: entry point ===============================================
// Consensus + the per-question matching that feeds the three aggregators.

// Consensus aggregates the successful decisions (keyed by decider name) over
// the question set. A question no decider answered still appears, with
// Succeeded 0; answers to ids outside the question set are ignored, because
// the question set defines the report. requested is echoed per question so a
// partial panel is visible next to the per-question success count.
func Consensus(questions map[string]providers.Question, decisions map[string]*providers.Decision, requested int) map[string]QuestionConsensus {
	out := make(map[string]QuestionConsensus, len(questions))
	for qid, q := range questions {
		qc := QuestionConsensus{Type: q.Type, Requested: requested}
		answers := answersForQuestion(qid, decisions)

		var dists [][]float64
		switch q.Type {
		case providers.QuestionChoice:
			dists = aggregateChoice(&qc, q, answers)
		case providers.QuestionScore:
			dists = aggregateScore(&qc, q, answers)
		case providers.QuestionNoul:
			dists = aggregateNoul(&qc, answers)
		default:
			// Question validation rejects unknown types before any spend;
			// degrading to a counts-only entry keeps this pure function total.
		}
		if a := agreement(dists); a != nil {
			qc.Agreement = a
		}
		out[qid] = qc
	}
	return out
}

// namedAnswer pairs an answer with the decider that gave it.
type namedAnswer struct {
	name   string
	answer providers.Answer
}

// answersForQuestion collects present answers for qid, sorted by decider name.
// Validation deliberately happens later: Invalid distinguishes malformed
// vendor payloads from missing answers and nil decisions. Sorting is not
// cosmetic because float addition is not associative and ADR-016 promises a
// reproducible arithmetic consensus.
func answersForQuestion(qid string, decisions map[string]*providers.Decision) []namedAnswer {
	var answers []namedAnswer
	for name, d := range decisions {
		if d == nil {
			continue
		}
		a, ok := d.Answers[qid]
		if !ok {
			continue
		}
		answers = append(answers, namedAnswer{name: name, answer: a})
	}
	sort.Slice(answers, func(i, j int) bool { return answers[i].name < answers[j].name })
	return answers
}

// === SECTION: per-type aggregation ======================================
// One aggregator per question type; each fills qc and returns the per-decider
// distributions that the agreement section consumes.

// aggregateChoice fills the choice fields of qc and returns the validated,
// normalized per-decider distributions for Agreement. The question criteria
// are the wire domain; accepting a vendor-created label would let it inject an
// answer the caller never offered.
func aggregateChoice(qc *QuestionConsensus, q providers.Question, answers []namedAnswer) [][]float64 {
	labels := make([]string, 0, len(q.Choices))
	for l := range q.Choices {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	sums := make([]float64, len(labels))
	dists := make([][]float64, 0, len(answers))
	picks := map[string]struct{}{}
	votes := map[string]int{}
	for _, named := range answers {
		a := named.answer
		if a.Type != providers.QuestionChoice || (a.Choice != "" && !hasChoice(q.Choices, a.Choice)) {
			qc.Invalid++
			continue
		}
		d, ok := normalizeChoiceDistribution(a.Probabilities, q.Choices, labels)
		if !ok {
			qc.Invalid++
			continue
		}
		qc.Succeeded++
		dists = append(dists, d)
		for i, p := range d {
			sums[i] += p
		}
		// Vendors may report a thresholded Choice that disagrees with their
		// probability argmax. It remains valid, but voting follows normalized
		// evidence so Votes, Contested and the aggregate share one basis.
		pick := labels[argmaxVec(d)]
		votes[pick]++
		picks[pick] = struct{}{}
	}
	if qc.Succeeded > 0 {
		avg := make(map[string]float64, len(labels))
		for i, label := range labels {
			avg[label] = sums[i] / float64(qc.Succeeded)
		}
		qc.Probabilities = avg
		qc.Choice = labels[argmaxVec(sums)]
	}
	if len(votes) > 0 {
		qc.Votes = votes
	}
	qc.Contested = len(picks) > 1
	return dists
}

// aggregateScore fills the score fields of qc and returns the per-decider
// distributions over indices 0..len(q.Scale)-1 for Agreement. The wire key
// grammar is exactly strconv.Itoa(index); accepting aliases such as "01"
// makes map iteration decide which value overwrites index 1.
func aggregateScore(qc *QuestionConsensus, q providers.Question, answers []namedAnswer) [][]float64 {
	n := len(q.Scale) // index = score value, per the Question contract
	sums := make([]float64, n)
	dists := make([][]float64, 0, len(answers))
	var picks []int
	votes := map[string]int{}
	for _, named := range answers {
		a := named.answer
		if a.Type != providers.QuestionScore {
			qc.Invalid++
			continue
		}
		d, ok := normalizeScoreDistribution(a.Probabilities, n)
		if !ok {
			qc.Invalid++
			continue
		}
		qc.Succeeded++
		for i, p := range d {
			sums[i] += p
		}
		dists = append(dists, d)
		// Score, like Choice, may be vendor-thresholded; its normalized
		// distribution is the sole voting and aggregation authority.
		idx := argmaxVec(d)
		picks = append(picks, idx)
		votes[strconv.Itoa(idx)]++
	}
	if qc.Succeeded > 0 {
		// all n indices are emitted, zero-valued ones included: the averaged
		// distribution is over the whole scale by construction
		prob := make(map[string]float64, n)
		avg := make([]float64, n)
		for i, p := range sums {
			avg[i] = p / float64(qc.Succeeded)
			prob[strconv.Itoa(i)] = avg[i]
		}
		qc.Probabilities = prob
		idx := argmaxVec(avg)
		qc.Score = &idx
		expected := 0.0
		for i, p := range avg {
			expected += float64(i) * p
		}
		qc.Expected = &expected
	}
	if len(votes) > 0 {
		qc.Votes = votes
	}
	if len(picks) > 0 {
		lo, hi := picks[0], picks[0]
		for _, p := range picks {
			if p < lo {
				lo = p
			}
			if p > hi {
				hi = p
			}
		}
		// "differ by >= 1 step": with integer picks that is the plain spread.
		qc.Contested = hi-lo >= 1
	}
	return dists
}

// aggregateNoul fills the noul fields of qc and returns the per-decider
// Bernoulli distributions, ordered [P(no), P(yes)], for Agreement.
func aggregateNoul(qc *QuestionConsensus, answers []namedAnswer) [][]float64 {
	dists := make([][]float64, 0, len(answers))
	var sum float64
	someYes, someNo := false, false
	for _, named := range answers {
		a := named.answer
		if a.Type != providers.QuestionNoul || a.Noul == nil || !finite(*a.Noul) || *a.Noul < 0 || *a.Noul > 1 {
			qc.Invalid++
			continue
		}
		v := *a.Noul
		sum += v
		qc.Succeeded++
		if v >= 0.5 {
			someYes = true // 0.5 itself counts as yes — the pinned boundary
		} else {
			someNo = true
		}
		dists = append(dists, []float64{1 - v, v})
	}
	if qc.Succeeded > 0 {
		mean := sum / float64(qc.Succeeded)
		qc.Noul = &mean
	}
	qc.Contested = someYes && someNo
	return dists
}

// === SECTION: agreement math ===========================================

// agreement returns 1 - JSD(dists)/log2(n), where JSD is Lin's generalised
// Jensen-Shannon divergence with equal weights and log base 2:
//
//	JSD(P_1..P_n) = H((1/n) * Σ P_i) - (1/n) * Σ H(P_i)
//
// (J. Lin, "Divergence measures based on the Shannon entropy",
// IEEE Trans. Inf. Theory 37(1), 1991.) Generalised JSD reaches log2(n) for n
// disjoint distributions, so dividing by that bound preserves partial-overlap
// resolution while keeping Agreement in [0,1]. Only last-ulp noise is clamped;
// nil means fewer than two distributions can diverge.
func agreement(dists [][]float64) *float64 {
	if len(dists) < 2 {
		return nil
	}
	n := float64(len(dists))
	m := make([]float64, len(dists[0]))
	for _, d := range dists {
		for i, p := range d {
			m[i] += p / n
		}
	}
	jsd := entropyBase2(m)
	for _, d := range dists {
		jsd -= entropyBase2(d) / n
	}
	a := 1 - jsd/math.Log2(n)
	const epsilon = 1e-12
	if a < 0 && a > -epsilon {
		a = 0
	} else if a > 1 && a < 1+epsilon {
		a = 1
	}
	return &a
}

// entropyBase2 is Shannon entropy in bits; non-positive components are
// skipped (0 log 0 = 0 by continuity).
func entropyBase2(p []float64) float64 {
	h := 0.0
	for _, v := range p {
		if v > 0 {
			h -= v * math.Log2(v)
		}
	}
	return h
}

// === SECTION: distribution helpers =====================================
// Shared small utilities over label maps and index vectors. All tie-breaks
// live here, in one place: sorted keys for labels, numeric order for indices.

// normalizeChoiceDistribution validates the vendor map against the criteria
// before normalizing it. Values are summed in sorted criteria order; map
// iteration is used only for order-independent rejection of unknown keys.
func normalizeChoiceDistribution(probabilities map[string]float64, choices map[string]string, labels []string) ([]float64, bool) {
	for label, p := range probabilities {
		if !hasChoice(choices, label) || !finite(p) || p < 0 {
			return nil, false
		}
	}
	raw := make([]float64, len(labels))
	for i, label := range labels {
		raw[i] = probabilities[label]
	}
	return normalizeVector(raw)
}

// normalizeScoreDistribution enforces the score wire grammar where each key
// is the canonical decimal spelling of an in-range index. Rejecting the whole
// map prevents invalid mass from being silently discarded and amplified.
func normalizeScoreDistribution(probabilities map[string]float64, n int) ([]float64, bool) {
	raw := make([]float64, n)
	for key, p := range probabilities {
		i, err := strconv.Atoi(key)
		if err != nil || i < 0 || i >= n || key != strconv.Itoa(i) || !finite(p) || p < 0 {
			return nil, false
		}
		raw[i] = p
	}
	return normalizeVector(raw)
}

// normalizeVector returns a fresh unit vector. Its index-order sum is the
// deterministic path shared by choice output, score output and JSD input.
func normalizeVector(v []float64) ([]float64, bool) {
	total := vecSum(v)
	if !finite(total) || total <= 0 {
		return nil, false
	}
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x / total
	}
	return out, true
}

// vecSum adds components in index order; callers reject negatives first.
func vecSum(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s
}

func hasChoice(choices map[string]string, label string) bool {
	_, ok := choices[label]
	return ok
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// argmaxVec returns the index of the highest value. The ascending scan with a
// strict > keeps the SMALLEST index on ties — the same rule as choice's
// sorted-key break, and numeric where a string sort of the wire keys would
// put "10" before "9".
func argmaxVec(v []float64) int {
	best := 0
	for i := 1; i < len(v); i++ {
		if v[i] > v[best] {
			best = i
		}
	}
	return best
}
