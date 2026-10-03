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
	Benchmarks []struct {
		ID     int         `json:"id"`
		Tracks []trackWire `json:"tracks"`
	} `json:"benchmarks"`
	Index struct {
		Areas []struct {
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
	GeneratedUTC string    `json:"generated_utc"`
	Models       []rowWire `json:"models"`
	Jev          *rowWire  `json:"jev"` // the closed model sits outside models[]
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
	Benchmarks map[string]struct {
		Raw      float64 `json:"raw"`
		Coverage float64 `json:"coverage"`
		Tracks   []struct {
			Track string  `json:"track"`
			Score float64 `json:"score"`
		} `json:"tracks"`
	} `json:"benchmarks"`
	// results carries the metric in its own units; for lower-is-better
	// benchmarks that is the Brier loss the conversion needs (raw holds
	// upstream's already-converted value, which we recompute rather than trust).
	Results map[string]struct {
		Score *float64 `json:"score"`
	} `json:"results"`
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
	areas  []area
	chance map[int]float64
	lower  map[int]lowerRule
	tracks map[int][]trackWire // in-index tracks only, for multi-track benchmarks
}

func parseMethodology(b []byte) (*method, error) {
	var w methodologyWire
	if err := json.Unmarshal(b, &w); err != nil {
		return nil, fmt.Errorf("decode methodology: %w", err)
	}
	m := &method{chance: map[int]float64{}, lower: map[int]lowerRule{}, tracks: map[int][]trackWire{}}
	for _, c := range w.Index.ChanceLevels {
		m.chance[c.ID] = c.Chance
	}
	for _, l := range w.Index.LowerRules {
		if l.Baseline == l.Best {
			return nil, fmt.Errorf("methodology: lower rule %d has baseline == best", l.ID)
		}
		m.lower[l.ID] = lowerRule{l.Baseline, l.Best}
	}
	for _, bm := range w.Benchmarks {
		var in []trackWire
		headline := false
		for _, t := range bm.Tracks {
			headline = headline || t.Headline
			if t.InIndex {
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
		// "games" is folded into knowledge with weight 0 and an empty panel.
		if a.Weight == 0 || len(a.Panel) == 0 {
			continue
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
			ar.panel = append(ar.panel, panelBench{id: p.ID, gold: g})
		}
		m.areas = append(m.areas, ar)
		sum += a.Weight
	}
	if len(m.areas) == 0 {
		return nil, errors.New("methodology: no weighted areas")
	}
	// Published weights are rounded to 4 dp; a larger gap means a format change.
	if math.Abs(sum-1) > 0.001 {
		return nil, fmt.Errorf("methodology: area weights sum to %.4f, not 1", sum)
	}
	return m, nil
}

// === The formula ===

func clip(z float64) float64 { return math.Min(1, math.Max(0, z)) }

func correct(s, chance float64) float64 {
	if chance >= 1 {
		return 0
	}
	return clip((s - chance) / (1 - chance))
}

// Compute applies the edition's methodology to the upstream index file. Pure:
// no I/O. The worked example in the methodology file is its fixed test.
func Compute(methodology, index []byte) ([]Entry, error) {
	m, err := parseMethodology(methodology)
	if err != nil {
		return nil, err
	}
	var w indexWire
	if err := json.Unmarshal(index, &w); err != nil {
		return nil, fmt.Errorf("decode index: %w", err)
	}
	rows := w.Models
	if w.Jev != nil {
		rows = append(rows, *w.Jev)
	}
	if len(rows) == 0 {
		return nil, errors.New("index: no models")
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, m.entry(r))
	}
	return out, nil
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
		e.Areas[a.id] = num / den
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
	if tracks, ok := m.tracks[id]; ok && len(b.Tracks) > 0 {
		got := map[string]float64{}
		for _, t := range b.Tracks {
			got[t.Track] = t.Score
		}
		var sum float64
		for _, t := range tracks {
			// Rows name a track by id ("GSM8K-4choice") or by label ("A · English").
			s, ok := got[t.Track]
			if !ok {
				s = got[t.Label]
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
