// Tests for the assembly half (ADR-018): turning a benchmarks Feed and a
// decision Board into Results. The source packages are stubs on this base, so
// every input is hand-built from their EXPORTED types only — no Load call, no
// network, no committed upstream data (the Decision Index Space carries no
// licence; the handful of numbers below is typed in with citations, as
// ADR-018 requires).
package frontier

import (
	"math"
	"strings"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/benchmarks/decisionindex"
	"github.com/0xDarkMatter/conclave-cli/internal/benchmarks/openrouter"
	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
)

// almostEqual keeps float assertions honest: computed costs (blends, means)
// must not be compared with ==.
func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// byID finds one point; nil (not a t.Fatal) lets the caller report which id
// was missing from which result.
func byID(t *testing.T, pts []Point, id string) *Point {
	t.Helper()
	for i := range pts {
		if pts[i].ID == id {
			return &pts[i]
		}
	}
	return nil
}

func mustPoint(t *testing.T, pts []Point, id string) *Point {
	t.Helper()
	p := byID(t, pts, id)
	if p == nil {
		t.Fatalf("no point with id %q in %d points", id, len(pts))
	}
	return p
}

// === CHAT RESULT ASSEMBLY ===

// TestBuildChatBlendCost pins the blend basis: (3*input + output)/4 from the
// row's own pricing, nil when either half is missing (a half-blend would be an
// invented price), plus the axis pick and the AA provenance.
func TestBuildChatBlendCost(t *testing.T) {
	feed := &openrouter.Feed{
		AA: []openrouter.AAScore{
			{Slug: "a/one", Name: "One", Intelligence: f64p(70), InputPerM: f64p(2), OutputPerM: f64p(6)},
			{Slug: "a/null-score", Name: "Null Score", InputPerM: f64p(1), OutputPerM: f64p(1)},
			{Slug: "a/no-pricing", Name: "No Pricing", Intelligence: f64p(60)},
			{Slug: "a/half-pricing", Name: "Half Pricing", Intelligence: f64p(65), InputPerM: f64p(2)},
		},
		AAAsOf:   "2026-10-01",
		Citation: "Artificial Analysis, https://artificialanalysis.ai",
	}
	res := BuildChat(feed, AxisIntelligence, CostBlendPerM)

	if res.Kind != KindChat || res.Axis != AxisIntelligence || res.CostBasis != CostBlendPerM {
		t.Fatalf("result header = %s/%s/%s; want chat/intelligence/blend_per_m", res.Kind, res.Axis, res.CostBasis)
	}
	if len(res.Points) != len(feed.AA) {
		t.Fatalf("%d points; want one per AA row (%d)", len(res.Points), len(feed.AA))
	}

	one := mustPoint(t, res.Points, "a/one")
	if one.Name != "One" || one.Kind != KindChat {
		t.Errorf("point identity = %+v; want the AA row's name and kind chat", one)
	}
	if one.Score == nil || !almostEqual(*one.Score, 70) {
		t.Errorf("intelligence axis score = %v; want 70", one.Score)
	}
	if one.Cost == nil || !almostEqual(*one.Cost, 3) { // (3*2 + 6)/4
		t.Errorf("blend cost = %v; want 3", one.Cost)
	}
	if one.ScoreSource != aaSourceName {
		t.Errorf("score source = %q; want %q", one.ScoreSource, aaSourceName)
	}

	if p := mustPoint(t, res.Points, "a/null-score"); p.Score != nil || p.ScoreSource != "" {
		t.Error("a null source score must stay unscored with no ScoreSource, never estimated")
	}
	if p := mustPoint(t, res.Points, "a/no-pricing"); p.Cost != nil {
		t.Error("absent pricing must give nil cost")
	}
	if p := mustPoint(t, res.Points, "a/half-pricing"); p.Cost != nil {
		t.Error("output price missing: blend must be nil, not computed from input alone")
	}

	if len(res.Sources) != 1 {
		t.Fatalf("%d sources for a non-per_task basis; want exactly the AA one", len(res.Sources))
	}
	aa := res.Sources[0]
	if aa.Name != aaSourceName || aa.URL != openrouter.BenchmarksURL || aa.AsOf != "2026-10-01" || aa.Citation != feed.Citation || aa.Error != "" {
		t.Errorf("AA source = %+v; want name/URL/AsOf/Citation carried, no error", aa)
	}
}

// TestBuildChatPerTaskMeanAndMissingIsNil pins the per_task basis: the mean
// AvgCostPerTask over the model's own eval rows (nil-valued rows excluded), a
// model with no valued rows unpriced, and the second source naming the evals.
func TestBuildChatPerTaskMeanAndMissingIsNil(t *testing.T) {
	feed := &openrouter.Feed{
		AA: []openrouter.AAScore{
			{Slug: "a/one", Name: "One", Coding: f64p(50), InputPerM: f64p(1)},
			{Slug: "a/no-evals", Name: "No Evals", Coding: f64p(40), InputPerM: f64p(1)},
		},
		Evals: []openrouter.EvalScore{
			{Slug: "a/one", Benchmark: "gpqa_diamond", AvgCostPerTask: f64p(0.10)},
			{Slug: "a/one", Benchmark: "aime", AvgCostPerTask: f64p(0.30)},
			{Slug: "a/one", Benchmark: "browsecomp"}, // unvalued row: excluded from the mean
			{Slug: "other/model", Benchmark: "gpqa_diamond", AvgCostPerTask: f64p(9)},
		},
		EvalsAsOf: "2026-10-02",
	}
	res := BuildChat(feed, AxisCoding, CostPerTask)

	one := mustPoint(t, res.Points, "a/one")
	if one.Score == nil || !almostEqual(*one.Score, 50) {
		t.Errorf("coding axis score = %v; want 50", one.Score)
	}
	if one.Cost == nil || !almostEqual(*one.Cost, 0.20) { // mean(0.10, 0.30)
		t.Errorf("per-task cost = %v; want 0.20", one.Cost)
	}
	if p := mustPoint(t, res.Points, "a/no-evals"); p.Cost != nil {
		t.Error("a model with no valued eval rows must be unpriced, not zero-priced")
	}

	if len(res.Sources) != 2 {
		t.Fatalf("%d sources for per_task; want AA + evals", len(res.Sources))
	}
	ev := res.Sources[1]
	if ev.Name != evalsSourceName || ev.URL != openrouter.BenchmarksURL || ev.AsOf != "2026-10-02" {
		t.Errorf("evals source = %+v; want name/URL/AsOf carried", ev)
	}
}

// TestBuildChatNilFeedIsSourceError pins the advisory contract (ADR-009): a
// lost feed is an error ON THE SOURCE with no points — never a panic, never a
// fatal for the caller.
func TestBuildChatNilFeedIsSourceError(t *testing.T) {
	res := BuildChat(nil, AxisAgentic, CostBlendPerM)
	if len(res.Points) != 0 {
		t.Fatalf("%d points from a nil feed; want none", len(res.Points))
	}
	if len(res.Sources) != 1 || res.Sources[0].Error == "" {
		t.Fatalf("sources = %+v; want exactly one carrying an Error", res.Sources)
	}
	if res.Kind != KindChat || res.Axis != AxisAgentic || res.CostBasis != CostBlendPerM {
		t.Errorf("result header = %s/%s/%s; want the requested kind/axis/basis even on failure", res.Kind, res.Axis, res.CostBasis)
	}
}

// === DECISION RESULT ASSEMBLY ===

// TestBuildDecisionPricesViaSlugThenDeciderTable pins the cost precedence:
// OpenRouter's decision-model InputPerM when the board name maps to a listed
// slug (even when the decider table disagrees), the Conclave decider table
// otherwise, and nil for unmapped names. Also pins that only input_per_m
// prices decision models.
func TestBuildDecisionPricesViaSlugThenDeciderTable(t *testing.T) {
	board := &decisionindex.Board{
		Edition:      "v0.2.1",
		GeneratedUTC: "2026-09-28",
		Entries: []decisionindex.Entry{
			{Name: "Jev", Index: 57.91, ECE: f64p(0.08), MedianMs: f64p(900)},
			{Name: "Clef", Index: 61.21, SelfReported: true},
			{Name: "Mystery Model", Index: 40},
		},
		Check: &decisionindex.Check{Compared: 2},
	}
	models := []openrouter.DecisionModel{
		{Slug: "typesafe/jev-1.13", Name: "Jev", Priced: true, InputPerM: 0.05},
	}
	// The jev row deliberately disagrees with the catalog: the slug price must
	// win, which is the whole point of the precedence.
	table := []pricing.DeciderPrice{
		{Decider: "jev", Model: "jev-latest", InPerM: 0.99},
		{Decider: "clef", Model: "clef", InPerM: 0.24},
	}
	res := BuildDecision(board, models, table, CostInputPerM)

	if res.Kind != KindDecision || res.Axis != AxisDecision || res.CostBasis != CostInputPerM {
		t.Fatalf("result header = %s/%s/%s; want decision/decision/input_per_m", res.Kind, res.Axis, res.CostBasis)
	}

	jev := mustPoint(t, res.Points, "typesafe/jev-1.13")
	if jev.Name != "Jev" || jev.Cost == nil || !almostEqual(*jev.Cost, 0.05) {
		t.Errorf("jev point = %+v; want the catalog slug price 0.05, not the table's 0.99", jev)
	}
	if jev.ECE == nil || !almostEqual(*jev.ECE, 0.08) || jev.LatencyMs == nil || !almostEqual(*jev.LatencyMs, 900) {
		t.Errorf("jev ECE/latency = %v/%v; want the board's carried through", jev.ECE, jev.LatencyMs)
	}
	if jev.Score == nil || !almostEqual(*jev.Score, 57.91) || jev.ScoreSource != boardSourceName {
		t.Errorf("jev score = %v (%q); want the board index 57.91 from %q", jev.Score, jev.ScoreSource, boardSourceName)
	}

	clef := mustPoint(t, res.Points, "clef")
	if clef.Cost == nil || !almostEqual(*clef.Cost, 0.24) {
		t.Errorf("clef cost = %v; want the decider table's 0.24", clef.Cost)
	}
	if !clef.SelfReported {
		t.Error("the board's SelfReported flag must reach the point")
	}

	mystery := mustPoint(t, res.Points, "Mystery Model")
	if mystery.Cost != nil {
		t.Error("an unmapped board name must be unpriced (shown, never priced — ADR-018)")
	}

	// Only input_per_m is supported for decision models: any other basis
	// leaves them unpriced rather than mislabelling a list price.
	other := BuildDecision(board, models, table, CostBlendPerM)
	for _, p := range other.Points {
		if p.Cost != nil {
			t.Errorf("%s priced %v on basis %s; decision models support input_per_m only", p.ID, *p.Cost, CostBlendPerM)
		}
	}
}

// TestBuildDecisionAddsUnscoredOpenRouterModels pins that catalog decision
// models missing from the board still appear (unscored — never estimated),
// that catalog "~" aliases do not double-list their target, and that
// board-matched slugs are not re-added.
func TestBuildDecisionAddsUnscoredOpenRouterModels(t *testing.T) {
	board := &decisionindex.Board{
		Edition:      "v0.2.1",
		GeneratedUTC: "2026-09-28",
		Entries:      []decisionindex.Entry{{Name: "Clef", Index: 61.21}},
		Check:        &decisionindex.Check{Compared: 1},
	}
	models := []openrouter.DecisionModel{
		{Slug: "liquid/d1", Name: "D1", Priced: true, InputPerM: 0.10},
		{Slug: "solar-ai/solar-decide", Name: "Solar Decide", Priced: true, InputPerM: 0.05},
		{Slug: "~liquid/d1", Name: "D1 (alias)", AliasTarget: "liquid/d1", Priced: true, InputPerM: 0.10},
	}
	table := []pricing.DeciderPrice{{Decider: "clef", Model: "clef", InPerM: 0.24}}
	res := BuildDecision(board, models, table, CostInputPerM)

	d1 := mustPoint(t, res.Points, "liquid/d1")
	if d1.Score != nil || d1.ScoreSource != "" {
		t.Error("an off-board catalog model must be unscored, never given an estimated score")
	}
	if d1.Cost == nil || !almostEqual(*d1.Cost, 0.10) {
		t.Errorf("d1 cost = %v; want its catalog InputPerM (cost is independent of score)", d1.Cost)
	}
	if d1.Kind != KindDecision {
		t.Errorf("d1 kind = %s; want decision", d1.Kind)
	}
	mustPoint(t, res.Points, "solar-ai/solar-decide") // the other off-board model appears too
	if byID(t, res.Points, "~liquid/d1") != nil {
		t.Error("a '~' alias row double-listed its target")
	}
	clefCount := 0
	for _, p := range res.Points {
		if p.ID == "clef" {
			clefCount++
		}
	}
	if clefCount != 1 {
		t.Errorf("clef appears %d times; want exactly the board point (no duplicate from the catalog side)", clefCount)
	}
}

// TestBuildDecisionNilCheckWarns pins the board source's Warning: a nil Check
// (mirror unreachable) must surface as "cross-check did not run", check
// mismatches and the board's own warnings must both be carried — ADR-018
// makes a mirror disagreement visible, never silent.
func TestBuildDecisionNilCheckWarns(t *testing.T) {
	base := decisionindex.Board{
		Edition:      "v0.2.1",
		GeneratedUTC: "2026-09-28",
		Entries:      []decisionindex.Entry{{Name: "Jev", Index: 57.91}},
	}

	nilCheck := base // Check stays nil: the mirror was unavailable
	res := BuildDecision(&nilCheck, nil, nil, CostInputPerM)
	if len(res.Sources) != 1 {
		t.Fatalf("%d sources; want exactly the board's", len(res.Sources))
	}
	src := res.Sources[0]
	if src.Name != boardSourceName || src.Edition != "v0.2.1" || src.AsOf != "2026-09-28" {
		t.Errorf("board source = %+v; want name/edition/AsOf from the board", src)
	}
	if src.Warning == "" {
		t.Error("a nil Check must add a warning that the mirror cross-check did not run")
	}

	mismatched := base
	mismatched.Warnings = []string{"upstream reweighted gold set"}
	mismatched.Check = &decisionindex.Check{Compared: 3, MaxDelta: 0.11, Mismatch: []string{"Jev"}}
	res = BuildDecision(&mismatched, nil, nil, CostInputPerM)
	w := res.Sources[0].Warning
	for _, want := range []string{"Jev", "upstream reweighted gold set"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning %q missing %q", w, want)
		}
	}

	clean := base
	clean.Check = &decisionindex.Check{Compared: 3, MaxDelta: 0.01}
	if w := BuildDecision(&clean, nil, nil, CostInputPerM).Sources[0].Warning; w != "" {
		t.Errorf("agreeing check produced warning %q; want none", w)
	}
}

// TestBuildDecisionNilBoardIsSourceError mirrors the chat nil-feed contract:
// no board means no decision scores at all — an error on the source, zero
// points, never a panic.
func TestBuildDecisionNilBoardIsSourceError(t *testing.T) {
	res := BuildDecision(nil, nil, nil, CostInputPerM)
	if len(res.Points) != 0 {
		t.Fatalf("%d points from a nil board; want none", len(res.Points))
	}
	if len(res.Sources) != 1 || res.Sources[0].Error == "" {
		t.Fatalf("sources = %+v; want exactly one carrying an Error", res.Sources)
	}
}

// TestKnownDecisionFrontier reproduces the real v0.2.1 decision frontier from
// typed-in values, so a regression in mapping, pricing precedence or Pareto
// maths breaks the known picture.
//
// Scores: Decision Index v0.2.1 (HF Space multimodalart/jev-decision-index),
// verified 2026-10-03. Kev/Tev1/Jev list prices: OpenRouter decision-model
// catalog, verified 2026-10-03. Clef/Clef-flash: Cloudflare Workers AI pricing
// (the rows behind internal/pricing/deciders.go, as-of 2026-10-02).
//
//	On input_per_m the frontier is Jev (57.91 @ $0.042) and Clef
//	(61.21 @ $0.24, self-reported): Jev dominates Kev and Tev1 at the
//	same $0.042 with a higher index, and dominates Clef-flash
//	(57.07 @ $0.09) on both axes.
func TestKnownDecisionFrontier(t *testing.T) {
	board := &decisionindex.Board{
		Edition:      "v0.2.1",
		GeneratedUTC: "2026-09-28",
		Entries: []decisionindex.Entry{
			{Name: "Kev 4B", Index: 34.64},
			{Name: "Tev1-4B-experimental", Index: 29.24},
			{Name: "Jev", Index: 57.91},
			{Name: "Clef", Index: 61.21, SelfReported: true, FromMirror: true},
			{Name: "Clef-flash", Index: 57.07, SelfReported: true, FromMirror: true},
		},
		Check: &decisionindex.Check{Compared: 3, MaxDelta: 0.01},
	}
	models := []openrouter.DecisionModel{
		{Slug: "typesafe/jev-1.13", Name: "Jev", Priced: true, InputPerM: 0.042},
		{Slug: "jaredpalmer/kev-4b", Name: "Kev 4B", Priced: true, InputPerM: 0.042},
		{Slug: "togethercomputer/tev1-4b-experimental", Name: "Tev1-4B-experimental", Priced: true, InputPerM: 0.042},
	}
	table := []pricing.DeciderPrice{
		{Decider: "clef", Model: "clef", InPerM: 0.24},
		{Decider: "clef-flash", Model: "clef-flash", InPerM: 0.09},
	}

	pts := Pareto(BuildDecision(board, models, table, CostInputPerM).Points)

	onFrontier := map[string]bool{}
	for _, p := range pts {
		onFrontier[p.ID] = p.OnFrontier
	}
	for _, id := range []string{"typesafe/jev-1.13", "clef"} {
		if !onFrontier[id] {
			t.Errorf("%s left the frontier; the known v0.2.1 frontier is Jev and Clef", id)
		}
	}
	for _, id := range []string{"jaredpalmer/kev-4b", "togethercomputer/tev1-4b-experimental", "clef-flash"} {
		if onFrontier[id] {
			t.Errorf("%s is on the frontier; Jev dominates it (equal or cheaper price, higher index)", id)
		}
	}

	jev := mustPoint(t, pts, "typesafe/jev-1.13")
	if jev.Score == nil || !almostEqual(*jev.Score, 57.91) || jev.Cost == nil || !almostEqual(*jev.Cost, 0.042) {
		t.Errorf("jev = score %v cost %v; want 57.91 @ 0.042", jev.Score, jev.Cost)
	}
	clef := mustPoint(t, pts, "clef")
	if clef.Score == nil || !almostEqual(*clef.Score, 61.21) || clef.Cost == nil || !almostEqual(*clef.Cost, 0.24) {
		t.Errorf("clef = score %v cost %v; want 61.21 @ 0.24", clef.Score, clef.Cost)
	}
	if !clef.SelfReported {
		t.Error("Clef must carry self-reported: its row exists only in the mirror (ADR-018)")
	}
}

// TestBuildDecisionDistinguishesFreeFromUnpricedCatalog pins the catalog's
// Priced presence bit: zero is a real free price only when present, while an
// unpriced mapped model falls through to the decider table and an unpriced
// catalog-only model remains nil.
func TestBuildDecisionDistinguishesFreeFromUnpricedCatalog(t *testing.T) {
	board := &decisionindex.Board{
		Entries: []decisionindex.Entry{{Name: "Jev", Index: 57.91}},
		Check:   &decisionindex.Check{},
	}
	models := []openrouter.DecisionModel{
		{Slug: "typesafe/jev-1.13", Name: "Jev", Priced: false},
		{Slug: "inception/free", Name: "Free", Priced: true, InputPerM: 0},
		{Slug: "liquid/unpriced", Name: "Unpriced", Priced: false},
	}
	table := []pricing.DeciderPrice{{Decider: "jev", InPerM: 0.42}}

	res := BuildDecision(board, models, table, CostInputPerM)
	jev := mustPoint(t, res.Points, "typesafe/jev-1.13")
	if jev.Cost == nil || !almostEqual(*jev.Cost, 0.42) {
		t.Errorf("unpriced mapped model cost = %v; want table fallback 0.42", jev.Cost)
	}
	free := mustPoint(t, res.Points, "inception/free")
	if free.Cost == nil || *free.Cost != 0 {
		t.Errorf("genuinely free model cost = %v; want non-nil zero", free.Cost)
	}
	if unpriced := mustPoint(t, res.Points, "liquid/unpriced"); unpriced.Cost != nil {
		t.Errorf("unpriced catalog model cost = %v; want nil", *unpriced.Cost)
	}
}

// TestBuildDecisionCopiesBoardOptionalValues ensures the assembled Result
// owns mutable optional values instead of aliasing cached source data.
func TestBuildDecisionCopiesBoardOptionalValues(t *testing.T) {
	board := &decisionindex.Board{
		Entries: []decisionindex.Entry{{Name: "Jev", Index: 57.91, ECE: f64p(0.08), MedianMs: f64p(900)}},
		Check:   &decisionindex.Check{},
	}
	res := BuildDecision(board, nil, nil, CostInputPerM)
	p := mustPoint(t, res.Points, "typesafe/jev-1.13")
	*p.ECE = 0.99
	*p.LatencyMs = 1
	if got := *board.Entries[0].ECE; !almostEqual(got, 0.08) {
		t.Errorf("mutating result ECE changed board to %v; want 0.08", got)
	}
	if got := *board.Entries[0].MedianMs; !almostEqual(got, 900) {
		t.Errorf("mutating result latency changed board to %v; want 900", got)
	}
}

// TestBuildDecisionDeduplicatesWarningsStable pins first-occurrence warning
// order across board warnings and synthesized mirror health messages.
func TestBuildDecisionDeduplicatesWarningsStable(t *testing.T) {
	const missing = "Decision Index mirror cross-check did not run"
	board := &decisionindex.Board{
		Warnings: []string{missing, "upstream warning", missing, "upstream warning"},
	}
	warning := BuildDecision(board, nil, nil, CostInputPerM).Sources[0].Warning
	if want := missing + "; upstream warning"; warning != want {
		t.Errorf("warning = %q; want stable deduplicated %q", warning, want)
	}
}

// Refuted 2026-10-03: a down Decision Index hid every catalog model behind
// "UNSCORED (0)". The catalog must still appear, unscored.
func TestBuildDecisionBoardDownKeepsCatalogUnscored(t *testing.T) {
	models := []openrouter.DecisionModel{
		{Slug: "liquid/d1", Name: "LiquidAI: D1", Priced: true, InputPerM: 0.04},
		{Slug: "jaredpalmer/kev-4b", Name: "Kev 4B", Priced: true, InputPerM: 0.042},
	}
	res := BuildDecision(nil, models, nil, CostInputPerM)
	if len(res.Points) != 2 {
		t.Fatalf("points = %d, want both catalog models", len(res.Points))
	}
	for _, p := range res.Points {
		if p.Score != nil {
			t.Errorf("%s scored with no board", p.ID)
		}
	}
	if len(res.Sources) == 0 || res.Sources[0].Error == "" {
		t.Errorf("board source error missing: %+v", res.Sources)
	}
}

// Refuted 2026-10-03: an evals failure must surface on the per_task source,
// not vanish.
func TestBuildChatEvalsErrorReachesPerTaskSource(t *testing.T) {
	feed := &openrouter.Feed{EvalsError: "fetch openrouter source: HTTP 500"}
	res := BuildChat(feed, AxisIntelligence, CostPerTask)
	found := false
	for _, s := range res.Sources {
		if s.Name == evalsSourceName && s.Error != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("evals error not carried: %+v", res.Sources)
	}
}
