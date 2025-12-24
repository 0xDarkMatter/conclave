package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/context"
	"github.com/0xDarkMatter/conclave-cli/internal/judge"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// Options configures output formatting
type Options struct {
	JSON    bool
	Verbose bool
	Brief   bool
	Quiet   bool
}

// Result holds all data for output
type Result struct {
	Query     string
	Context   *context.Context
	Providers []string
	JudgeName string
	Responses []providers.Response
	Verdict   *judge.Verdict
}

// Formatter handles output rendering
type Formatter struct {
	opts Options
}

// New creates a new formatter
func New(opts Options) *Formatter {
	return &Formatter{opts: opts}
}

// Render outputs the result
func (f *Formatter) Render(r Result) error {
	if f.opts.JSON {
		return f.renderJSON(r)
	}
	if f.opts.Brief {
		return f.renderBrief(r)
	}
	if f.opts.Quiet {
		return f.renderQuiet(r)
	}
	return f.renderHuman(r)
}

// JSONOutput is the structured JSON output
type JSONOutput struct {
	Version   string `json:"version"`
	Query     string `json:"query"`
	Timestamp string `json:"timestamp"`
	Context   struct {
		Sources    []context.Source `json:"sources"`
		TotalBytes int64            `json:"total_bytes"`
	} `json:"context"`
	Execution struct {
		Providers      []string `json:"providers"`
		Judge          string   `json:"judge"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	} `json:"execution"`
	Responses map[string]ResponseJSON `json:"responses"`
	Verdict   *VerdictJSON            `json:"verdict,omitempty"`
	Meta      struct {
		TotalDurationMs int64 `json:"total_duration_ms"`
		JudgeDurationMs int64 `json:"judge_duration_ms,omitempty"`
	} `json:"meta"`
}

type ResponseJSON struct {
	Status     string `json:"status"`
	Model      string `json:"model"`
	Response   string `json:"response,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

type VerdictJSON struct {
	Result          string   `json:"result"`
	Confidence      string   `json:"confidence"`
	Reasoning       string   `json:"reasoning"`
	Agreements      []string `json:"agreements"`
	Disagreements   []string `json:"disagreements"`
	Recommendations []string `json:"recommendations"`
}

func (f *Formatter) renderJSON(r Result) error {
	out := JSONOutput{
		Version:   "1.0",
		Query:     r.Query,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	if r.Context != nil {
		out.Context.Sources = r.Context.Sources
		out.Context.TotalBytes = r.Context.TotalBytes
	}

	out.Execution.Providers = r.Providers
	out.Execution.Judge = r.JudgeName

	out.Responses = make(map[string]ResponseJSON)
	var maxDuration time.Duration
	for _, resp := range r.Responses {
		out.Responses[resp.Provider] = ResponseJSON{
			Status:     resp.Status,
			Model:      resp.Model,
			Response:   resp.Response,
			Error:      resp.Error,
			DurationMs: resp.Duration.Milliseconds(),
		}
		if resp.Duration > maxDuration {
			maxDuration = resp.Duration
		}
	}

	if r.Verdict != nil {
		out.Verdict = &VerdictJSON{
			Result:          r.Verdict.Result,
			Confidence:      r.Verdict.Confidence,
			Reasoning:       r.Verdict.Reasoning,
			Agreements:      r.Verdict.Agreements,
			Disagreements:   r.Verdict.Disagreements,
			Recommendations: r.Verdict.Recommendations,
		}
		out.Meta.JudgeDurationMs = r.Verdict.JudgeDuration.Milliseconds()
	}

	out.Meta.TotalDurationMs = maxDuration.Milliseconds()
	if r.Verdict != nil {
		out.Meta.TotalDurationMs += r.Verdict.JudgeDuration.Milliseconds()
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func (f *Formatter) renderBrief(r Result) error {
	if r.Verdict != nil {
		// Short one-liner
		rec := ""
		if len(r.Verdict.Recommendations) > 0 {
			rec = " " + strings.Join(r.Verdict.Recommendations[:min(2, len(r.Verdict.Recommendations))], ", ")
		}
		fmt.Printf("%s (%s): %s%s\n",
			r.Verdict.Result,
			r.Verdict.Confidence,
			truncate(r.Verdict.Reasoning, 100),
			rec)
	} else if len(r.Responses) == 1 && r.Responses[0].Status == "success" {
		// Single provider, no verdict
		fmt.Println(truncate(r.Responses[0].Response, 500))
	} else {
		fmt.Println("No verdict available")
	}
	return nil
}

func (f *Formatter) renderQuiet(r Result) error {
	if r.Verdict != nil {
		fmt.Println(r.Verdict.Result)
	} else if len(r.Responses) == 1 && r.Responses[0].Status == "success" {
		fmt.Println(r.Responses[0].Response)
	}
	return nil
}

func (f *Formatter) renderHuman(r Result) error {
	// Header
	if r.Verdict != nil {
		fmt.Println(strings.Repeat("=", 65))
		fmt.Printf("CONCLAVE VERDICT: %s (%s confidence)\n", r.Verdict.Result, r.Verdict.Confidence)
		fmt.Println(strings.Repeat("=", 65))
	} else {
		fmt.Println(strings.Repeat("=", 65))
		fmt.Println("CONCLAVE RESULTS")
		fmt.Println(strings.Repeat("=", 65))
	}
	fmt.Println()

	// Query info
	fmt.Printf("Query: %s\n", truncate(r.Query, 60))
	if r.Context != nil && len(r.Context.Sources) > 0 {
		var sources []string
		for _, s := range r.Context.Sources {
			if s.Error == "" {
				sources = append(sources, fmt.Sprintf("%s (%s)", s.Name, formatBytes(s.SizeBytes)))
			}
		}
		if len(sources) > 0 {
			fmt.Printf("Context: %s\n", strings.Join(sources, ", "))
		}
	}
	fmt.Printf("Experts: %s\n", strings.Join(r.Providers, ", "))
	if r.Verdict != nil {
		fmt.Printf("Judge: %s\n", r.JudgeName)
	}
	fmt.Println()

	// Verdict details
	if r.Verdict != nil {
		fmt.Println("REASONING:")
		fmt.Println(wrapText(r.Verdict.Reasoning, 65))
		fmt.Println()

		if len(r.Verdict.Agreements) > 0 {
			fmt.Println("AGREEMENTS:")
			for _, a := range r.Verdict.Agreements {
				fmt.Printf("  - %s\n", a)
			}
			fmt.Println()
		}

		if len(r.Verdict.Disagreements) > 0 {
			fmt.Println("DISAGREEMENTS:")
			for _, d := range r.Verdict.Disagreements {
				fmt.Printf("  - %s\n", d)
			}
			fmt.Println()
		}

		if len(r.Verdict.Recommendations) > 0 {
			fmt.Println("RECOMMENDATIONS:")
			for i, rec := range r.Verdict.Recommendations {
				fmt.Printf("  %d. %s\n", i+1, rec)
			}
			fmt.Println()
		}
	}

	// Show individual responses if verbose or no verdict
	if f.opts.Verbose || r.Verdict == nil {
		for _, resp := range r.Responses {
			fmt.Println(strings.Repeat("-", 65))
			fmt.Printf("%s (%s) - %s\n", resp.Provider, resp.Model, resp.Status)
			fmt.Println(strings.Repeat("-", 65))
			if resp.Status == "success" {
				fmt.Println(resp.Response)
			} else {
				fmt.Printf("Error: %s\n", resp.Error)
			}
			fmt.Println()
		}
	}

	// Timing footer
	fmt.Println(strings.Repeat("-", 65))
	var timings []string
	for _, resp := range r.Responses {
		timings = append(timings, fmt.Sprintf("%s: %.1fs", resp.Provider, resp.Duration.Seconds()))
	}
	if r.Verdict != nil {
		timings = append(timings, fmt.Sprintf("judge: %.1fs", r.Verdict.JudgeDuration.Seconds()))
	}
	fmt.Printf("Completed | %s\n", strings.Join(timings, ", "))

	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func wrapText(s string, width int) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}

	var lines []string
	line := words[0]

	for _, word := range words[1:] {
		if len(line)+1+len(word) <= width {
			line += " " + word
		} else {
			lines = append(lines, line)
			line = word
		}
	}
	lines = append(lines, line)

	return strings.Join(lines, "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
