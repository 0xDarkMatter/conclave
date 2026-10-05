// HTML report for a frontier Result (ADR-018): `conclave models --frontier
// --html FILE`. Charts are inline SVG built here in Go; report.html.tmpl is
// the page around them, embedded so the binary carries it.
//
// GUARD: the report is ONE self-contained file with every asset inline (CSS,
// SVG, the few lines of JS) and NO external request of any kind: no CDN, no
// webfont, no remote image. It must open offline and from file://, and a
// report someone mails around must not phone home. Splitting the CSS/JS out,
// or "upgrading" the charts to a JS chart library, breaks exactly that. The
// only URLs in the page are the sources' own, as plain <a href> anchors
// inside the sources block (pinned by TestHTMLReportHasNoExternalReferences).
//
// Invariants:
//   - Deterministic for a given Result: no clock, no map iteration, fixed
//     float formatting, so a golden file can pin it (TestHTMLReportGolden).
//   - Never encode by colour alone: frontier markers are filled, dominated
//     ones hollow, self-reported ones are diamonds; the table is the
//     accessible equivalent of every chart.
//   - Never Arial, never native dialogs (user rule; pinned by tests).
//   - Every external string reaches the page escaped: html/template for the
//     page, html.EscapeString for the SVG built here.
package frontier

import (
	_ "embed"
	"fmt"
	"html"
	"html/template"
	"io"
	"math"
	"strconv"
	"strings"
)

//go:embed report.html.tmpl
var reportTemplate string

var reportTmpl = template.Must(template.New("report").Parse(reportTemplate))

// === Page data ===

type reportRow struct {
	Name, ID, Score, Cost, Basis, Latency, ECE string
	// Sort keys: numbers as text, "" when unknown (the JS sorts unknown last).
	ScoreKey, CostKey, LatencyKey, ECEKey string
	SelfReported, OnFrontier              bool
}

type reportData struct {
	Title, Subtitle string
	CostLabel       string
	BasisNote       string
	PriceChart      template.HTML
	LatencyChart    template.HTML // decision models only
	Rows            []reportRow
	Unscored        []string
	Sources         []Source
	ZeroCostNote    string
	FreeDaily       []freeDailyNote // one per distinct allocation, first-seen order
}

// freeDailyNote is one free daily allocation (Point.FreeDaily) and the models
// it applies to, comma-joined in Pareto order (ADR-019).
type freeDailyNote struct {
	Allocation, Models string
}

// RenderHTML writes the self-contained report. It re-runs Pareto itself.
func RenderHTML(w io.Writer, r Result) error {
	pts := Pareto(r.Points)
	d := reportData{
		Title:     kindLabel(r.Kind) + " price-performance frontier",
		Subtitle:  fmt.Sprintf("Score axis: %s. Cost: %s.", axisLabel(r.Axis), CostBasisLabel(r.CostBasis)),
		CostLabel: CostBasisLabel(r.CostBasis),
		BasisNote: basisNote(r),
		Sources:   r.Sources,
	}
	zero := 0
	freeAt := map[string]int{} // allocation -> index in d.FreeDaily; never ranged over
	for _, p := range pts {
		if p.Score == nil {
			d.Unscored = append(d.Unscored, p.Name)
		}
		if p.FreeDaily != "" {
			if i, ok := freeAt[p.FreeDaily]; ok {
				d.FreeDaily[i].Models += ", " + p.Name
			} else {
				freeAt[p.FreeDaily] = len(d.FreeDaily)
				d.FreeDaily = append(d.FreeDaily, freeDailyNote{Allocation: p.FreeDaily, Models: p.Name})
			}
		}
		if p.Score != nil && p.Cost != nil && *p.Cost <= 0 {
			zero++
		}
		d.Rows = append(d.Rows, reportRow{
			Name: p.Name, ID: p.ID,
			Score: FormatScore(r.Kind, p.Score), Cost: FormatCost(p.Cost),
			Basis: string(p.CostBasis), Latency: FormatLatency(p.LatencyMs), ECE: FormatECE(p.ECE),
			ScoreKey: numKey(p.Score), CostKey: numKey(p.Cost), LatencyKey: numKey(p.LatencyMs), ECEKey: numKey(p.ECE),
			SelfReported: p.SelfReported, OnFrontier: p.OnFrontier,
		})
	}
	if zero > 0 {
		// A log axis has no place for $0; hiding these silently would lose
		// models, so the page says where they went.
		d.ZeroCostNote = fmt.Sprintf("%d scored model(s) list a cost of $0 and cannot sit on a log axis; they are in the table.", zero)
	}
	d.PriceChart = template.HTML(scatterSVG(pts, chartSpec{
		id:     "price",
		title:  "Score vs cost",
		xLabel: CostBasisLabel(r.CostBasis) + ", log scale",
		yLabel: axisLabel(r.Axis),
		x:      func(p Point) *float64 { return p.Cost },
		xFmt:   func(v float64) string { return FormatCost(&v) },
		xTick:  func(v float64) string { return "$" + decade(v) },
		step:   true,
		kind:   r.Kind,
	}))
	if r.Kind == KindDecision {
		d.LatencyChart = template.HTML(scatterSVG(pts, chartSpec{
			id:     "latency",
			title:  "Score vs median latency",
			xLabel: "median latency per call, log scale",
			yLabel: axisLabel(r.Axis),
			x:      func(p Point) *float64 { return p.LatencyMs },
			xFmt:   func(v float64) string { return FormatLatency(&v) },
			xTick: func(v float64) string {
				if v >= 1000 {
					return decade(v/1000) + " s"
				}
				return decade(v) + " ms"
			},
			kind: r.Kind,
		}))
	}
	return reportTmpl.Execute(w, d)
}

func numKey(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%g", *v)
}

func axisLabel(a Axis) string {
	switch a {
	case AxisIntelligence:
		return "Artificial Analysis Intelligence Index"
	case AxisCoding:
		return "Artificial Analysis Coding Index"
	case AxisAgentic:
		return "Artificial Analysis Agentic Index"
	case AxisDecision:
		return "Decision Index"
	default:
		return string(a)
	}
}

// basisNote is the plain-language caveat for the cost basis. The decision
// wording is ADR-018's: list price per input token is the only published
// basis, and per-call token counts differ by up to 13x between vendors.
func basisNote(r Result) string {
	switch r.CostBasis {
	case CostPerTask:
		return "Cost is OpenRouter's measured average cost per evaluation task, averaged across benchmark types. Task lengths differ by benchmark, so treat it as a relative figure rather than the price of your own workload."
	case CostBlendPerM:
		return "Cost is the list price in USD per million tokens, blended 3:1 input to output. It is a price-sheet figure: what a task costs also depends on how many tokens a model spends on it, which list price does not show."
	case CostInputPerM:
		if r.Kind == KindDecision {
			return "Cost is the list price in USD per million input tokens, the only basis any source publishes for decision models. It can mislead: the tokens one decision call uses depend on each vendor's prompt format and tokenizer and differ by up to 13x between vendors, so a lower per-token price can still mean a dearer decision."
		}
		return "Cost is the list price in USD per million input tokens. Output tokens are not counted, so models that answer at length cost more than this suggests."
	default:
		return ""
	}
}

// === SVG scatter ===

// Chart geometry in viewBox units; the SVG scales to its container, so these
// are proportions, not pixels.
const (
	chartW, chartH                  = 720.0, 420.0
	padL, padR, padT, padB          = 64.0, 20.0, 16.0, 56.0
	markerR                         = 5.0
	labelChars                      = 24
	frontierLabelMax                = 12 // more labels than this collide; the table has them all
	plotW, plotH                    = chartW - padL - padR, chartH - padT - padB
	minDecadeSpan, minScoreSpanHalf = 0.5, 5.0
)

type chartSpec struct {
	id, title, xLabel, yLabel string
	x                         func(Point) *float64
	xFmt                      func(float64) string // a point's value, in tooltips
	xTick                     func(float64) string // a power-of-ten gridline label
	step                      bool                 // draw the Pareto step line (price chart only)
	kind                      Kind
}

type plotted struct {
	p    Point
	x, y float64 // viewBox coordinates
}

// scatterSVG draws score (linear y) against a positive x on a log axis.
// Points with no score, no x, or x <= 0 are not drawable and are skipped
// (they stay in the table).
func scatterSVG(pts []Point, c chartSpec) string {
	var in []Point
	for _, p := range pts {
		if x := c.x(p); p.Score != nil && x != nil && *x > 0 {
			in = append(in, p)
		}
	}
	var b strings.Builder
	titleID, descID := c.id+"-title", c.id+"-desc"
	fmt.Fprintf(&b, `<svg class="chart" viewBox="0 0 %g %g" role="img" aria-labelledby="%s %s">`, chartW, chartH, titleID, descID)
	fmt.Fprintf(&b, `<title id="%s">%s</title>`, titleID, esc(c.title))
	fmt.Fprintf(&b, `<desc id="%s">%s</desc>`, descID, esc(chartDesc(in, c)))
	if len(in) == 0 {
		fmt.Fprintf(&b, `<text class="empty" x="%g" y="%g" text-anchor="middle">No model has both a score and a value on this axis.</text></svg>`, chartW/2, chartH/2)
		return b.String()
	}

	// X: log10 domain padded to whole decades for readable ticks.
	lo, hi := math.Inf(1), math.Inf(-1)
	sLo, sHi := math.Inf(1), math.Inf(-1)
	for _, p := range in {
		lx := math.Log10(*c.x(p))
		lo, hi = math.Min(lo, lx), math.Max(hi, lx)
		sLo, sHi = math.Min(sLo, *p.Score), math.Max(sHi, *p.Score)
	}
	if hi-lo < minDecadeSpan {
		mid := (hi + lo) / 2
		lo, hi = mid-minDecadeSpan, mid+minDecadeSpan
	}
	xLo, xHi := math.Floor(lo), math.Ceil(hi)
	if sHi-sLo < 2*minScoreSpanHalf {
		mid := (sHi + sLo) / 2
		sLo, sHi = mid-minScoreSpanHalf, mid+minScoreSpanHalf
	}
	yStep := niceStep((sHi - sLo) / 5)
	yLo, yHi := math.Floor(sLo/yStep)*yStep, math.Ceil(sHi/yStep)*yStep

	px := func(v float64) float64 { return padL + (math.Log10(v)-xLo)/(xHi-xLo)*plotW }
	py := func(s float64) float64 { return padT + (yHi-s)/(yHi-yLo)*plotH }

	// Grid and ticks.
	b.WriteString(`<g class="grid">`)
	for d := xLo; d <= xHi+1e-9; d++ {
		x := px(math.Pow(10, d))
		fmt.Fprintf(&b, `<line x1="%s" y1="%s" x2="%s" y2="%s"/>`, f(x), f(padT), f(x), f(padT+plotH))
		fmt.Fprintf(&b, `<text class="tick" x="%s" y="%s" text-anchor="middle">%s</text>`, f(x), f(padT+plotH+18), esc(c.xTick(math.Pow(10, d))))
	}
	for s := yLo; s <= yHi+1e-9; s += yStep {
		y := py(s)
		fmt.Fprintf(&b, `<line x1="%s" y1="%s" x2="%s" y2="%s"/>`, f(padL), f(y), f(padL+plotW), f(y))
		fmt.Fprintf(&b, `<text class="tick" x="%s" y="%s" text-anchor="end">%s</text>`, f(padL-8), f(y+4), trimFloat(s))
	}
	b.WriteString(`</g>`)
	fmt.Fprintf(&b, `<text class="axis" x="%s" y="%s" text-anchor="middle">%s</text>`, f(padL+plotW/2), f(chartH-10), esc(c.xLabel))
	fmt.Fprintf(&b, `<text class="axis" transform="translate(16 %s) rotate(-90)" text-anchor="middle">%s</text>`, f(padT+plotH/2), esc(c.yLabel))

	var dots []plotted
	for _, p := range in {
		dots = append(dots, plotted{p: p, x: px(*c.x(p)), y: py(*p.Score)})
	}

	// Pareto step line: from each frontier point run flat at its score to the
	// next point's cost, then up — the best score attainable at or below
	// each cost. Pareto already ordered frontier points by ascending cost.
	if c.step {
		var path strings.Builder
		n := 0
		for _, d := range dots {
			if !d.p.OnFrontier {
				continue
			}
			if n == 0 {
				fmt.Fprintf(&path, "M%s %s", f(d.x), f(d.y))
			} else {
				fmt.Fprintf(&path, " H%s V%s", f(d.x), f(d.y))
			}
			n++
		}
		if n > 1 {
			fmt.Fprintf(&b, `<path class="step" d="%s"/>`, path.String())
		}
	}

	// Dominated first so frontier markers paint on top.
	b.WriteString(`<g class="dots">`)
	for pass := 0; pass < 2; pass++ {
		for _, d := range dots {
			if d.p.OnFrontier != (pass == 1) {
				continue
			}
			writeMarker(&b, d, c)
		}
	}
	b.WriteString(`</g>`)

	// Labels for frontier points only, and only while they fit.
	labels := 0
	for _, d := range dots {
		if d.p.OnFrontier {
			labels++
		}
	}
	if labels <= frontierLabelMax {
		b.WriteString(`<g class="labels">`)
		for _, d := range dots {
			if !d.p.OnFrontier {
				continue
			}
			anchor, dx := "start", 8.0
			if d.x > padL+plotW*0.75 {
				anchor, dx = "end", -8
			}
			fmt.Fprintf(&b, `<text x="%s" y="%s" text-anchor="%s">%s</text>`, f(d.x+dx), f(d.y-8), anchor, esc(trimName(d.p.Name, labelChars)))
		}
		b.WriteString(`</g>`)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// writeMarker: circle, or diamond when self-reported; filled on the
// frontier, hollow when dominated. The <title> is the hover tooltip and
// needs no script.
func writeMarker(b *strings.Builder, d plotted, c chartSpec) {
	cls := "dom"
	if d.p.OnFrontier {
		cls = "front"
	}
	if d.p.SelfReported {
		cls += " self"
	}
	fmt.Fprintf(b, `<g class="pt %s">`, cls)
	tip := fmt.Sprintf("%s: score %s, %s", d.p.Name, FormatScore(c.kind, d.p.Score), c.xFmt(*c.x(d.p)))
	if d.p.OnFrontier {
		tip += ", on the frontier"
	}
	if d.p.SelfReported {
		tip += ", self-reported"
	}
	if d.p.FreeDaily != "" {
		tip += ", free daily allocation"
	}
	fmt.Fprintf(b, `<title>%s</title>`, esc(tip))
	if d.p.SelfReported {
		r := markerR * 1.4
		fmt.Fprintf(b, `<path d="M%s %s L%s %s L%s %s L%s %sZ"/>`,
			f(d.x), f(d.y-r), f(d.x+r), f(d.y), f(d.x), f(d.y+r), f(d.x-r), f(d.y))
	} else {
		fmt.Fprintf(b, `<circle cx="%s" cy="%s" r="%g"/>`, f(d.x), f(d.y), markerR)
	}
	b.WriteString(`</g>`)
}

// chartDesc is the screen-reader summary of a chart.
func chartDesc(in []Point, c chartSpec) string {
	var front []string
	for _, p := range in {
		if p.OnFrontier {
			front = append(front, p.Name)
		}
	}
	s := fmt.Sprintf("Scatter of %d models: %s against %s.", len(in), c.yLabel, c.xLabel)
	if len(front) > 0 {
		s += fmt.Sprintf(" %d on the price frontier: %s.", len(front), strings.Join(front, ", "))
	}
	return s + " The table below lists every model."
}

// decade prints a power of ten without float noise or padding: 0.01, 1, 100.
func decade(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// niceStep rounds a raw tick step to 1, 2 or 5 times a power of ten.
func niceStep(raw float64) float64 {
	if raw <= 0 {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 5, 10} {
		if raw <= m*p {
			return m * p
		}
	}
	return 10 * p
}

// f fixes coordinate precision so the golden file is stable across
// platforms (and the file stays small).
func f(v float64) string { return trimFloat(math.Round(v*10) / 10) }

func trimFloat(v float64) string {
	s := fmt.Sprintf("%.1f", v)
	s = strings.TrimSuffix(s, ".0")
	if s == "-0" {
		return "0"
	}
	return s
}

func esc(s string) string { return html.EscapeString(s) }
