package output

import (
	"github.com/charmbracelet/lipgloss"
)

// Adaptive colors that work on both light and dark terminals
var (
	colorPrimary   = lipgloss.AdaptiveColor{Light: "21", Dark: "212"}  // Blue
	colorSecondary = lipgloss.AdaptiveColor{Light: "127", Dark: "213"} // Magenta
	colorSuccess   = lipgloss.AdaptiveColor{Light: "22", Dark: "78"}   // Green
	colorWarning   = lipgloss.AdaptiveColor{Light: "208", Dark: "214"} // Orange
	colorError     = lipgloss.AdaptiveColor{Light: "160", Dark: "196"} // Red
	colorMuted     = lipgloss.AdaptiveColor{Light: "240", Dark: "245"} // Gray
	colorHighlight = lipgloss.AdaptiveColor{Light: "136", Dark: "229"} // Yellow
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
