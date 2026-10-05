// Terminal rendering of a frontier Result (ADR-018), for `conclave models
// --frontier`. Plain text through text/tabwriter with UPPERCASE section
// headers, matching the rest of `conclave models` (no colour: the output is
// read by people AND piped into files).
//
// Invariants:
//   - Sections, in order: FRONTIER, DOMINATED (top DominatedShown by score,
//     then "and N more"), UNPRICED (scored, no cost on this basis; capped the
//     same way), UNSCORED (names only), SOURCES. UNPRICED exists because a
//     scored model with no price is neither unscored nor dominated, and
//     calling it either would misstate it.
//   - Every score shown is attributed: the SOURCES footer names each source,
//     its as-of date, edition and citation.
//   - COST is always a list price. A free daily allocation (Point.FreeDaily)
//     is a "+" mark and a legend line, never a lower number (ADR-019).
//   - Source errors and warnings are NOT printed here; Warnings returns them
//     so the caller can send them to stderr (Gotcha 5: advisory, never fatal).
//   - Deterministic for a given Result: no clock, no map iteration.
package frontier

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"text/tabwriter"
)

// DominatedShown caps the DOMINATED section; the rest is summarised as
// "and N more" (the HTML table lists them all).
const DominatedShown = 15

// selfReportedMark suffixes a self-reported name in the terminal; the legend
// line under the table explains it whenever one is shown.
const selfReportedMark = " *"

// freeDailyMark suffixes a name whose COST has a free daily allocation in
// front of it (Point.FreeDaily, ADR-019); a legend names each allocation once.
const freeDailyMark = " +"

// buckets splits Pareto-ordered points into the four display groups.
type buckets struct {
	frontier, dominated, unpriced, unscored []Point
}

func split(points []Point) buckets {
	var b buckets
	for _, p := range Pareto(points) {
		switch {
		case p.OnFrontier:
			b.frontier = append(b.frontier, p)
		case p.Score == nil:
			b.unscored = append(b.unscored, p)
		case p.Cost == nil:
			b.unpriced = append(b.unpriced, p)
		default:
			b.dominated = append(b.dominated, p)
		}
	}
	return b
}

// RenderText writes the terminal report. It re-runs Pareto itself, so a
// Result straight from BuildChat/BuildDecision renders correctly.
func RenderText(w io.Writer, r Result) error {
	b := split(r.Points)
	decision := r.Kind == KindDecision
	selfShown := false
	// Allocations in first-shown order, with the names shown under each: a
	// slice plus a map keyed by it, so the legend never depends on map order.
	var freeOrder []string
	freeNames := map[string][]string{}

	fmt.Fprintf(w, "%s frontier: axis %s, cost %s\n", kindLabel(r.Kind), r.Axis, CostBasisLabel(r.CostBasis))
	fmt.Fprintf(w, "Higher score is better; lower cost is better. Scores are external, never estimated (ADR-018).\n")

	// total is the whole bucket's size; pts may be its capped head.
	table := func(title string, pts []Point, total int) {
		fmt.Fprintf(w, "\n%s (%d)\n", title, total)
		if len(pts) == 0 {
			fmt.Fprintf(w, "  (none)\n")
			return
		}
		tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
		if decision {
			fmt.Fprintf(tw, "  MODEL\tSCORE\tCOST\tLATENCY\tECE\n")
		} else {
			fmt.Fprintf(tw, "  MODEL\tSCORE\tCOST\n")
		}
		for _, p := range pts {
			name := p.Name
			if p.SelfReported {
				name += selfReportedMark
				selfShown = true
			}
			if p.FreeDaily != "" {
				name += freeDailyMark
				if _, seen := freeNames[p.FreeDaily]; !seen {
					freeOrder = append(freeOrder, p.FreeDaily)
				}
				freeNames[p.FreeDaily] = append(freeNames[p.FreeDaily], p.Name)
			}
			if decision {
				fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", name, FormatScore(r.Kind, p.Score), FormatCost(p.Cost), FormatLatency(p.LatencyMs), FormatECE(p.ECE))
			} else {
				fmt.Fprintf(tw, "  %s\t%s\t%s\n", name, FormatScore(r.Kind, p.Score), FormatCost(p.Cost))
			}
		}
		tw.Flush()
	}

	table("FRONTIER", b.frontier, len(b.frontier))
	shown := b.dominated
	if len(shown) > DominatedShown {
		shown = shown[:DominatedShown]
	}
	table("DOMINATED", shown, len(b.dominated))
	if more := len(b.dominated) - len(shown); more > 0 {
		fmt.Fprintf(w, "  and %d more\n", more)
	}
	if len(b.unpriced) > 0 {
		// Pareto orders this bucket by name; by score is what a reader scans
		// for. Usually large for decision models: most board names have no
		// price mapping (decidermap.go).
		up := append([]Point(nil), b.unpriced...)
		sort.SliceStable(up, func(i, j int) bool { return *up[i].Score > *up[j].Score })
		more := 0
		if len(up) > DominatedShown {
			up, more = up[:DominatedShown], len(up)-DominatedShown
		}
		table("UNPRICED: scored, no price on this cost basis", up, len(b.unpriced))
		if more > 0 {
			fmt.Fprintf(w, "  and %d more\n", more)
		}
	}
	if selfShown {
		fmt.Fprintf(w, "\n  * self-reported: the vendor's own figure, not recomputed from the upstream source.\n")
	}
	for _, alloc := range freeOrder {
		fmt.Fprintf(w, "\n  + free daily allocation, %s: %s.\n", alloc, strings.Join(freeNames[alloc], ", "))
		fmt.Fprintf(w, "    Calls inside it are billed $0; COST is the metered list price, which applies once it is spent (ADR-019).\n")
	}

	fmt.Fprintf(w, "\nUNSCORED (%d, no external score; never estimated)\n", len(b.unscored))
	if len(b.unscored) == 0 {
		fmt.Fprintf(w, "  (none)\n")
	} else {
		names := make([]string, len(b.unscored))
		for i, p := range b.unscored {
			names[i] = p.Name
		}
		writeWrapped(w, names, 96)
	}

	fmt.Fprintf(w, "\nSOURCES\n")
	for _, s := range r.Sources {
		line := "  " + s.Name
		if s.AsOf != "" {
			line += ", as of " + s.AsOf
		}
		if s.Edition != "" {
			line += ", edition " + s.Edition
		}
		if s.Error != "" {
			line += " (UNAVAILABLE)"
		}
		fmt.Fprintln(w, line)
		if s.URL != "" {
			fmt.Fprintf(w, "    %s\n", s.URL)
		}
		if s.Citation != "" {
			fmt.Fprintf(w, "    %s\n", s.Citation)
		}
	}
	return nil
}

// Warnings returns one line per failed or warned source, for stderr. A
// source can carry both (an error plus a stale-data note).
func Warnings(r Result) []string {
	var out []string
	for _, s := range r.Sources {
		if s.Error != "" {
			out = append(out, fmt.Sprintf("%s unavailable, its models are unscored: %s", s.Name, s.Error))
		}
		if s.Warning != "" {
			out = append(out, fmt.Sprintf("%s: %s", s.Name, s.Warning))
		}
	}
	return out
}

// writeWrapped prints names comma-joined, wrapped at width, indented two.
func writeWrapped(w io.Writer, names []string, width int) {
	line := " "
	for i, n := range names {
		piece := " " + n
		if i < len(names)-1 {
			piece += ","
		}
		if len(line)+len(piece) > width && line != " " {
			fmt.Fprintln(w, line)
			line = " "
		}
		line += piece
	}
	fmt.Fprintln(w, line)
}

// === Shared formatting (terminal and HTML use the same strings) ===

func kindLabel(k Kind) string {
	if k == KindDecision {
		return "Decision-model"
	}
	return "Chat-model"
}

// CostBasisLabel is the human unit for a basis.
func CostBasisLabel(b CostBasis) string {
	switch b {
	case CostPerTask:
		return "USD per task (OpenRouter evals)"
	case CostInputPerM:
		return "USD per 1M input tokens (list price)"
	case CostBlendPerM:
		return "USD per 1M tokens, 3:1 input:output blend (list price)"
	default:
		return string(b)
	}
}

// FormatScore uses two decimals for the Decision Index (the mirror
// cross-check compares at 2 dp) and one for the AA indices.
func FormatScore(k Kind, v *float64) string {
	if v == nil {
		return "-"
	}
	if k == KindDecision {
		return fmt.Sprintf("%.2f", *v)
	}
	return fmt.Sprintf("%.1f", *v)
}

// FormatCost keeps three significant-ish digits across the five decades a
// frontier spans (fractions of a cent per task up to tens of dollars per M).
func FormatCost(v *float64) string {
	if v == nil {
		return "-"
	}
	c := *v
	switch {
	case c == 0:
		return "$0"
	case c < 0.01:
		return fmt.Sprintf("$%.4f", c)
	case c < 1:
		return fmt.Sprintf("$%.3f", c)
	case c < 100:
		return fmt.Sprintf("$%.2f", c)
	default:
		return fmt.Sprintf("$%.0f", c)
	}
}

// FormatLatency renders a median in ms, switching to seconds at 1000 ms.
func FormatLatency(ms *float64) string {
	if ms == nil {
		return "-"
	}
	if *ms >= 1000 {
		return fmt.Sprintf("%.1f s", *ms/1000)
	}
	return fmt.Sprintf("%.0f ms", math.Round(*ms))
}

// FormatECE renders expected calibration error (0 is perfectly calibrated).
func FormatECE(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.3f", *v)
}

// trimName shortens chart labels; the table and the <title> keep the full name.
func trimName(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
