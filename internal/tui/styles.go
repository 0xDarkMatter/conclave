package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Colors matching the original progress display
var (
	ColorCyan   = lipgloss.Color("6")  // ANSI cyan
	ColorYellow = lipgloss.Color("3")  // ANSI yellow
	ColorGreen  = lipgloss.Color("2")  // ANSI green
	ColorRed    = lipgloss.Color("1")  // ANSI red
	ColorGray   = lipgloss.Color("8")  // ANSI bright black (gray)
)

// Styles for various UI elements
var (
	// Header style for phase announcements
	HeaderStyle = lipgloss.NewStyle().
			Bold(true)

	// Synthesis verb style (cyan like original)
	SynthVerbStyle = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true)

	// Success indicator
	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorGreen)

	// Error indicator
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorRed)

	// Pending indicator (dimmed)
	PendingStyle = lipgloss.NewStyle().
			Foreground(ColorGray)

	// Stats style (duration/tokens)
	StatsStyle = lipgloss.NewStyle().
			Foreground(ColorGray)

	// Tree prefix style
	TreeStyle = lipgloss.NewStyle()
)

// Icons for status display
const (
	IconPending = "○"
	IconSuccess = "✓"
	IconError   = "✗"
)

// Tree structure prefixes
const (
	TreeBranch = "├──"
	TreeLast   = "└──"
	TreeIndent = "  "
)
