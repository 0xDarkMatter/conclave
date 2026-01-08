package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// renderStyledOutput renders beautiful Lipgloss-styled output
func (f *Formatter) renderStyledOutput(r Result) error {
	var sections []string

	// Header panel with metadata
	headerPanel := renderHeaderPanel(r)
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

	// Provider responses (verbose mode or no verdict)
	if f.opts.Verbose || r.Verdict == nil {
		for _, resp := range r.Responses {
			sections = append(sections, renderProviderResponse(resp.Provider, resp.Model, resp.Status, resp.Response, resp.Error))
		}
	}

	// Footer with timing
	footer := renderFooter(r)
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
		Foreground(lipgloss.Color("255")).
		Render(result)

	header := fmt.Sprintf("%s %s", resultLine, confBadge)

	// Reasoning text (wrapped nicely)
	reasoningWrapped := lipgloss.NewStyle().
		Width(68).
		Foreground(lipgloss.Color("250")).
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

// renderProviderResponse creates a provider response box
func renderProviderResponse(provider, model, status, response, errMsg string) string {
	// Header line
	provName := providerHeaderStyle.Render(provider)
	modelName := providerModelStyle.Render(model)

	var statusBadge string
	if status == "success" {
		statusBadge = statusSuccessStyle.Render("✓")
	} else {
		statusBadge = statusErrorStyle.Render("✗")
	}

	header := fmt.Sprintf("%s %s %s", statusBadge, provName, modelName)

	// Content
	var content string
	if status == "success" {
		// Wrap long responses
		content = lipgloss.NewStyle().
			Width(68).
			Foreground(lipgloss.Color("250")).
			Render(response)
	} else {
		content = statusErrorStyle.Render("Error: " + errMsg)
	}

	boxContent := header + "\n\n" + content
	return providerBoxStyle.Width(72).Render(boxContent)
}

// renderHeaderPanel creates the metadata header
func renderHeaderPanel(r Result) string {
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
	queryRow := infoLabelStyle.Render("Query:") + " " + lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("250")).Render(query)

	content := strings.Join([]string{titleRow, "", providersRow, judgeRow, queryRow}, "\n")

	return infoPanelStyle.Width(72).Render(content)
}

// renderFooter creates the timing footer
func renderFooter(r Result) string {
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

	return footerStyle.Render(strings.Join(parts, " │ "))
}
