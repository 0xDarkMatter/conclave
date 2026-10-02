// Rendering for `conclave decide`: the --json envelope, the human per-question
// table and the -q one-line-per-answer form (docs/PLAN-decision-models.md,
// "--json"). ADR-016 makes this envelope its own top level, never the chat
// panel's output.Result: a decision has typed answers and a consensus block,
// not a free-text response and a verdict.
//
// Invariants:
//   - The envelope is built from data only (BuildEnvelope takes the elapsed
//     time as an argument, never reads a clock), so the golden test pins the
//     exact bytes with a fixed duration.
//   - A cache hit keeps status "success" and is signalled only by
//     `cached: true` (ADR-011, AGENTS Gotcha 11).
//   - meta.total_cost_usd sums PRICED deciders only and is omitted when none
//     is priced: an unpriced decider is unknown cost, never $0 (ADR-009). A
//     cache hit is priced at exactly 0 — the store charged nothing.
//   - Human output mirrors internal/output's palette (styles there are
//     unexported, so the adaptive colours are restated below, same values).
package decide

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
	"github.com/charmbracelet/lipgloss"
)

// Decider statuses. "success" matches the chat panel's spelling so a parser
// shared between the two envelopes reads one vocabulary.
const (
	StatusSuccess = "success"
	StatusError   = "error"
)

// DeciderResult is one decider's entry under the envelope's "deciders" key.
// Answers are the vendor's typed answers re-encoded through providers.Answer,
// i.e. every field the wire contract defines, verbatim in value.
type DeciderResult struct {
	Model      string                      `json:"model,omitempty"`
	Status     string                      `json:"status"`
	DurationMs int64                       `json:"duration_ms"`
	Answers    map[string]providers.Answer `json:"answers,omitempty"`
	Metrics    *providers.Metrics          `json:"metrics,omitempty"`
	Cached     bool                        `json:"cached,omitempty"`
	Error      string                      `json:"error,omitempty"`

	// Priced separates "cost is known to be 0" from "cost is unknown":
	// Metrics.CostUSD is omitempty, so the number alone cannot say which.
	Priced bool `json:"-"`
}

// Meta is the envelope's run summary. DurationMs is the wall time of the
// whole parallel panel, not a sum of decider durations.
type Meta struct {
	TotalCostUSD *float64 `json:"total_cost_usd,omitempty"`
	DurationMs   int64    `json:"duration_ms"`
	Requested    int      `json:"requested"`
	Succeeded    int      `json:"succeeded"`
}

// Envelope is the complete `conclave decide --json` document.
type Envelope struct {
	Deciders  map[string]DeciderResult     `json:"deciders"`
	Consensus map[string]QuestionConsensus `json:"consensus"`
	Meta      Meta                         `json:"meta"`
}

// BuildEnvelope aggregates the per-decider results into the envelope: the
// successful ones feed Consensus, every one counts toward Requested.
func BuildEnvelope(questions map[string]providers.Question, results map[string]DeciderResult, elapsed time.Duration) Envelope {
	decisions := make(map[string]*providers.Decision, len(results))
	var total float64
	priced := false
	for name, r := range results {
		if r.Status != StatusSuccess {
			continue
		}
		decisions[name] = &providers.Decision{Model: r.Model, Answers: r.Answers}
		if r.Priced {
			priced = true
			if r.Metrics != nil {
				total += r.Metrics.CostUSD
			}
		}
	}
	env := Envelope{
		Deciders:  results,
		Consensus: Consensus(questions, decisions, len(results)),
		Meta: Meta{
			DurationMs: elapsed.Milliseconds(),
			Requested:  len(results),
			Succeeded:  len(decisions),
		},
	}
	if priced {
		env.Meta.TotalCostUSD = &total
	}
	return env
}

// RenderJSON writes the envelope indented, matching the chat panel's --json.
func RenderJSON(w io.Writer, env Envelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Vendor text and the "<redacted>" credential marker must reach callers
	// verbatim; this is machine output, never embedded in HTML.
	enc.SetEscapeHTML(false)
	return enc.Encode(env)
}

// RenderQuiet writes one `id=value` line per question, sorted by id. Value is
// the consensus answer: the choice label, the score index, or the noul mean.
// A question no decider answered prints an empty value, so the id set stays
// complete and a `cut -d= -f2` consumer can tell "no answer" from a missing
// line.
func RenderQuiet(w io.Writer, env Envelope) error {
	for _, id := range sortedKeys(env.Consensus) {
		value, _ := consensusValue(env.Consensus[id], nil, false)
		if _, err := fmt.Fprintf(w, "%s=%s\n", id, value); err != nil {
			return err
		}
	}
	return nil
}

// === SECTION: human table ===============================================

// Same adaptive values as internal/output/styles.go; keep the two in step.
var (
	colorSecondary = lipgloss.AdaptiveColor{Light: "30", Dark: "80"}
	colorWarning   = lipgloss.AdaptiveColor{Light: "136", Dark: "222"}
	colorError     = lipgloss.AdaptiveColor{Light: "124", Dark: "167"}
	colorMuted     = lipgloss.AdaptiveColor{Light: "244", Dark: "246"}

	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorSecondary)
	contestedStyle = lipgloss.NewStyle().Bold(true).Foreground(colorWarning)
	errorStyle     = lipgloss.NewStyle().Foreground(colorError)
	mutedStyle     = lipgloss.NewStyle().Foreground(colorMuted)
)

// RenderHuman writes the per-question table: id, consensus answer,
// probability (the noul mean for noul), agreement, contested marker, then
// one column per decider with that decider's own pick. questions supplies the
// score anchors so a score reads "1 Frustrated" rather than a bare index.
func RenderHuman(w io.Writer, env Envelope, questions map[string]providers.Question) error {
	names := sortedKeys(env.Deciders)

	var b strings.Builder
	summary := fmt.Sprintf("Decision panel: %d/%d deciders answered, %s", env.Meta.Succeeded, env.Meta.Requested, formatMs(env.Meta.DurationMs))
	if env.Meta.TotalCostUSD != nil {
		summary += fmt.Sprintf(", $%.6f", *env.Meta.TotalCostUSD)
	}
	b.WriteString(headerStyle.Render(summary) + "\n")
	for _, n := range names {
		r := env.Deciders[n]
		switch {
		case r.Status != StatusSuccess:
			b.WriteString(errorStyle.Render(fmt.Sprintf("  ✗ %s: %s", n, r.Error)) + "\n")
		case r.Cached:
			b.WriteString(mutedStyle.Render(fmt.Sprintf("  ✓ %s (%s, cached)", n, r.Model)) + "\n")
		default:
			b.WriteString(mutedStyle.Render(fmt.Sprintf("  ✓ %s (%s)", n, r.Model)) + "\n")
		}
	}
	b.WriteString("\n")

	header := append([]string{"QUESTION", "ANSWER", "PROB", "AGREE", ""}, names...)
	rows := [][]string{header}
	contested := make([]bool, 1, len(env.Consensus)+1)
	for _, id := range sortedKeys(env.Consensus) {
		qc := env.Consensus[id]
		q := questions[id]
		answer, prob := consensusValue(qc, q.Scale, true)
		if qc.Succeeded == 0 {
			answer, prob = "—", "—"
		}
		agree := "—"
		if qc.Agreement != nil {
			agree = fmt.Sprintf("%.2f", *qc.Agreement)
		}
		marker := ""
		if qc.Contested {
			marker = "contested"
		}
		row := []string{id, answer, prob, agree, marker}
		for _, n := range names {
			row = append(row, deciderPick(env.Deciders[n], id))
		}
		rows = append(rows, row)
		contested = append(contested, qc.Contested)
	}

	widths := make([]int, len(header))
	for _, row := range rows {
		for i, cell := range row {
			if wd := lipgloss.Width(cell); wd > widths[i] {
				widths[i] = wd
			}
		}
	}
	// Pad on the PLAIN text, then style: padding a styled cell would count
	// ANSI escapes as width and skew every column after it.
	for ri, row := range rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			padded := cell + strings.Repeat(" ", widths[i]-lipgloss.Width(cell))
			switch {
			case ri == 0:
				padded = headerStyle.Render(padded)
			case i == 4 && contested[ri]:
				padded = contestedStyle.Render(padded)
			case cell == "—" || cell == "error":
				padded = mutedStyle.Render(padded)
			}
			cells[i] = padded
		}
		b.WriteString("  " + strings.TrimRight(strings.Join(cells, "  "), " ") + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// consensusValue renders the consensus answer and its probability column.
// human selects the table form (score anchor appended, noul as yes/no); the
// -q form keeps the bare machine value. scale is the score anchors, nil for
// the other types.
func consensusValue(qc QuestionConsensus, scale []string, human bool) (answer, prob string) {
	switch qc.Type {
	case providers.QuestionChoice:
		if qc.Choice == "" {
			return "", "—"
		}
		return qc.Choice, fmt.Sprintf("%.2f", qc.Probabilities[qc.Choice])
	case providers.QuestionScore:
		if qc.Score == nil {
			return "", "—"
		}
		idx := strconv.Itoa(*qc.Score)
		answer = idx
		if human && *qc.Score < len(scale) {
			answer = idx + " " + scale[*qc.Score]
		}
		return answer, fmt.Sprintf("%.2f", qc.Probabilities[idx])
	case providers.QuestionNoul:
		if qc.Noul == nil {
			return "", "—"
		}
		if !human {
			return strconv.FormatFloat(*qc.Noul, 'f', -1, 64), ""
		}
		// 0.5 counts as yes, the same boundary consensus.go pins for contested.
		verdict := "no"
		if *qc.Noul >= 0.5 {
			verdict = "yes"
		}
		return verdict, fmt.Sprintf("%.2f", *qc.Noul)
	}
	return "", "—"
}

// deciderPick renders one decider's own answer to question id.
func deciderPick(r DeciderResult, id string) string {
	if r.Status != StatusSuccess {
		return "error"
	}
	a, ok := r.Answers[id]
	if !ok {
		return "—"
	}
	switch a.Type {
	case providers.QuestionChoice:
		if a.Choice != "" {
			return a.Choice
		}
	case providers.QuestionScore:
		if a.Score != nil {
			return strconv.FormatFloat(*a.Score, 'f', -1, 64)
		}
	case providers.QuestionNoul:
		if a.Noul != nil {
			return fmt.Sprintf("%.2f", *a.Noul)
		}
	}
	return "—"
}

func formatMs(ms int64) string {
	return (time.Duration(ms) * time.Millisecond).String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
