package output

import (
	"github.com/charmbracelet/lipgloss"
)

// Adaptive colors - cool tones (blues, cyans, teals)
var (
	colorPrimary   = lipgloss.AdaptiveColor{Light: "27", Dark: "75"}   // Blue
	colorSecondary = lipgloss.AdaptiveColor{Light: "30", Dark: "80"}   // Cyan
	colorSuccess   = lipgloss.AdaptiveColor{Light: "29", Dark: "48"}   // Teal green
	colorWarning   = lipgloss.AdaptiveColor{Light: "136", Dark: "222"} // Muted gold
	colorError     = lipgloss.AdaptiveColor{Light: "124", Dark: "167"} // Muted rose
	colorMuted     = lipgloss.AdaptiveColor{Light: "244", Dark: "246"} // Cool gray
	colorHighlight = lipgloss.AdaptiveColor{Light: "33", Dark: "117"}  // Sky blue
	colorText      = lipgloss.AdaptiveColor{Light: "235", Dark: "252"} // Main text
	colorSubtle    = lipgloss.AdaptiveColor{Light: "241", Dark: "250"} // Subtle text
)

// Box styles
var (
	// Main verdict box
	verdictBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(0, 1).
			MarginTop(1)

	// Header inside verdict box
	verdictHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary).
				MarginBottom(1)

	// Confidence badge
	confidenceHighStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("0")).
				Background(colorSuccess).
				Padding(0, 1)

	confidenceMediumStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("0")).
				Background(colorWarning).
				Padding(0, 1)

	confidenceLowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("0")).
				Background(colorError).
				Padding(0, 1)

	// Section headers
	sectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorSecondary).
				MarginTop(1).
				MarginBottom(0)

	// List items
	listItemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	bulletStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Provider response box
	providerBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(colorMuted).
				Padding(0, 1).
				MarginTop(1)

	providerHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorHighlight)

	providerModelStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Italic(true)

	// Status badges
	statusSuccessStyle = lipgloss.NewStyle().
				Foreground(colorSuccess)

	statusErrorStyle = lipgloss.NewStyle().
				Foreground(colorError)

	// Footer
	footerStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			MarginTop(1).
			Italic(true)

	// Timing/metrics
	metricsStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Query display
	queryStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true).
			PaddingLeft(2)

	// Info panel (header)
	infoPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(0, 1)

	infoLabelStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	infoValueStyle = lipgloss.NewStyle().
			Foreground(colorText)

	// Title
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			Background(lipgloss.Color("235")).
			Padding(0, 2).
			MarginBottom(1)
)

// getConfidenceStyle returns the appropriate style for a confidence level
func getConfidenceStyle(confidence string) lipgloss.Style {
	switch confidence {
	case "high":
		return confidenceHighStyle
	case "medium":
		return confidenceMediumStyle
	case "low":
		return confidenceLowStyle
	default:
		return confidenceMediumStyle
	}
}
