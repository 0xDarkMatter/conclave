// Pareto maths for price-performance frontiers (ADR-018). Pure: no I/O, no
// source packages — only Points. The invariants:
//   - only points with BOTH a finite Score and a finite, non-negative Cost
//     participate in domination (invalid or absent observations are never
//     estimated onto the axis, and never dominate anything);
//   - domination is computed within Kind because chat and decision scores are
//     explicitly incomparable;
//   - an exact tie (equal score AND equal cost) dominates nothing, so both
//     members stay on the frontier;
//   - the returned order is total and deterministic: frontier points by
//     ascending cost, dominated points by descending score, then everything
//     unscored or uncosted by name.
package frontier

import (
	"math"
	"sort"
)

// Pareto returns the same points with OnFrontier set, computing a separate
// frontier per Kind so incomparable chat and decision scores never dominate
// each other. It returns a fresh slice in the deterministic order above: the
// caller's slice and its order are left untouched.
func Pareto(points []Point) []Point {
	out := make([]Point, len(points))
	copy(out, points)
	for i := range out {
		out[i].OnFrontier = onFrontier(out[i], out)
	}
	sort.Slice(out, func(i, j int) bool { return pointLess(out[i], out[j]) })
	return out
}

// onFrontier reports whether p is dominated by any other scored+costed point.
// Domination: q.Score >= p.Score AND q.Cost <= p.Cost with at least one strict
// — "better or equal everywhere, better somewhere".
func onFrontier(p Point, all []Point) bool {
	if !availableForPareto(p) {
		return false
	}
	for _, q := range all {
		if q.Kind != p.Kind || !availableForPareto(q) {
			continue
		}
		if *q.Score >= *p.Score && *q.Cost <= *p.Cost &&
			(*q.Score > *p.Score || *q.Cost < *p.Cost) {
			return false
		}
	}
	return true
}

// availableForPareto is the single validity boundary for both domination and
// bucketing. Zero cost is valid; negative and non-finite source values mean
// unavailable rather than "free" or "infinitely good" (ADR-018).
func availableForPareto(p Point) bool {
	return p.Score != nil && !math.IsNaN(*p.Score) && !math.IsInf(*p.Score, 0) &&
		p.Cost != nil && !math.IsNaN(*p.Cost) && !math.IsInf(*p.Cost, 0) && *p.Cost >= 0
}

// pointLess is the total order: bucket (frontier < dominated < rest), then the
// exact public ordering keys, then every remaining observable field. The last
// step prevents stable-sort input order from becoming part of the wire JSON.
func pointLess(a, b Point) bool {
	ra, rb := orderRank(a), orderRank(b)
	if ra != rb {
		return ra < rb
	}
	switch ra {
	case 0: // frontier: (cost, -score, ID, name)
		if *a.Cost != *b.Cost {
			return *a.Cost < *b.Cost
		}
		if *a.Score != *b.Score {
			return *a.Score > *b.Score
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
	case 1: // dominated: (-score, cost, ID, name)
		if *a.Score != *b.Score {
			return *a.Score > *b.Score
		}
		if *a.Cost != *b.Cost {
			return *a.Cost < *b.Cost
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
	default: // rest: (name, ID)
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
	}
	return remainingFieldsLess(a, b)
}

// remainingFieldsLess closes ties across the documented public keys without
// comparing pointer addresses. Float bits distinguish observable -0 and NaN
// payloads, while exact duplicates remain byte-identical in either order.
func remainingFieldsLess(a, b Point) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if c := optionalFloatCompare(a.Score, b.Score); c != 0 {
		return c < 0
	}
	if c := optionalFloatCompare(a.Cost, b.Cost); c != 0 {
		return c < 0
	}
	if a.CostBasis != b.CostBasis {
		return a.CostBasis < b.CostBasis
	}
	if c := optionalFloatCompare(a.LatencyMs, b.LatencyMs); c != 0 {
		return c < 0
	}
	if c := optionalFloatCompare(a.ECE, b.ECE); c != 0 {
		return c < 0
	}
	if a.SelfReported != b.SelfReported {
		return !a.SelfReported
	}
	if a.ScoreSource != b.ScoreSource {
		return a.ScoreSource < b.ScoreSource
	}
	return false
}

func optionalFloatCompare(a, b *float64) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	ab, bb := math.Float64bits(*a), math.Float64bits(*b)
	if ab < bb {
		return -1
	}
	if ab > bb {
		return 1
	}
	return 0
}

// orderRank buckets a point for the deterministic order. Rank 2 (not 1) for
// a scored-but-uncosted point: it is not dominated in the Pareto sense
// (domination compares only scored+costed pairs), so it belongs with the
// unscored remainder, ordered by name.
func orderRank(p Point) int {
	switch {
	case p.OnFrontier:
		return 0
	case availableForPareto(p):
		return 1 // scored, costed, dominated
	default:
		return 2 // unscored or uncosted
	}
}
