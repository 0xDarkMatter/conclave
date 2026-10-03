// Point assembly (ADR-018): turns the two source packages' loaded types into
// Results. Builders are pure — no I/O, no score is ever invented (a missing
// external score stays nil), and kinds are never mixed within one Result.
// Reachability is the sources' problem: a nil feed/board degrades to a Source
// carrying an Error and zero points (ADR-009's advisory, nil-safe rule).
// Frontier marking is deliberately NOT done here — call Pareto on the
// Result's Points to order and mark them.
package frontier

import (
	"fmt"
	"strings"

	"github.com/0xDarkMatter/conclave-cli/internal/benchmarks/decisionindex"
	"github.com/0xDarkMatter/conclave-cli/internal/benchmarks/openrouter"
	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
)

// Source names used as Point.ScoreSource values, so a point always names the
// dataset its score came from.
const (
	aaSourceName    = "Artificial Analysis via OpenRouter"
	evalsSourceName = "OpenRouter evals"
	boardSourceName = "Decision Index"
)

// BuildChat assembles the chat frontier from OpenRouter's benchmarks feed:
// one Point per Artificial Analysis row, scored on the chosen axis, costed on
// the requested basis.
func BuildChat(feed *openrouter.Feed, axis Axis, basis CostBasis) Result {
	res := Result{Kind: KindChat, Axis: axis, CostBasis: basis}
	if feed == nil {
		res.Sources = []Source{{
			Name:  aaSourceName,
			URL:   openrouter.BenchmarksURL,
			Error: "OpenRouter benchmarks feed unavailable",
		}}
		return res
	}

	// Per-task means are looked up per slug, so the evals are folded once
	// instead of rescanned for every AA row.
	taskCost := map[string]float64{}
	if basis == CostPerTask {
		sums := map[string]float64{}
		counts := map[string]int{}
		for _, ev := range feed.Evals {
			if ev.AvgCostPerTask == nil {
				continue // unvalued row: excluded from the mean, never counted as 0
			}
			sums[ev.Slug] += *ev.AvgCostPerTask
			counts[ev.Slug]++
		}
		for slug, n := range counts {
			taskCost[slug] = sums[slug] / float64(n)
		}
	}

	for _, row := range feed.AA {
		p := Point{ID: row.Slug, Name: row.Name, Kind: KindChat, CostBasis: basis}
		if s := aaScore(row, axis); s != nil {
			p.Score = s
			p.ScoreSource = aaSourceName
		}
		switch basis {
		case CostBlendPerM:
			// Blend = (3*in + out)/4, the 3:1 input:output weighting. Both
			// halves must be present: pricing half a blend would fabricate a
			// price the catalog never stated.
			if row.InputPerM != nil && row.OutputPerM != nil {
				v := (3**row.InputPerM + *row.OutputPerM) / 4
				p.Cost = &v
			}
		case CostInputPerM:
			p.Cost = row.InputPerM
		case CostPerTask:
			// Mean AvgCostPerTask over the slug's eval rows. CAVEAT: this
			// mixes benchmark types — gpqa_diamond, aime and friends have
			// different task lengths, so the mean averages their per-task
			// costs across types; it is OpenRouter's own cross-benchmark
			// basis, not a like-for-like unit.
			if c, ok := taskCost[row.Slug]; ok {
				p.Cost = &c
			}
		}
		res.Points = append(res.Points, p)
	}

	res.Sources = append(res.Sources, Source{
		Name:     aaSourceName,
		URL:      openrouter.BenchmarksURL,
		AsOf:     feed.AAAsOf,
		Citation: feed.Citation,
	})
	if basis == CostPerTask {
		res.Sources = append(res.Sources, Source{
			Name: evalsSourceName,
			URL:  openrouter.BenchmarksURL,
			AsOf: feed.EvalsAsOf,
		})
	}
	return res
}

// aaScore picks the row's value for the axis. An unknown or decision axis
// yields nil for every row: the chat builder never guesses a column.
func aaScore(row openrouter.AAScore, axis Axis) *float64 {
	switch axis {
	case AxisIntelligence:
		return row.Intelligence
	case AxisCoding:
		return row.Coding
	case AxisAgentic:
		return row.Agentic
	default:
		return nil
	}
}

// BuildDecision assembles the decision frontier from the recomputed board,
// OpenRouter's decision-model catalog and the Conclave decider price table.
// Board entries are scored; catalog models missing from the board are added
// unscored so they still appear (never estimated — ADR-018).
func BuildDecision(board *decisionindex.Board, models []openrouter.DecisionModel, table []pricing.DeciderPrice, basis CostBasis) Result {
	res := Result{Kind: KindDecision, Axis: AxisDecision, CostBasis: basis}
	if board == nil {
		res.Sources = []Source{{
			Name:  boardSourceName,
			URL:   decisionindex.UpstreamBase + decisionindex.IndexFile,
			Error: "Decision Index board unavailable",
		}}
		return res
	}
	res.Sources = append(res.Sources, Source{
		Name:    boardSourceName,
		URL:     decisionindex.UpstreamBase + decisionindex.IndexFile,
		Edition: board.Edition,
		AsOf:    board.GeneratedUTC,
		Warning: boardWarning(board),
	})

	bySlug := map[string]openrouter.DecisionModel{}
	onBoard := map[string]bool{}
	for _, m := range models {
		// "~" alias rows duplicate their target's slug; keeping them would
		// double-list one model under two ids.
		if m.AliasTarget != "" {
			continue
		}
		bySlug[m.Slug] = m
	}

	for _, e := range board.Entries {
		m, _ := mapBoard(e.Name)
		p := Point{
			Name:         e.Name,
			Kind:         KindDecision,
			ECE:          e.ECE,
			LatencyMs:    e.MedianMs,
			SelfReported: e.SelfReported,
			CostBasis:    basis,
		}
		// The board always computes an Index; that IS the external score.
		idx := e.Index
		p.Score = &idx
		p.ScoreSource = boardSourceName
		switch {
		case m.Slug != "":
			p.ID = m.Slug
			onBoard[m.Slug] = true
		case m.Decider != "":
			p.ID = m.Decider
		default:
			// Unmapped: displayed under the board name, unpriceable (ADR-018
			// accepts this rather than guessing a mapping).
			p.ID = e.Name
		}
		if basis == CostInputPerM {
			// GUARD: decision models are priced ONLY on input_per_m. No
			// external source publishes a per-call cost for them (per-call
			// token counts differ up to 13x between vendors — ADR-018), and
			// no blend exists without an output price. Any other basis
			// leaves the point unpriced rather than mislabelling a number.
			//
			// Slug price first, table second: a catalog miss falls back to
			// the decider row (same list price family), and a decider-only
			// model (Clef) prices from the table alone.
			if m.Slug != "" {
				if mm, ok := bySlug[m.Slug]; ok {
					c := mm.InputPerM
					p.Cost = &c
				}
			}
			if p.Cost == nil && m.Decider != "" {
				p.Cost = deciderTableCost(table, m.Decider)
			}
		}
		res.Points = append(res.Points, p)
	}

	for _, m := range models {
		if m.AliasTarget != "" || onBoard[m.Slug] {
			continue
		}
		p := Point{ID: m.Slug, Name: m.Name, Kind: KindDecision, CostBasis: basis}
		if basis == CostInputPerM {
			c := m.InputPerM
			p.Cost = &c
		}
		res.Points = append(res.Points, p)
	}
	return res
}

// deciderTableCost finds the Conclave decider row's input price; nil when the
// decider is absent from the table (unpriced, never an error).
func deciderTableCost(table []pricing.DeciderPrice, decider string) *float64 {
	for i := range table {
		if table[i].Decider == decider {
			v := table[i].InPerM
			return &v
		}
	}
	return nil
}

// boardWarning renders the board's health onto its Source: the board's own
// warnings, the cross-check's mismatches, and — when Check is nil because the
// mirror was unreachable — a note that the cross-check never ran. ADR-018
// makes every one of these visible rather than silent.
func boardWarning(b *decisionindex.Board) string {
	var ws []string
	ws = append(ws, b.Warnings...)
	if b.Check == nil {
		ws = append(ws, "Decision Index mirror cross-check did not run")
	} else {
		for _, name := range b.Check.Mismatch {
			ws = append(ws, fmt.Sprintf("mirror disagrees on %s (|delta| > %.2f)", name, decisionindex.Tolerance))
		}
	}
	return strings.Join(ws, "; ")
}
