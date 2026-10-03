// Tests for the frontier maths half (ADR-018): domination, tie handling,
// the unscored exclusion, and the deterministic output order. All values are
// hand-built Points — the maths must not depend on any source package.
package frontier

import (
	"reflect"
	"testing"
)

// f64p is the pointer-taking helper every nil-able numeric field needs.
func f64p(v float64) *float64 { return &v }

// TestParetoDominatedPointIsNotOnFrontier pins the domination rule: a point is
// off the frontier when another scored+costed point is at least as good on
// both axes and strictly better on one — including the equal-score-dearer
// case, which is the easy one to get wrong.
func TestParetoDominatedPointIsNotOnFrontier(t *testing.T) {
	pts := []Point{
		{ID: "worse-both", Name: "Worse Both", Score: f64p(50), Cost: f64p(2)},
		{ID: "equal-score-dearer", Name: "Equal Score Dearer", Score: f64p(60), Cost: f64p(2)},
		{ID: "best", Name: "Best", Score: f64p(60), Cost: f64p(1)},
	}
	got := Pareto(pts)
	byID := map[string]bool{}
	for _, p := range got {
		byID[p.ID] = p.OnFrontier
	}
	if !byID["best"] {
		t.Fatal("the strictly dominating point must be on the frontier")
	}
	if byID["worse-both"] {
		t.Error("a point beaten on both axes is on the frontier")
	}
	if byID["equal-score-dearer"] {
		t.Error("an equal-scored, dearer point is on the frontier; equal score + cheaper cost dominates it")
	}
}

// TestParetoTiesBothStay pins the tie rule: identical score AND identical cost
// dominate nothing, so both duplicates stay on the frontier.
func TestParetoTiesBothStay(t *testing.T) {
	pts := []Point{
		{ID: "tie-a", Name: "Tie A", Score: f64p(50), Cost: f64p(1)},
		{ID: "tie-b", Name: "Tie B", Score: f64p(50), Cost: f64p(1)},
	}
	got := Pareto(pts)
	if len(got) != 2 {
		t.Fatalf("Pareto returned %d points; want both tie members kept", len(got))
	}
	for _, p := range got {
		if !p.OnFrontier {
			t.Errorf("tie member %s dropped off the frontier; exact ties must both stay", p.ID)
		}
	}
}

// TestParetoUnscoredNeverOnFrontier pins ADR-018's unscored rule: a model
// without an external score (or without a cost) is never plotted on the
// quality axis, and an unscored point must not dominate anything either.
func TestParetoUnscoredNeverOnFrontier(t *testing.T) {
	pts := []Point{
		{ID: "no-score", Name: "No Score", Cost: f64p(1)},
		{ID: "no-cost", Name: "No Cost", Score: f64p(99)},
		{ID: "neither", Name: "Neither"},
		// The only scored+costed point; a cheaper unscored point must not
		// evict it.
		{ID: "modest", Name: "Modest", Score: f64p(10), Cost: f64p(5)},
		{ID: "cheap-mute", Name: "Cheap Mute", Cost: f64p(0.1)},
	}
	got := Pareto(pts)
	byID := map[string]bool{}
	for _, p := range got {
		byID[p.ID] = p.OnFrontier
	}
	if !byID["modest"] {
		t.Error("the only scored+costed point is off the frontier; an unscored point must never dominate it")
	}
	for _, id := range []string{"no-score", "no-cost", "neither", "cheap-mute"} {
		if byID[id] {
			t.Errorf("%s is on the frontier; unscored or uncosted points never are", id)
		}
	}
}

// TestParetoOrderIsDeterministic pins the full output order: frontier points
// by ascending cost, then dominated points by descending score (name breaks
// score ties), then unscored/uncosted points by name.
func TestParetoOrderIsDeterministic(t *testing.T) {
	pts := []Point{
		{ID: "u-zed", Name: "Zed", Cost: f64p(5)},
		{ID: "dom-b", Name: "Beta", Score: f64p(40), Cost: f64p(2)},
		{ID: "dear-smart", Name: "Dear Smart", Score: f64p(80), Cost: f64p(2)},
		{ID: "u-amy", Name: "Amy", Score: f64p(30)},
		{ID: "cheap-modest", Name: "Cheap Modest", Score: f64p(50), Cost: f64p(1)},
		{ID: "mid", Name: "Mid", Score: f64p(70), Cost: f64p(3)},
		{ID: "dom-a", Name: "Alpha", Score: f64p(40), Cost: f64p(5)},
		{ID: "u-nodata", Name: "NoData"},
	}
	want := []string{
		"cheap-modest", "dear-smart", // frontier, ascending cost
		"mid",            // dominated, descending score: 70
		"dom-a", "dom-b", // score tie 40/40 broken by name: Alpha < Beta
		"u-amy", "u-nodata", "u-zed", // unscored, by name
	}
	got := Pareto(pts)
	gotIDs := make([]string, len(got))
	for i, p := range got {
		gotIDs[i] = p.ID
	}
	if !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("order = %v; want %v", gotIDs, want)
	}
	if !got[0].OnFrontier || !got[1].OnFrontier {
		t.Error("the first two points are the frontier and must carry OnFrontier")
	}
	for _, p := range got[2:] {
		if p.OnFrontier {
			t.Errorf("%s is dominated or unscored but marked OnFrontier", p.ID)
		}
	}
}
