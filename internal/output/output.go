package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/context"
	"github.com/0xDarkMatter/conclave-cli/internal/judge"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// Options configures output formatting
type Options struct {
	JSON     bool
	Verbose  bool
	Brief    bool
	Quiet    bool
	Raw      bool   // sentinel-separated provider blocks only, for piping
	Template string // Custom template name
	Blind    bool
	Timeout  int
}

// Result holds all data for output
type Result struct {
	Query     string
	Context   *context.Context
	Providers []string
	JudgeName string
	Responses []providers.Response
	Verdict   *judge.Verdict
	Blind     bool
	Timeout   int
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
	if f.opts.Raw {
		return f.renderRaw(r)
	}
	if f.opts.JSON {
		return f.renderJSON(r)
	}
	if f.opts.Quiet {
		return f.renderQuiet(r)
	}
	if f.opts.Brief {
		return f.renderBrief(r)
	}

	// Use Lipgloss-styled output
	return f.renderStyledOutput(r)
}

// renderRaw emits sentinel-separated provider blocks suitable for downstream
// parsing. No header art, no judge, no styling. Errors are emitted as blocks
// with status=error so the consumer can detect them.
//
// Format:
//
//	===PROVIDER:openai MODEL:gpt-5.5 STATUS:success===
//	<response body>
//	===PROVIDER:claude MODEL:claude-opus-4-5 STATUS:error===
//	<error message>
//	===END===
func (f *Formatter) renderRaw(r Result) error {
	for _, resp := range r.Responses {
		fmt.Printf("===PROVIDER:%s MODEL:%s STATUS:%s===\n", resp.Provider, resp.Model, resp.Status)
		if resp.Status == "success" {
			fmt.Println(resp.Response)
		} else {
			fmt.Println(resp.Error)
		}
	}
	fmt.Println("===END===")
	return nil
}

// buildTemplateData converts Result to TemplateData
func buildTemplateData(r Result) TemplateData {
	data := TemplateData{
		Timestamp: time.Now().Format("02-01-2006 15:04"),
		Query:     r.Query,
		Providers: r.Providers,
		JudgeName: r.JudgeName,
		Blind:     r.Blind,
		Timeout:   r.Timeout,
	}

	// Build provider list with display names
	var maxDuration time.Duration
	for _, resp := range r.Responses {
		data.ProviderList = append(data.ProviderList, ProviderInfo{
			Name:        resp.Provider,
			Model:       resp.Model,
			DisplayName: providers.DisplayName(resp.Provider, resp.Model),
		})
		if resp.Duration > maxDuration {
			maxDuration = resp.Duration
		}
	}

	// Judge display name (use judge name with first response's model pattern if available)
	if r.JudgeName != "" {
		// Find the judge's model from responses if it participated
		judgeModel := ""
		for _, resp := range r.Responses {
			if resp.Provider == r.JudgeName {
				judgeModel = resp.Model
				break
			}
		}
		if judgeModel == "" {
			judgeModel = "default"
		}
		data.JudgeDisplay = providers.DisplayName(r.JudgeName, judgeModel)
	}

	// Context sources
	if r.Context != nil {
		for _, s := range r.Context.Sources {
			if s.Error == "" {
				data.ContextSources = append(data.ContextSources, SourceInfo{
					Name:      s.Name,
					Size:      formatBytes(s.SizeBytes),
					Truncated: s.Truncated,
				})
			}
		}
		data.TotalBytes = r.Context.TotalBytes

		// Build context summary
		if len(data.ContextSources) > 0 {
			data.ContextSummary = fmt.Sprintf("%d file(s) (%s)",
				len(data.ContextSources), formatBytes(r.Context.TotalBytes))
		}
	}

	// Responses and metrics
	for _, resp := range r.Responses {
		rd := ResponseData{
			Provider: resp.Provider,
			Model:    resp.Model,
			Status:   resp.Status,
			Response: resp.Response,
			Error:    resp.Error,
			Duration: fmt.Sprintf("%.1fs", resp.Duration.Seconds()),
		}

		// Add metrics if available
		if resp.Metrics != nil {
			rd.Metrics = &MetricsData{
				InputTokens:  resp.Metrics.InputTokens,
				OutputTokens: resp.Metrics.OutputTokens,
				CacheTokens:  resp.Metrics.CacheTokens,
				CostUSD:      resp.Metrics.CostUSD,
				HasMetrics:   true,
			}
			// Aggregate totals
			data.TotalInputTokens += resp.Metrics.InputTokens
			data.TotalOutputTokens += resp.Metrics.OutputTokens
			data.TotalTokens += resp.Metrics.InputTokens + resp.Metrics.OutputTokens
			data.TotalCostUSD += resp.Metrics.CostUSD
			data.HasMetrics = true
		}

		data.Responses = append(data.Responses, rd)
		data.Timings = append(data.Timings, TimingData{
			Provider: resp.Provider,
			Duration: fmt.Sprintf("%.1fs", resp.Duration.Seconds()),
		})
	}

	// Calculate total time
	if r.Verdict != nil {
		maxDuration += r.Verdict.JudgeDuration
	}
	data.TotalTime = fmt.Sprintf("%.1fs", maxDuration.Seconds())

	// Verdict
	if r.Verdict != nil {
		data.Verdict = &VerdictData{
			Result:          r.Verdict.Result,
			Confidence:      r.Verdict.Confidence,
			Reasoning:       r.Verdict.Reasoning,
			Agreements:      r.Verdict.Agreements,
			Disagreements:   r.Verdict.Disagreements,
			Recommendations: r.Verdict.Recommendations,
		}
		data.Timings = append(data.Timings, TimingData{
			Provider: "judge",
			Duration: fmt.Sprintf("%.1fs", r.Verdict.JudgeDuration.Seconds()),
		})
	}

	// Build header panel
	data.HeaderPanel = buildHeaderPanel(data)

	return data
}

// buildHeaderPanel creates a formatted two-column header
func buildHeaderPanel(data TemplateData) string {
	const width = 72
	const colWidth = 34

	var sb strings.Builder

	// Top border with title and timestamp
	sb.WriteString("╭" + strings.Repeat("─", width) + "╮\n")
	title := fmt.Sprintf("│ CONCLAVE%s%s │\n", strings.Repeat(" ", width-10-len(data.Timestamp)), data.Timestamp)
	sb.WriteString(title)
	sb.WriteString("├" + strings.Repeat("─", colWidth) + "┬" + strings.Repeat("─", width-colWidth-1) + "┤\n")

	// Build left column (providers) and right column (settings)
	var leftCol, rightCol []string

	leftCol = append(leftCol, "Providers:")
	for _, p := range data.ProviderList {
		leftCol = append(leftCol, "  • "+p.DisplayName)
	}

	rightCol = append(rightCol, "Settings:")
	if data.JudgeDisplay != "" {
		rightCol = append(rightCol, "  Judge: "+data.JudgeDisplay)
	}
	if data.Blind {
		rightCol = append(rightCol, "  Blind: Yes")
	} else {
		rightCol = append(rightCol, "  Blind: No")
	}
	rightCol = append(rightCol, fmt.Sprintf("  Timeout: %ds", data.Timeout))
	if data.ContextSummary != "" {
		rightCol = append(rightCol, "  Context: "+data.ContextSummary)
	}

	// Pad columns to same length
	maxRows := len(leftCol)
	if len(rightCol) > maxRows {
		maxRows = len(rightCol)
	}
	for len(leftCol) < maxRows {
		leftCol = append(leftCol, "")
	}
	for len(rightCol) < maxRows {
		rightCol = append(rightCol, "")
	}

	// Render rows
	for i := 0; i < maxRows; i++ {
		left := leftCol[i]
		right := rightCol[i]
		// Truncate if too long
		if len(left) > colWidth-3 {
			left = left[:colWidth-6] + "..."
		}
		if len(right) > width-colWidth-4 {
			right = right[:width-colWidth-7] + "..."
		}
		// Pad to width
		leftPadded := left + strings.Repeat(" ", colWidth-2-len(left))
		rightPadded := right + strings.Repeat(" ", width-colWidth-3-len(right))
		sb.WriteString(fmt.Sprintf("│ %s│ %s│\n", leftPadded, rightPadded))
	}

	// Query row
	sb.WriteString("├" + strings.Repeat("─", colWidth) + "┴" + strings.Repeat("─", width-colWidth-1) + "┤\n")
	query := data.Query
	if len(query) > width-4 {
		query = query[:width-7] + "..."
	}
	sb.WriteString(fmt.Sprintf("│ %s%s │\n", query, strings.Repeat(" ", width-2-len(query))))
	sb.WriteString("╰" + strings.Repeat("─", width) + "╯")

	return sb.String()
}

// Template functions
var templateFuncs = template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"truncate": func(s string, max int) string {
		if len(s) <= max {
			return s
		}
		return s[:max-3] + "..."
	},
	"hasCost": func(cost float64) bool { return cost > 0 },
}

func (f *Formatter) renderTemplate(r Result, templateName string) error {
	tmpl, err := loadTemplateWithFuncs(templateName, templateFuncs)
	if err != nil {
		// Fallback to hardcoded output if template fails
		return f.renderHuman(r)
	}

	data := buildTemplateData(r)
	return tmpl.Execute(os.Stdout, data)
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
		fmt.Printf("CONCLAVE: %s (%s confidence)\n", r.Verdict.Result, r.Verdict.Confidence)
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

	// Show individual responses if verbose, no verdict, or judge parse failure
	if f.opts.Verbose || r.Verdict == nil || r.Verdict.Result == "PARSE_ERROR" {
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
