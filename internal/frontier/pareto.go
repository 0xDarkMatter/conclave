// Pareto maths for price-performance frontiers (ADR-018). Pure: no I/O, no
// source packages — only Points. The invariants:
//   - only points with BOTH a Score and a Cost participate in domination
//     (an unscored model is never estimated onto the axis, and never
//     dominates anything);
//   - an exact tie (equal score AND equal cost) dominates nothing, so both
//     members stay on the frontier;
//   - the returned order is total and deterministic: frontier points by
//     ascending cost, dominated points by descending score, then everything
//     unscored or uncosted by name.
package frontier

import "sort"

// Pareto returns the same points with OnFrontier set, in the deterministic
// order above. It returns a fresh slice: the caller's slice and its order are
// left untouched, so Results can be built in source order and re-ordered here
// at render time.
func Pareto(points []Point) []Point {
	out := make([]Point, len(points))
	copy(out, points)
	for i := range out {
		out[i].OnFrontier = onFrontier(out[i], out)
	}
	// SliceStable, not Slice: the comparator ties only on (name, ID), and
	// duplicate names are plausible in hand-mapped data — stability keeps
	// repeats in source order instead of shuffle-sorted.
	sort.SliceStable(out, func(i, j int) bool { return pointLess(out[i], out[j]) })
	return out
}

// onFrontier reports whether p is dominated by any other scored+costed point.
// Domination: q.Score >= p.Score AND q.Cost <= p.Cost with at least one strict
// — "better or equal everywhere, better somewhere".
func onFrontier(p Point, all []Point) bool {
	if p.Score == nil || p.Cost == nil {
		return false
	}
	for _, q := range all {
		if q.Score == nil || q.Cost == nil {
			continue
		}
		if *q.Score >= *p.Score && *q.Cost <= *p.Cost &&
			(*q.Score > *p.Score || *q.Cost < *p.Cost) {
			return false
		}
	}
	return true
}

// pointLess is the total order: bucket (frontier < dominated < rest), then the
// bucket's own key, then name and ID as the final tiebreak.
func pointLess(a, b Point) bool {
	ra, rb := orderRank(a), orderRank(b)
	if ra != rb {
		return ra < rb
	}
	switch ra {
	case 0: // frontier: ascending cost. Equal cost among frontier points can
		// only be an exact tie (anything dearer at that score is dominated),
		// so name/ID settles it.
		if *a.Cost != *b.Cost {
			return *a.Cost < *b.Cost
		}
	case 1: // dominated: descending score — the interesting models first.
		if *a.Score != *b.Score {
			return *a.Score > *b.Score
		}
	default: // rest: unscored or uncosted, keyed by name alone.
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.ID < b.ID
}

// orderRank buckets a point for the deterministic order. Rank 2 (not 1) for
// a scored-but-uncosted point: it is not dominated in the Pareto sense
// (domination compares only scored+costed pairs), so it belongs with the
// unscored remainder, ordered by name.
func orderRank(p Point) int {
	switch {
	case p.OnFrontier:
		return 0
	case p.Score != nil && p.Cost != nil:
		return 1 // scored, costed, dominated
	default:
		return 2 // unscored or uncosted
	}
}
