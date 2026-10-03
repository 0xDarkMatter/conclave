// Compute: the Decision Index formula for the pinned edition, applied to the
// upstream index file's per-benchmark scores. Pure (no I/O, no clock).
//
// Everything that varies by edition is READ from the methodology file, never
// hard-coded: area weights and panels (`index.areas`), gold weights (panel
// `gold`), chance levels (`index.chance_levels`), per-track chance
// (`benchmarks[].tracks[].random`) and lower-is-better rules
// (`index.lower_rules`). What is coded here is the shape of the formula, taken
// from methodology-v0.2.1.json `index.steps` and `index.functions`:
//
//	k(bench)  = clip((s - chance) / (1 - chance))         steps[4]
//	k(lower)  = clip((baseline - loss) / (baseline - best)) x coverage   steps[2]
//	k(multi)  = mean over in-index tracks of each track's k               steps[1]
//	area      = sum(gold_w * k) / sum(gold_w), gold 1.2 else 1.0          steps[3]
//	index     = 100 x sum(area_weight * area)                             steps[5]
//
// s is upstream's coverage-adjusted `raw` (steps[0]: unanswered counts as
// wrong); a panel benchmark missing from a row scores 0 and stays in the
// denominator. Verified 2026-10-03 against every v0.2.1 row: the per-benchmark
// k matches upstream's published `skill` and the index matches
// `scores.balanced_skill` within 0.006 (upstream rounds to 2 dp). ADR-018.
//
// Invariants: the edition identity and every number the formula reads are
// validated BEFORE computing (a bad value rejects the edition with an error
// naming the field), and no Entry leaves here with a non-finite Index or one
// outside 0..100 (such a row is dropped with a warning). Never NaN.
package decisionindex

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// === Wire shapes (only the fields the formula reads) ===

type methodologyWire struct {
	// edition identifies the file's own edition; v0.2.1 publishes
	// {"id":"v0.2.1","panel_id":"decision-index-0.2.1",...}.
	Edition struct {
		ID      string `json:"id"`
		PanelID string `json:"panel_id"`
	} `json:"edition"`
	Benchmarks []struct {
		ID     int         `json:"id"`
		Tracks []trackWire `json:"tracks"`
	} `json:"benchmarks"`
	Index struct {
		PanelID string `json:"panel_id"`
		Areas   []struct {
			ID     string  `json:"id"`
			Weight float64 `json:"weight"`
			Panel  []struct {
				ID   int      `json:"id"`
				Gold *float64 `json:"gold"`
			} `json:"panel"`
		} `json:"areas"`
		ChanceLevels []struct {
			ID     int     `json:"id"`
			Chance float64 `json:"chance"`
		} `json:"chance_levels"`
		LowerRules []struct {
			ID       int     `json:"id"`
			Baseline float64 `json:"baseline"`
			Best     float64 `json:"best"`
		} `json:"lower_rules"`
	} `json:"index"`
}

type trackWire struct {
	Track    string  `json:"track"`
	Label    string  `json:"label"`
	Random   float64 `json:"random"`
	Headline bool    `json:"headline"`
	InIndex  bool    `json:"in_index"`
}

type indexWire struct {
	GeneratedUTC string `json:"generated_utc"`
	// suite.panel_id is the only edition identifier the index file carries
	// that matches the methodology: suite.edition is upstream's internal
	// release name ("release-v2.1" for v0.2.1), not the public edition id.
	Suite *struct {
		PanelID string `json:"panel_id"`
	} `json:"suite"`
	Models []rowWire `json:"models"`
	Jev    *rowWire  `json:"jev"` // the closed model sits outside models[]
}

type rowWire struct {
	Engine      string `json:"engine"`
	Name        string `json:"name"`
	Calibration *struct {
		ECE *float64 `json:"ece"`
	} `json:"calibration"`
	Latency *struct {
		Median *float64 `json:"median"`
		P95    *float64 `json:"p95"`
	} `json:"latency"`
	// Keyed by benchmark id as a decimal string ("48").
	Benchmarks map[string]benchWire `json:"benchmarks"`
	// results carries the metric in its own units; for lower-is-better
	// benchmarks that is the Brier loss the conversion needs (raw holds
	// upstream's already-converted value, which we recompute rather than trust).
	Results map[string]struct {
		Score *float64 `json:"score"`
	} `json:"results"`
}

type benchWire struct {
	Raw      float64 `json:"raw"`
	Coverage float64 `json:"coverage"`
	Tracks   []struct {
		Track string  `json:"track"`
		Score float64 `json:"score"`
	} `json:"tracks"`
}

// === Methodology, parsed and validated ===

type area struct {
	id     string
	weight float64
	panel  []panelBench
}

type panelBench struct {
	id   int
	gold float64 // 1.2 for gold, 1.0 otherwise
}

type lowerRule struct{ baseline, best float64 }

type method struct {
	panelID string
	areas   []area
	chance  map[int]float64
	lower   map[int]lowerRule
	tracks  map[int][]trackWire // in-index tracks only, for multi-track benchmarks
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// unit reports v in [0,1]; chanceLevel reports v in [0,1), where 1 would
// divide by zero in the correction.
func unit(v float64) bool        { return finite(v) && v >= 0 && v <= 1 }
func chanceLevel(v float64) bool { return finite(v) && v >= 0 && v < 1 }

// weightSumTolerance: v0.2.1's published 4-dp weights sum to exactly 1.0000,
// so anything beyond float noise is a format change, not rounding. The
// methodology documents no renormalisation step, so none is applied.
const weightSumTolerance = 1e-6

func parseMethodology(b []byte) (*method, error) {
	var w methodologyWire
	if err := json.Unmarshal(b, &w); err != nil {
		return nil, fmt.Errorf("decode methodology: %w", err)
	}
	// Identity first: data from another edition must never be relabelled as
	// the pinned one, however well-formed (ADR-018).
	if w.Edition.ID != Edition {
		return nil, fmt.Errorf("methodology: edition.id %q, want %q", w.Edition.ID, Edition)
	}
	if w.Index.PanelID == "" {
		return nil, errors.New("methodology: index.panel_id is empty")
	}
	if w.Edition.PanelID != "" && w.Edition.PanelID != w.Index.PanelID {
		return nil, fmt.Errorf("methodology: edition.panel_id %q != index.panel_id %q", w.Edition.PanelID, w.Index.PanelID)
	}
	m := &method{panelID: w.Index.PanelID, chance: map[int]float64{}, lower: map[int]lowerRule{}, tracks: map[int][]trackWire{}}
	for _, c := range w.Index.ChanceLevels {
		if !chanceLevel(c.Chance) {
			return nil, fmt.Errorf("methodology: chance_levels[id=%d].chance %v outside [0,1)", c.ID, c.Chance)
		}
		m.chance[c.ID] = c.Chance
	}
	for _, l := range w.Index.LowerRules {
		if !finite(l.Baseline) || !finite(l.Best) || l.Baseline == l.Best {
			return nil, fmt.Errorf("methodology: lower_rules[id=%d] baseline %v / best %v unusable", l.ID, l.Baseline, l.Best)
		}
		m.lower[l.ID] = lowerRule{l.Baseline, l.Best}
	}
	for _, bm := range w.Benchmarks {
		var in []trackWire
		headline := false
		for _, t := range bm.Tracks {
			headline = headline || t.Headline
			if t.InIndex {
				if !chanceLevel(t.Random) {
					return nil, fmt.Errorf("methodology: benchmark %d track %q random %v outside [0,1)", bm.ID, t.Track, t.Random)
				}
				in = append(in, t)
			}
		}
		// A headline track (iSarcasmEval A-English) IS the benchmark score, so
		// the benchmark is corrected once against its own chance level.
		if !headline && len(in) > 0 {
			m.tracks[bm.ID] = in
		}
	}
	var sum float64
	for _, a := range w.Index.Areas {
		if !finite(a.Weight) || a.Weight < 0 {
			return nil, fmt.Errorf("methodology: area %q weight %v is not a finite non-negative number", a.ID, a.Weight)
		}
		sum += a.Weight
		// "games" is folded into knowledge with weight 0 and an empty panel; a
		// zero-weight area contributes nothing whatever its panel holds.
		if a.Weight == 0 {
			continue
		}
		// A weighted area with no benchmarks would be 0/0. It used to be
		// skipped, which let weights summing to 1.5 pass the sum check below.
		if len(a.Panel) == 0 {
			return nil, fmt.Errorf("methodology: area %q has weight %v but an empty panel", a.ID, a.Weight)
		}
		ar := area{id: a.ID, weight: a.Weight}
		for _, p := range a.Panel {
			_, hasChance := m.chance[p.ID]
			_, isLower := m.lower[p.ID]
			_, isMulti := m.tracks[p.ID]
			if !hasChance && !isLower && !isMulti {
				return nil, fmt.Errorf("methodology: panel benchmark %d (%s) has no chance level", p.ID, a.ID)
			}
			g := 1.0
			if p.Gold != nil {
				g = *p.Gold
			}
			if !finite(g) || g <= 0 {
				return nil, fmt.Errorf("methodology: area %q benchmark %d gold weight %v must be > 0", a.ID, p.ID, g)
			}
			ar.panel = append(ar.panel, panelBench{id: p.ID, gold: g})
		}
		m.areas = append(m.areas, ar)
	}
	if len(m.areas) == 0 {
		return nil, errors.New("methodology: no weighted areas")
	}
	if math.Abs(sum-1) > weightSumTolerance {
		return nil, fmt.Errorf("methodology: area weights sum to %v, not 1", sum)
	}
	return m, nil
}

// validateRow checks every number the formula will read from r, so Compute
// never sees a value outside its domain. Only panel benchmarks are checked:
// shown-only benchmarks (RouterBench, SGD) never enter the arithmetic, and a
// quirk there must not void the edition.
func (m *method) validateRow(r rowWire) error {
	for _, a := range m.areas {
		for _, p := range a.panel {
			key := strconv.Itoa(p.id)
			b, ok := r.Benchmarks[key]
			if !ok {
				continue
			}
			if !unit(b.Raw) {
				return fmt.Errorf("index: model %q benchmark %d raw %v outside [0,1]", r.Engine, p.id, b.Raw)
			}
			if !unit(b.Coverage) {
				return fmt.Errorf("index: model %q benchmark %d coverage %v outside [0,1]", r.Engine, p.id, b.Coverage)
			}
			for _, t := range b.Tracks {
				if !unit(t.Score) {
					return fmt.Errorf("index: model %q benchmark %d track %q score %v outside [0,1]", r.Engine, p.id, t.Track, t.Score)
				}
			}
			if _, lower := m.lower[p.id]; lower {
				// A Brier loss on binary outcomes lies in [0,1].
				if res, ok := r.Results[key]; ok && res.Score != nil && !unit(*res.Score) {
					return fmt.Errorf("index: model %q benchmark %d results score %v outside [0,1]", r.Engine, p.id, *res.Score)
				}
			}
		}
	}
	return nil
}

// === The formula ===

func clip(z float64) float64 { return math.Min(1, math.Max(0, z)) }

// correct is only ever called with chance in [0,1) (parseMethodology).
func correct(s, chance float64) float64 {
	return clip((s - chance) / (1 - chance))
}

// computed is one validated, computed generation: what Load caches and serves.
type computed struct {
	entries      []Entry
	warnings     []string
	generatedUTC string
}

// Compute applies the edition's methodology to the upstream index file. Pure:
// no I/O. The worked example in the methodology file is its fixed test. Rows
// dropped for an out-of-range result are omitted silently here; Load reports
// them as warnings.
func Compute(methodology, index []byte) ([]Entry, error) {
	g, err := compute(methodology, index)
	if err != nil {
		return nil, err
	}
	return g.entries, nil
}

func compute(methodology, index []byte) (*computed, error) {
	m, err := parseMethodology(methodology)
	if err != nil {
		return nil, err
	}
	var w indexWire
	if err := json.Unmarshal(index, &w); err != nil {
		return nil, fmt.Errorf("decode index: %w", err)
	}
	// The index file's name pins the edition too, but a mis-served or
	// republished file would still carry its own panel id; trust that.
	if w.Suite == nil || w.Suite.PanelID != m.panelID {
		got := ""
		if w.Suite != nil {
			got = w.Suite.PanelID
		}
		return nil, fmt.Errorf("index: suite.panel_id %q, want methodology's %q", got, m.panelID)
	}
	rows := w.Models
	if w.Jev != nil {
		rows = append(rows, *w.Jev)
	}
	if len(rows) == 0 {
		return nil, errors.New("index: no models")
	}
	for _, r := range rows {
		if err := m.validateRow(r); err != nil {
			return nil, err
		}
	}
	g := &computed{entries: make([]Entry, 0, len(rows)), generatedUTC: w.GeneratedUTC}
	for _, r := range rows {
		e := m.entry(r)
		// Defence in depth: validation should make this unreachable, but a
		// NaN or 140 plotted on the frontier is worse than a missing point.
		if !finite(e.Index) || e.Index < 0 || e.Index > 100 {
			g.warnings = append(g.warnings, fmt.Sprintf("decision index %s: dropped %q, computed index %v outside 0..100",
				Edition, r.Name, e.Index))
			continue
		}
		g.entries = append(g.entries, e)
	}
	if len(g.entries) == 0 {
		return nil, errors.New("index: no model produced a valid index")
	}
	return g, nil
}

func (m *method) entry(r rowWire) Entry {
	e := Entry{Name: r.Name, Engine: r.Engine, Areas: map[string]float64{}}
	if r.Calibration != nil {
		e.ECE = r.Calibration.ECE
	}
	if r.Latency != nil {
		e.MedianMs, e.P95Ms = r.Latency.Median, r.Latency.P95
	}
	var index float64
	for _, a := range m.areas {
		var num, den float64
		for _, p := range a.panel {
			k, scored := m.benchmark(p.id, r)
			if scored {
				e.Benchmarks++
			}
			num += p.gold * k
			den += p.gold
		}
		e.Areas[a.id] = num / den // den > 0: non-empty panel, gold > 0
		index += a.weight * e.Areas[a.id]
	}
	e.Index = 100 * index
	return e
}

// benchmark returns one chance-corrected score k in [0,1] and whether the row
// has a score for it at all. Unscored benchmarks return (0, false): they count
// as wrong, never as absent (steps[0]).
func (m *method) benchmark(id int, r rowWire) (float64, bool) {
	key := strconv.Itoa(id)
	b, ok := r.Benchmarks[key]
	if !ok || b.Coverage <= 0 {
		return 0, false
	}
	if rule, ok := m.lower[id]; ok {
		res, ok := r.Results[key]
		if !ok || res.Score == nil {
			return 0, false
		}
		return clip((rule.baseline-*res.Score)/(rule.baseline-rule.best)) * b.Coverage, true
	}
	if tracks, ok := m.tracks[id]; ok {
		// GUARD: a multi-track benchmark is scored from its tracks ONLY, never
		// from scalar raw, even when every track is missing. steps[1] averages
		// the in-index tracks equally and steps[0] counts an unanswered one as
		// wrong, so a missing track contributes 0 and stays in the mean. The
		// old scalar fallback scored a trackless row's raw 0.8 as 80.
		got := map[string]float64{}
		for _, t := range b.Tracks {
			got[t.Track] = t.Score
		}
		var sum float64
		for _, t := range tracks {
			// Rows name a track by id ("GSM8K-4choice") or by label ("A · English").
			s, ok := got[t.Track]
			if !ok {
				s, ok = got[t.Label]
			}
			if !ok {
				continue // unanswered: k = 0
			}
			// Track scores are taken as unadjusted and scaled by the
			// benchmark's answered share. Every v0.2.1 row has coverage 1 on
			// its multi-track benchmarks, so the edition data cannot tell
			// this apart from pre-adjusted tracks; revisit on an edition bump.
			sum += correct(s*b.Coverage, t.Random)
		}
		return sum / float64(len(tracks)), true
	}
	return correct(b.Raw, m.chance[id]), true
}
