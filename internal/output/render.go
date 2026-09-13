package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
	"github.com/charmbracelet/lipgloss"
)

// renderStyledOutput renders beautiful Lipgloss-styled output
func (f *Formatter) renderStyledOutput(r Result, c costs) error {
	var sections []string

	// Header panel with metadata
	headerPanel := renderHeaderPanel(r, c)
	sections = append(sections, headerPanel)

	// Verdict box (if we have one)
	if r.Verdict != nil {
		verdictSection := renderVerdict(r.Verdict.Result, r.Verdict.Confidence, r.Verdict.Reasoning)
		sections = append(sections, verdictSection)

		// Agreements
		if len(r.Verdict.Agreements) > 0 {
			sections = append(sections, renderList("AGREEMENTS", r.Verdict.Agreements, "✓"))
		}

		// Disagreements
		if len(r.Verdict.Disagreements) > 0 {
			sections = append(sections, renderList("DISAGREEMENTS", r.Verdict.Disagreements, "✗"))
		}

		// Recommendations
		if len(r.Verdict.Recommendations) > 0 {
			sections = append(sections, renderNumberedList("RECOMMENDATIONS", r.Verdict.Recommendations))
		}
	}

	// Provider responses: shown in verbose mode, when no verdict exists, or
	// when the judge failed to parse — so the user can still see what the
	// providers actually returned instead of losing their work to a
	// synthesis failure.
	if f.opts.Verbose || r.Verdict == nil || r.Verdict.Result == "PARSE_ERROR" {
		mixed := panelIsMixed(r.Responses)
		for i, resp := range r.Responses {
			var cost *float64
			if i < len(c.byIndex) {
				cost = c.byIndex[i]
			}
			sections = append(sections, renderProviderResponse(resp, cost, mixed))
		}
	}

	// Footer with timing
	footer := renderFooter(r, c)
	sections = append(sections, footer)

	// Print everything
	fmt.Println(strings.Join(sections, "\n"))
	return nil
}

// renderVerdict creates the main verdict display
func renderVerdict(result, confidence, reasoning string) string {
	// Confidence badge
	confStyle := getConfidenceStyle(confidence)
	confBadge := confStyle.Render(" " + strings.ToUpper(confidence) + " ")

	// Result line - bold and prominent
	resultLine := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorText).
		Render(result)

	header := fmt.Sprintf("%s %s", resultLine, confBadge)

	// Reasoning text (wrapped nicely)
	reasoningWrapped := lipgloss.NewStyle().
		Width(68).
		Foreground(colorSubtle).
		MarginTop(1).
		Render(reasoning)

	content := header + "\n" + reasoningWrapped

	return verdictBoxStyle.Width(72).Render(content)
}

// renderList creates a bulleted list section
func renderList(title string, items []string, bullet string) string {
	header := sectionHeaderStyle.Render(title)

	var listItems []string
	bulletStyled := bulletStyle.Render(bullet)
	for _, item := range items {
		listItems = append(listItems, listItemStyle.Render(bulletStyled+" "+item))
	}

	return header + "\n" + strings.Join(listItems, "\n")
}

// renderNumberedList creates a numbered list section
func renderNumberedList(title string, items []string) string {
	header := sectionHeaderStyle.Render(title)

	var listItems []string
	for i, item := range items {
		num := lipgloss.NewStyle().
			Foreground(colorSecondary).
			Bold(true).
			Render(fmt.Sprintf("%d.", i+1))
		listItems = append(listItems, listItemStyle.Render(num+" "+item))
	}

	return header + "\n" + strings.Join(listItems, "\n")
}

// panelIsMixed reports whether the responses ran on more than one transport
// (ADR-012). Only then does the styled view tag each block with its transport:
// in a single-transport run the tag would be the same word on every block, and
// the run's mode is already obvious from the command line.
func panelIsMixed(responses []providers.Response) bool {
	seen := ""
	for _, r := range responses {
		if r.Transport == "" {
			continue
		}
		if seen == "" {
			seen = r.Transport
		} else if r.Transport != seen {
			return true
		}
	}
	return false
}

// renderProviderResponse creates a provider response box. cost is nil when the
// dollar figure is unknown or the leg ran on a subscription CLI — an unknown
// price is shown as nothing, never as $0.00. showTransport adds a "via cli" /
// "via api" tag to the header; callers pass it only for mixed panels.
func renderProviderResponse(resp providers.Response, cost *float64, showTransport bool) string {
	provider, model, status := resp.Provider, resp.Model, resp.Status
	response, errMsg := resp.Response, resp.Error

	// Header line. A slash-routed OpenRouter token is its own model id
	// (ADR-010), so printing both would repeat the slug; show it once.
	provName := providerHeaderStyle.Render(provider)
	modelName := ""
	if model != provider {
		modelName = " " + providerModelStyle.Render(model)
	}

	var statusBadge string
	if status == "success" {
		statusBadge = statusSuccessStyle.Render("✓")
	} else {
		statusBadge = statusErrorStyle.Render("✗")
	}

	header := fmt.Sprintf("%s %s%s", statusBadge, provName, modelName)
	if showTransport && resp.Transport != "" {
		header += "  " + providerModelStyle.Render("via "+resp.Transport)
	}
	if resp.Cached {
		header += "  " + providerModelStyle.Render("(cached)")
	}
	if cost != nil {
		header += "  " + providerModelStyle.Render(pricing.FormatUSD(*cost))
	}

	// Content
	var content string
	if status == "success" {
		// Wrap long responses
		content = lipgloss.NewStyle().
			Width(68).
			Foreground(colorSubtle).
			Render(response)
	} else {
		// Wrap long error messages but never truncate them — auth/HTTP/transport
		// errors are diagnostic and the actionable detail is often near the end.
		content = statusErrorStyle.Copy().Width(68).Render("Error: " + errMsg)
	}

	boxContent := header + "\n\n" + content
	return providerBoxStyle.Width(72).Render(boxContent)
}

// renderHeaderPanel creates the metadata header
func renderHeaderPanel(r Result, c costs) string {
	width := 68 // inner width

	// Title row with timestamp right-aligned
	title := "CONCLAVE"
	ts := time.Now().Format("02 Jan 2006 15:04")
	padding := width - len(title) - len(ts)
	if padding < 1 {
		padding = 1
	}
	titleRow := lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render(title) +
		strings.Repeat(" ", padding) +
		lipgloss.NewStyle().Foreground(colorMuted).Render(ts)

	// Providers
	var providerNames []string
	for _, resp := range r.Responses {
		providerNames = append(providerNames, resp.Provider)
	}
	providersRow := infoLabelStyle.Render("Providers:") + " " + infoValueStyle.Render(strings.Join(providerNames, ", "))

	// Judge
	judgeRow := infoLabelStyle.Render("Judge:") + " " + infoValueStyle.Render(r.JudgeName)

	// Query (truncated if needed)
	query := r.Query
	if len(query) > 55 {
		query = query[:52] + "..."
	}
	queryRow := infoLabelStyle.Render("Query:") + " " + lipgloss.NewStyle().Italic(true).Foreground(colorSubtle).Render(query)

	rows := []string{titleRow, "", providersRow, judgeRow, queryRow}

	// Cost row: API mode only, and only when at least one figure is known.
	// CLI mode is subscription-billed, so it never shows dollars.
	if c.enabled() {
		rows = append(rows, infoLabelStyle.Render("Cost:")+" "+infoValueStyle.Render(c.formatTotal()+" (providers + judge)"))
	}

	content := strings.Join(rows, "\n")

	return infoPanelStyle.Width(72).Render(content)
}

// renderFooter creates the timing footer
func renderFooter(r Result, c costs) string {
	var parts []string

	// Timing info
	var timings []string
	for _, resp := range r.Responses {
		timings = append(timings, fmt.Sprintf("%s: %.1fs", resp.Provider, resp.Duration.Seconds()))
	}
	if r.Verdict != nil {
		timings = append(timings, fmt.Sprintf("judge: %.1fs", r.Verdict.JudgeDuration.Seconds()))
	}

	if len(timings) > 0 {
		parts = append(parts, "Completed in "+strings.Join(timings, ", "))
	}

	// Token metrics if available
	var totalIn, totalOut int
	for _, resp := range r.Responses {
		if resp.Metrics != nil {
			totalIn += resp.Metrics.InputTokens
			totalOut += resp.Metrics.OutputTokens
		}
	}
	if totalIn > 0 || totalOut > 0 {
		parts = append(parts, fmt.Sprintf("Tokens: %d in / %d out", totalIn, totalOut))
	}

	if c.enabled() {
		parts = append(parts, "Cost: "+c.formatTotal())
	}

	return footerStyle.Render(strings.Join(parts, " │ "))
}
