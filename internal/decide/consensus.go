// Consensus aggregates a decision panel's typed answers per question
// (ADR-016: equal-weight probability averaging — never an LLM judge, never
// confidence-weighted, because each vendor's calibration is its own claim).
//
// Invariants: pure and deterministic (no I/O; deciders accumulate in name
// order because float addition is not associative; ties break by sorted key);
// the provider contract in internal/providers/decider.go is imported, never
// redefined; a decider is skipped per question when its answer is missing or
// type-mismatched, and Succeeded counts only matches; Agreement = 1 - JSD
// (equal weights, log base 2), nil under two usable distributions.
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
	Agreement     *float64           `json:"agreement,omitempty"`     // 1 - JSD (base 2) across deciders; nil when <2 succeeded
	Contested     bool               `json:"contested"`
	Succeeded     int                `json:"succeeded"`
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
		matched := matchedAnswers(qid, q.Type, decisions)
		qc.Succeeded = len(matched)

		var dists [][]float64
		switch q.Type {
		case providers.QuestionChoice:
			dists = aggregateChoice(&qc, matched)
		case providers.QuestionScore:
			dists = aggregateScore(&qc, q, matched)
		case providers.QuestionNoul:
			dists = aggregateNoul(&qc, matched)
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

// matchedAnswers collects the answers that count for qid, sorted by decider
// name. Sorting is not cosmetic: float addition is not associative, so an
// unsorted accumulation could wobble the averages in the last ulp between
// runs and break ADR-016's "reproducible" promise. A nil *Decision is
// skipped defensively; callers should not produce one.
func matchedAnswers(qid, qType string, decisions map[string]*providers.Decision) []namedAnswer {
	var matched []namedAnswer
	for name, d := range decisions {
		if d == nil {
			continue
		}
		a, ok := d.Answers[qid]
		if !ok || a.Type != qType {
			// Missing or mismatched type: skipped for this question only —
			// the decider's other answers still count there.
			continue
		}
		matched = append(matched, namedAnswer{name: name, answer: a})
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].name < matched[j].name })
	return matched
}

// === SECTION: per-type aggregation ======================================
// One aggregator per question type; each fills qc and returns the per-decider
// distributions that the agreement section consumes.

// aggregateChoice fills the choice fields of qc and returns the per-decider
// distributions (over the sorted union label set) for Agreement.
func aggregateChoice(qc *QuestionConsensus, matched []namedAnswer) [][]float64 {
	// Label universe is the union of what deciders emitted, not q.Choices: a
	// label nobody emitted carries no mass, and seeding it would only pad
	// the output with 0.0 entries no decider ever spoke to.
	labelSet := map[string]struct{}{}
	for _, m := range matched {
		for l := range m.answer.Probabilities {
			labelSet[l] = struct{}{}
		}
	}
	labels := make([]string, 0, len(labelSet))
	for l := range labelSet {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	// ADR-016 average: each label's raw probability summed across deciders
	// (a label absent from one decider's map counts 0 for it), then the
	// average scaled to sum 1.
	sums := make(map[string]float64, len(labels))
	for _, m := range matched {
		for l, p := range m.answer.Probabilities {
			sums[l] += p
		}
	}
	if avg, ok := scaleToUnit(sums); ok {
		qc.Probabilities = avg
		if c, ok := argmaxLabel(avg, labels); ok {
			qc.Choice = c
		}
	}

	var dists [][]float64
	picks := map[string]struct{}{}
	votes := map[string]int{}
	for _, m := range matched {
		a := m.answer
		d, hasMass := scaleToUnit(a.Probabilities)
		if hasMass {
			// vector over the union label order, for the JSD computation
			v := make([]float64, len(labels))
			for i, l := range labels {
				v[i] = d[l]
			}
			dists = append(dists, v)
		}
		// A decider's own pick: its declared Choice when it made one, else
		// the argmax of its own distribution.
		pick := a.Choice
		if pick == "" && hasMass {
			if c, ok := argmaxLabel(d, labels); ok {
				pick = c
			}
		}
		if pick == "" {
			continue // no declared choice and no mass: no vote, no contested say
		}
		votes[pick]++
		picks[pick] = struct{}{}
	}
	if len(votes) > 0 {
		qc.Votes = votes
	}
	qc.Contested = len(picks) > 1
	return dists
}

// aggregateScore fills the score fields of qc and returns the per-decider
// distributions over indices 0..len(q.Scale)-1 for Agreement. Index keys are
// the wire's decimal strings ("0".."n-1"); an index outside the scale is
// dropped — it is not a score this question asked for, and keeping it would
// silently move mass off the reported distribution.
func aggregateScore(qc *QuestionConsensus, q providers.Question, matched []namedAnswer) [][]float64 {
	n := len(q.Scale) // index = score value, per the Question contract
	sums := make([]float64, n)
	var dists [][]float64
	var picks []int
	votes := map[string]int{}
	for _, m := range matched {
		// p <= 0 is skipped at parse: zero adds nothing, and a negative
		// probability would make log2 undefined inside the JSD entropies.
		raw := make([]float64, n)
		for k, p := range m.answer.Probabilities {
			i, err := strconv.Atoi(k)
			if err != nil || i < 0 || i >= n || p <= 0 {
				continue
			}
			raw[i] = p
		}
		for i, p := range raw {
			sums[i] += p
		}
		if total := vecSum(raw); total > 0 {
			// per-decider normalisation for JSD; the average above uses raw
			// values, matching choice's averaging of declared probabilities.
			v := make([]float64, n)
			for i, p := range raw {
				v[i] = p / total
			}
			dists = append(dists, v)
			// A decider's own pick is the argmax of its own distribution —
			// Answer.Score is ignored so votes, contested steps and the
			// aggregate all read the same index scale.
			idx := argmaxVec(v)
			picks = append(picks, idx)
			votes[strconv.Itoa(idx)]++
		}
	}
	if avg, ok := scaleVecToUnit(sums); ok {
		// all n indices are emitted, zero-valued ones included: the averaged
		// distribution is over the whole scale by construction
		prob := make(map[string]float64, n)
		for i, p := range avg {
			prob[strconv.Itoa(i)] = p
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
func aggregateNoul(qc *QuestionConsensus, matched []namedAnswer) [][]float64 {
	var dists [][]float64
	var sum float64
	count := 0
	someYes, someNo := false, false
	for _, m := range matched {
		a := m.answer
		if a.Noul == nil {
			// Type matched but the vendor omitted the value: still
			// Succeeded (the skip rule is type-based only) — it just adds
			// no mass to the mean, the boundary check or the agreement.
			continue
		}
		v := *a.Noul
		sum += v
		count++
		if v >= 0.5 {
			someYes = true // 0.5 itself counts as yes — the pinned boundary
		} else {
			someNo = true
		}
		// Clamp into [0,1] for the Bernoulli only: an out-of-range vendor
		// value would make P(no) negative and log2 undefined; the mean above
		// still reports the raw value honestly.
		p := math.Min(math.Max(v, 0), 1)
		dists = append(dists, []float64{1 - p, p})
	}
	if count > 0 {
		mean := sum / float64(count)
		qc.Noul = &mean
	}
	qc.Contested = someYes && someNo
	return dists
}

// === SECTION: agreement math ===========================================

// agreement returns 1 - JSD(dists), where JSD is the generalised
// Jensen-Shannon divergence with equal weights and log base 2:
//
//	JSD(P_1..P_n) = H((1/n) * Σ P_i) - (1/n) * Σ H(P_i)
//
// (J. Lin, "Divergence measures based on the Shannon entropy",
// IEEE Trans. Inf. Theory 37(1), 1991 — the equal-weight, base-2 form whose
// two-distribution value is bounded to [0,1].) With n > 2 fully disjoint
// distributions the value can reach log2(n), so the result is clamped:
// Agreement 0 then simply reads "no overlap at all". nil when fewer than
// two distributions — divergence needs two things to diverge.
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
	a := 1 - jsd
	if a < 0 {
		a = 0
	} else if a > 1 {
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

// scaleToUnit returns a copy of m scaled to sum 1, ok=false when m carries
// no positive mass (an all-zero average has no argmax to report). Keys are
// preserved at 0.0 — a label a decider emitted at zero still shows in the
// averaged output, like "sales": 0.0 in the plan's --json example. Negative
// values are clamped to 0: log2 of a negative probability is NaN inside the
// JSD entropies, and no vendor legitimately emits one.
func scaleToUnit(m map[string]float64) (map[string]float64, bool) {
	total := 0.0
	for _, v := range m {
		if v > 0 {
			total += v
		}
	}
	if total <= 0 {
		return nil, false
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		if v < 0 {
			v = 0
		}
		out[k] = v / total
	}
	return out, true
}

// argmaxLabel returns the key with the highest value; keys must be pre-sorted
// so ties resolve to the first in sort order — deterministic across runs and
// immune to Go's randomised map iteration. ok=false when no key of keys is
// present in m.
func argmaxLabel(m map[string]float64, keys []string) (string, bool) {
	best := ""
	bestP := 0.0
	found := false
	for _, k := range keys {
		p, ok := m[k]
		if !ok {
			continue
		}
		if !found || p > bestP {
			best, bestP, found = k, p, true
		}
	}
	return best, found
}

// scaleVecToUnit is scaleToUnit for score index vectors (negatives cannot
// occur here: they are dropped when the wire map is read).
func scaleVecToUnit(v []float64) ([]float64, bool) {
	total := vecSum(v)
	if total <= 0 {
		return nil, false
	}
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x / total
	}
	return out, true
}

// vecSum adds the positive components of v.
func vecSum(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		if x > 0 {
			s += x
		}
	}
	return s
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
