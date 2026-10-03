// Tests for Compute, the pure Decision Index formula (ADR-018).
//
// Every fixture here is SYNTHETIC: the Decision Index data carries no licence,
// so upstream files are never committed (ADR-018). Where a number comes from
// the real edition it is typed in with a citation comment.
package decisionindex

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// === Synthetic fixture builders ===

type tArea struct {
	id     string
	weight float64
	panel  []tBench
}

type tBench struct {
	id     int
	gold   bool
	chance float64
	// tracks, when set, become the benchmark's in-index tracks.
	tracks []tTrack
	// lower makes this a lower-is-better (Brier) benchmark with this baseline.
	lower float64
}

type tTrack struct {
	name     string
	random   float64
	headline bool
}

func buildMethodology(t *testing.T, areas []tArea) []byte {
	t.Helper()
	var mAreas, chance, lower, benches []map[string]any
	for _, a := range areas {
		var panel []map[string]any
		for _, b := range a.panel {
			p := map[string]any{"id": b.id}
			if b.gold {
				p["gold"] = 1.2
			}
			panel = append(panel, p)
			if b.lower > 0 {
				lower = append(lower, map[string]any{"id": b.id, "baseline": b.lower, "best": 0.0})
			} else {
				chance = append(chance, map[string]any{"id": b.id, "chance": b.chance})
			}
			var tracks []map[string]any
			for _, tr := range b.tracks {
				tracks = append(tracks, map[string]any{"track": tr.name, "random": tr.random, "headline": tr.headline, "in_index": true})
			}
			benches = append(benches, map[string]any{"id": b.id, "tracks": tracks})
		}
		mAreas = append(mAreas, map[string]any{"id": a.id, "weight": a.weight, "panel": panel})
	}
	b, err := json.Marshal(map[string]any{
		"edition":    map[string]any{"id": Edition, "panel_id": testPanelID},
		"benchmarks": benches,
		"index":      map[string]any{"panel_id": testPanelID, "areas": mAreas, "chance_levels": chance, "lower_rules": lower},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// row builds one upstream model row. raws maps benchmark id to its
// coverage-adjusted raw score; briers maps a lower-is-better benchmark to its
// Brier loss (stored under results, as upstream does).
func row(engine string, raws map[int]float64, briers map[int]float64) map[string]any {
	bm := map[string]any{}
	for id, v := range raws {
		bm[itoa(id)] = map[string]any{"raw": v, "coverage": 1.0}
	}
	res := map[string]any{}
	for id, v := range briers {
		bm[itoa(id)] = map[string]any{"raw": 0.0, "coverage": 1.0}
		res[itoa(id)] = map[string]any{"score": v}
	}
	return map[string]any{"engine": engine, "name": engine, "benchmarks": bm, "results": res}
}

func buildIndex(t *testing.T, rows ...map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"generated_utc": "2026-09-28T00:00:00Z",
		"suite": map[string]any{"panel_id": testPanelID}, "models": rows})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// testPanelID stands in for the edition's panel id (v0.2.1 publishes
// "decision-index-0.2.1" in methodology `edition.panel_id`, `index.panel_id`
// and the index file's `suite.panel_id`).
const testPanelID = "decision-index-test"

// mutate decodes a fixture, lets f edit it as generic JSON and re-encodes it,
// so a test can corrupt one field without a bespoke builder.
func mutate(t *testing.T, b []byte, f func(map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	f(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func onlyEntry(t *testing.T, meth, idx []byte) Entry {
	t.Helper()
	es, err := Compute(meth, idx)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(es) != 1 {
		t.Fatalf("want 1 entry, got %d", len(es))
	}
	return es[0]
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// singleArea wraps one area holding the whole index weight.
func singleArea(panel ...tBench) []tArea {
	return []tArea{{id: "knowledge", weight: 1, panel: panel}}
}

// === Tests ===

// The methodology's own worked example. Numbers typed from
// methodology-v0.2.1.json `index.worked_example` (model "Jev", fetched
// 2026-10-03): area skill scores knowledge 0.514, language 0.6202,
// retrieval 0.5542, tools 0.7509, arts 0.3766; scores.balanced_skill 57.91.
// Area weights from `index.weights`. Each area is given one chance-0 benchmark
// whose raw score equals the published area score, so the test pins the
// area-weighting step of the formula exactly as the edition states it.
func TestComputeReproducesWorkedExample(t *testing.T) {
	areas := []tArea{
		{id: "knowledge", weight: 0.2585, panel: []tBench{{id: 1}}},
		{id: "language", weight: 0.2585, panel: []tBench{{id: 2}}},
		{id: "retrieval", weight: 0.2002, panel: []tBench{{id: 3}}},
		{id: "tools", weight: 0.1828, panel: []tBench{{id: 4}}},
		{id: "arts", weight: 0.1000, panel: []tBench{{id: 5}}},
	}
	want := map[string]float64{"knowledge": 0.514, "language": 0.6202, "retrieval": 0.5542, "tools": 0.7509, "arts": 0.3766}
	e := onlyEntry(t, buildMethodology(t, areas), buildIndex(t,
		row("jev", map[int]float64{1: 0.514, 2: 0.6202, 3: 0.5542, 4: 0.7509, 5: 0.3766}, nil)))
	// Upstream rounds to 2 decimals, so half a hundredth is the honest bound.
	if !near(e.Index, 57.91, 0.005) {
		t.Errorf("Index = %.4f, want 57.91 (worked example)", e.Index)
	}
	for a, v := range want {
		if !near(e.Areas[a], v, 1e-9) {
			t.Errorf("Areas[%s] = %v, want %v", a, e.Areas[a], v)
		}
	}
}

func TestComputeChanceCorrectionClipsBelowChance(t *testing.T) {
	meth := buildMethodology(t, singleArea(tBench{id: 1, chance: 0.5}))
	// Below chance must clip to 0, never go negative and drag the area down.
	if e := onlyEntry(t, meth, buildIndex(t, row("m", map[int]float64{1: 0.3}, nil))); e.Index != 0 {
		t.Errorf("below-chance Index = %v, want 0", e.Index)
	}
	// (0.75 - 0.5) / (1 - 0.5) = 0.5: the correction itself, not the raw score.
	if e := onlyEntry(t, meth, buildIndex(t, row("m", map[int]float64{1: 0.75}, nil))); !near(e.Index, 50, 1e-9) {
		t.Errorf("Index = %v, want 50", e.Index)
	}
}

// ForecastBench is a Brier LOSS: lower is better, so the chance formula would
// reward bad forecasts. Methodology `index.lower_rules`:
// clip((0.25 - Brier) / (0.25 - 0)) x coverage. Jev's published Brier 0.1736
// converts to 0.3056 (index-v0.2.1.json jev.benchmarks["48"].skill).
func TestForecastBenchUsesBrierConversion(t *testing.T) {
	meth := buildMethodology(t, singleArea(tBench{id: 48, lower: 0.25}))
	if e := onlyEntry(t, meth, buildIndex(t, row("m", nil, map[int]float64{48: 0.1736}))); !near(e.Index, 30.56, 1e-9) {
		t.Errorf("Index = %v, want 30.56", e.Index)
	}
	// Worse than always-0.5 scores 0, not negative.
	if e := onlyEntry(t, meth, buildIndex(t, row("m", nil, map[int]float64{48: 0.4}))); e.Index != 0 {
		t.Errorf("Index = %v, want 0", e.Index)
	}
}

func TestGoldBenchmarksWeigh1_2(t *testing.T) {
	meth := buildMethodology(t, singleArea(tBench{id: 1, gold: true}, tBench{id: 2}))
	e := onlyEntry(t, meth, buildIndex(t, row("m", map[int]float64{1: 1, 2: 0}, nil)))
	// 1.2*1 / (1.2 + 1.0), not the unweighted 0.5.
	if want := 100 * 1.2 / 2.2; !near(e.Index, want, 1e-9) {
		t.Errorf("Index = %v, want %v", e.Index, want)
	}
}

func TestMissingBenchmarkScoresZero(t *testing.T) {
	meth := buildMethodology(t, singleArea(tBench{id: 1}, tBench{id: 2}))
	e := onlyEntry(t, meth, buildIndex(t, row("m", map[int]float64{1: 1}, nil)))
	// Unanswered counts as wrong: the denominator keeps benchmark 2.
	if !near(e.Index, 50, 1e-9) {
		t.Errorf("Index = %v, want 50 (missing benchmark must count as 0, not be skipped)", e.Index)
	}
	if e.Benchmarks != 1 {
		t.Errorf("Benchmarks = %d, want 1", e.Benchmarks)
	}
}

// GSM8K-style benchmarks correct each track against its OWN chance level and
// then average (methodology `index.steps[1]` and `[4]`); correcting the
// averaged raw against the averaged chance gives a different number.
func TestMultiTrackBenchmarkCorrectsEachTrack(t *testing.T) {
	meth := buildMethodology(t, singleArea(tBench{id: 30, chance: 0.175, tracks: []tTrack{
		{name: "GSM8K-4choice", random: 0.25}, {name: "GSM8K-10choice", random: 0.1},
	}}))
	r := row("m", nil, nil)
	r["benchmarks"] = map[string]any{"30": map[string]any{"raw": 0.1892, "coverage": 1.0, "tracks": []map[string]any{
		{"track": "GSM8K-4choice", "score": 0.2881}, {"track": "GSM8K-10choice", "score": 0.0902},
	}}}
	// Typed from index-v0.2.1.json models[0] (system-one-gemma) benchmarks["30"]:
	// tracks 0.2881 / 0.0902 publish skill 0.0254. Track-wise:
	// mean(clip((0.2881-0.25)/0.75), clip((0.0902-0.1)/0.9)) = 0.0254.
	e := onlyEntry(t, meth, buildIndex(t, r))
	if !near(e.Index, 2.54, 0.005) {
		t.Errorf("Index = %v, want ~2.54 (per-track correction)", e.Index)
	}
}

func TestComputeIncludesTopLevelJev(t *testing.T) {
	meth := buildMethodology(t, singleArea(tBench{id: 1}))
	idx, _ := json.Marshal(map[string]any{
		"suite":  map[string]any{"panel_id": testPanelID},
		"models": []any{row("a", map[int]float64{1: 0.2}, nil)},
		"jev":    row("jev", map[int]float64{1: 0.9}, nil),
	})
	es, err := Compute(meth, idx)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[1].Engine != "jev" || !near(es[1].Index, 90, 1e-9) {
		t.Fatalf("top-level jev missing or wrong: %+v", es)
	}
}

// === Refuted defects (Codex review, 2026-10-03) ===

// Corrupt edition numbers used to flow straight into the formula: gold 0
// produced Index NaN, coverage 2 produced Index 200, and an empty weighted
// area was silently dropped so weights summing to 1.5 passed. Each must now
// reject the edition with an error naming the field.
func TestComputeRejectsCorruptNumbers(t *testing.T) {
	two := []tArea{
		{id: "knowledge", weight: 1, panel: []tBench{{id: 1}}},
		{id: "language", weight: 0.5},
	}
	good := buildMethodology(t, singleArea(tBench{id: 1, chance: 0.25}))
	goodIdx := buildIndex(t, row("m", map[int]float64{1: 0.5}, nil))
	setBench := func(field string, v any) func(map[string]any) {
		return func(m map[string]any) {
			b := m["models"].([]any)[0].(map[string]any)["benchmarks"].(map[string]any)["1"].(map[string]any)
			b[field] = v
		}
	}
	setMeth := func(f func(ix map[string]any)) func(map[string]any) {
		return func(m map[string]any) { f(m["index"].(map[string]any)) }
	}
	cases := []struct {
		name      string
		meth, idx []byte
		field     string
	}{
		{"gold zero", mutate(t, good, setMeth(func(ix map[string]any) {
			ix["areas"].([]any)[0].(map[string]any)["panel"].([]any)[0].(map[string]any)["gold"] = 0.0
		})), goodIdx, "gold"},
		{"coverage above one", good, mutate(t, goodIdx, setBench("coverage", 2.0)), "coverage"},
		{"raw above one", good, mutate(t, goodIdx, setBench("raw", 1.5)), "raw"},
		{"raw negative", good, mutate(t, goodIdx, setBench("raw", -0.1)), "raw"},
		{"chance one", mutate(t, good, setMeth(func(ix map[string]any) {
			ix["chance_levels"].([]any)[0].(map[string]any)["chance"] = 1.0
		})), goodIdx, "chance"},
		{"negative area weight", mutate(t, good, setMeth(func(ix map[string]any) {
			a := ix["areas"].([]any)
			ix["areas"] = append(a, map[string]any{"id": "x", "weight": -0.5, "panel": a[0].(map[string]any)["panel"]})
		})), goodIdx, "weight"},
		{"empty weighted area", buildMethodology(t, two), goodIdx, "language"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			es, err := Compute(c.meth, c.idx)
			if err == nil {
				t.Fatalf("corrupt edition accepted: %+v", es)
			}
			if !strings.Contains(err.Error(), c.field) {
				t.Errorf("error %q does not name the field %q", err, c.field)
			}
		})
	}
}

// A multi-track benchmark whose in-index tracks are absent used to fall back
// to scalar raw scoring (raw 0.8 gave Index 80). Methodology `index.steps[0]`
// and `[1]`: an unanswered track counts as wrong and tracks average equally,
// so the missing tracks score 0 and raw is never consulted.
func TestMultiTrackBenchmarkMissingTracksScoreZeroNotRaw(t *testing.T) {
	meth := buildMethodology(t, singleArea(tBench{id: 30, chance: 0.175, tracks: []tTrack{
		{name: "GSM8K-4choice", random: 0.25}, {name: "GSM8K-10choice", random: 0.1},
	}}))
	if e := onlyEntry(t, meth, buildIndex(t, row("m", map[int]float64{30: 0.8}, nil))); e.Index != 0 {
		t.Errorf("no tracks: Index = %v, want 0 (never scalar raw)", e.Index)
	}
	r := row("m", nil, nil)
	r["benchmarks"] = map[string]any{"30": map[string]any{"raw": 0.8, "coverage": 1.0, "tracks": []map[string]any{
		{"track": "GSM8K-4choice", "score": 1.0},
	}}}
	// One track perfect, the other missing: mean(1, 0) = 0.5.
	if e := onlyEntry(t, meth, buildIndex(t, r)); !near(e.Index, 50, 1e-9) {
		t.Errorf("one track missing: Index = %v, want 50", e.Index)
	}
}
