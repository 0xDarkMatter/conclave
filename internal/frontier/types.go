// Package frontier assembles price-performance points from EXTERNAL quality
// sources and finds the Pareto frontier (ADR-018). It never runs a model:
// every score carries the source and snapshot date it came from, and a model
// without an external score is Unscored, never estimated.
//
// This file is the contract the source packages (internal/benchmarks/...),
// the frontier maths and cmd/models.go --frontier are built against. Change
// it first, alone.
package frontier

// Kind separates the two model classes (ADR-016): their quality axes are not
// comparable, so one frontier never mixes them.
type Kind string

const (
	KindChat     Kind = "chat"
	KindDecision Kind = "decision"
)

// Axis names a quality score. Chat axes come from Artificial Analysis via
// OpenRouter; AxisDecision is the recomputed Decision Index.
type Axis string

const (
	AxisIntelligence Axis = "intelligence" // Artificial Analysis Intelligence Index
	AxisCoding       Axis = "coding"       // Artificial Analysis Coding Index
	AxisAgentic      Axis = "agentic"      // Artificial Analysis Agentic Index
	AxisDecision     Axis = "decision"     // Decision Index (edition-pinned)
)

// CostBasis says what a point's Cost means, because the bases are not
// interchangeable and the output must say which one it plots.
type CostBasis string

const (
	CostPerTask   CostBasis = "per_task"    // OpenRouter evals avg_cost_per_task, USD
	CostInputPerM CostBasis = "input_per_m" // list price, USD per million input tokens
	CostBlendPerM CostBasis = "blend_per_m" // list price, (3*input + output)/4 per million
)

// Source is one external dataset feeding a frontier, with its provenance.
type Source struct {
	Name     string `json:"name"`               // e.g. "Artificial Analysis via OpenRouter"
	URL      string `json:"url"`                // where the data was fetched
	AsOf     string `json:"as_of"`              // the source's own snapshot date/time
	Edition  string `json:"edition,omitempty"`  // pinned edition, e.g. "v0.2.1"
	Citation string `json:"citation,omitempty"` // attribution text the source asks for
	Error    string `json:"error,omitempty"`    // set when the source failed; data then absent
	Warning  string `json:"warning,omitempty"`  // e.g. a cross-check disagreement
}

// Point is one model on one axis. Pointers are nil when unknown; nil Score
// means unscored (shown, never plotted on the quality axis).
type Point struct {
	ID           string    `json:"id"`   // OpenRouter slug, or a fixed decider name ("clef")
	Name         string    `json:"name"` // display name
	Kind         Kind      `json:"kind"`
	Score        *float64  `json:"score,omitempty"`
	Cost         *float64  `json:"cost,omitempty"`
	CostBasis    CostBasis `json:"cost_basis,omitempty"`
	LatencyMs    *float64  `json:"latency_ms,omitempty"` // median, where the source publishes it
	ECE          *float64  `json:"ece,omitempty"`        // calibration error, decision models
	SelfReported bool      `json:"self_reported,omitempty"`
	ScoreSource  string    `json:"score_source,omitempty"` // Source.Name the score came from
	OnFrontier   bool      `json:"on_frontier"`
}

// Result is one frontier: a kind, an axis, a cost basis, every point
// (frontier, dominated and unscored) and the sources that fed it.
type Result struct {
	Kind      Kind      `json:"kind"`
	Axis      Axis      `json:"axis"`
	CostBasis CostBasis `json:"cost_basis"`
	Points    []Point   `json:"points"`
	Sources   []Source  `json:"sources"`
}
