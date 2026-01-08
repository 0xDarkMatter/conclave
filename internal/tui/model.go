package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// Spinner frames for animation (matching original)
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Synthesis verbs - rotated during synthesis phase (from original)
var synthVerbs = []string{
	"Synthesizing",
	"Percolating",
	"Triangulating",
	"Cogitating",
	"Harmonizing",
	"Crystallizing",
	"Ruminating",
	"Deliberating",
	"Distilling",
	"Condensing",
	"Amalgamating",
	"Coalescing",
	"Fermenting",
	"Marinating",
	"Simmering",
	"Brewing",
	"Concocting",
	"Weaving",
	"Untangling",
	"Deciphering",
	"Pondering",
	"Contemplating",
	"Mulling",
	"Calibrating",
	"Recombobulating",
}

// Model is the main TUI model
type Model struct {
	providers   []ProviderState
	providerMap map[string]int // name -> index

	spinner     spinner.Model
	phase       Phase
	startTime   time.Time
	totalTokens int

	// Synthesis state
	synthStart   time.Time
	synthVerbIdx int
	synthSpinner spinner.Model
	synthTokens  int

	// Display state
	width    int
	height   int
	quitting bool
}

// NewModel creates a new Model with the given providers
func NewModel(providers []ProviderInfo) Model {
	// Create spinner with braille frames
	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: spinnerFrames,
		FPS:    time.Second / 12, // ~80ms per frame
	}

	// Synthesis spinner (same style)
	synthS := spinner.New()
	synthS.Spinner = spinner.Spinner{
		Frames: spinnerFrames,
		FPS:    time.Second / 12,
	}

	// Build provider states
	states := make([]ProviderState, len(providers))
	providerMap := make(map[string]int)
	for i, p := range providers {
		states[i] = ProviderState{
			Info:   p,
			Status: StatusPending,
		}
		providerMap[p.Name] = i
	}

	return Model{
		providers:    states,
		providerMap:  providerMap,
		spinner:      s,
		synthSpinner: synthS,
		phase:        PhaseQuerying,
		startTime:    time.Now(),
		synthVerbIdx: rand.Intn(len(synthVerbs)),
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		if m.phase == PhaseQuerying {
			m.spinner, cmd = m.spinner.Update(msg)
		} else if m.phase == PhaseSynthesizing {
			m.synthSpinner, cmd = m.synthSpinner.Update(msg)
		}
		return m, cmd

	case ProviderStartMsg:
		if idx, ok := m.providerMap[msg.Provider]; ok {
			m.providers[idx].Status = StatusRunning
			m.providers[idx].StartTime = time.Now()
		}
		return m, nil

	case ProviderDoneMsg:
		if idx, ok := m.providerMap[msg.Provider]; ok {
			if msg.Error != nil {
				m.providers[idx].Status = StatusError
				m.providers[idx].Error = msg.Error
			} else {
				m.providers[idx].Status = StatusSuccess
			}
			m.providers[idx].Duration = msg.Duration
			m.providers[idx].Tokens = msg.Tokens
			m.totalTokens += msg.Tokens
		}
		return m, nil

	case SynthesisStartMsg:
		m.phase = PhaseSynthesizing
		m.synthStart = time.Now()
		m.synthVerbIdx = rand.Intn(len(synthVerbs))
		return m, tea.Batch(
			m.synthSpinner.Tick,
			verbTickCmd(),
		)

	case VerbTickMsg:
		if m.phase == PhaseSynthesizing {
			m.synthVerbIdx = rand.Intn(len(synthVerbs))
			return m, verbTickCmd()
		}
		return m, nil

	case SynthesisDoneMsg:
		m.synthTokens = msg.Tokens
		m.totalTokens += msg.Tokens
		m.phase = PhaseComplete
		return m, nil

	case CompleteMsg:
		m.phase = PhaseComplete
		m.quitting = true
		return m, tea.Quit

	case QuitMsg:
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

// verbTickCmd returns a command that sends VerbTickMsg every 1.5s
func verbTickCmd() tea.Cmd {
	return tea.Tick(1500*time.Millisecond, func(t time.Time) tea.Msg {
		return VerbTickMsg{}
	})
}

// View renders the model
func (m Model) View() string {
	if m.quitting && m.phase == PhaseComplete {
		return m.viewComplete()
	}

	var b strings.Builder

	// Header
	b.WriteString(fmt.Sprintf("\n▸ Querying %d providers...\n", len(m.providers)))

	// Provider tree
	for i, p := range m.providers {
		prefix := TreeBranch
		if i == len(m.providers)-1 {
			prefix = TreeLast
		}

		line := m.renderProviderLine(p, prefix)
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Synthesis phase
	if m.phase == PhaseSynthesizing {
		b.WriteString("\n")
		verb := synthVerbs[m.synthVerbIdx]
		elapsed := time.Since(m.synthStart)
		b.WriteString(fmt.Sprintf("%s▸ %s...%s %s %s",
			SynthVerbStyle.Render(""),
			SynthVerbStyle.Render(verb),
			"",
			m.synthSpinner.View(),
			StatsStyle.Render(formatElapsed(elapsed)),
		))
	}

	return b.String()
}

// viewComplete renders the final completion state
func (m Model) viewComplete() string {
	var b strings.Builder

	// Header
	b.WriteString(fmt.Sprintf("\n▸ Querying %d providers...\n", len(m.providers)))

	// Provider tree (final state)
	for i, p := range m.providers {
		prefix := TreeBranch
		if i == len(m.providers)-1 {
			prefix = TreeLast
		}

		line := m.renderProviderLine(p, prefix)
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Synthesis complete
	if m.synthTokens > 0 {
		elapsed := time.Since(m.synthStart)
		b.WriteString(fmt.Sprintf("%s Synthesized %s\n",
			SuccessStyle.Render(IconSuccess),
			StatsStyle.Render(formatStats(elapsed, m.synthTokens)),
		))
	}

	// Total
	totalElapsed := time.Since(m.startTime)
	b.WriteString(fmt.Sprintf("\n%s Complete %s\n\n",
		SuccessStyle.Render(IconSuccess),
		StatsStyle.Render(formatStats(totalElapsed, m.totalTokens)),
	))

	return b.String()
}

// renderProviderLine renders a single provider line
func (m Model) renderProviderLine(p ProviderState, prefix string) string {
	switch p.Status {
	case StatusPending:
		return fmt.Sprintf("%s%s %s %s",
			TreeIndent, prefix,
			PendingStyle.Render(IconPending),
			p.Info.DisplayName,
		)
	case StatusRunning:
		elapsed := time.Since(p.StartTime)
		return fmt.Sprintf("%s%s %s %s %s",
			TreeIndent, prefix,
			m.spinner.View(),
			p.Info.DisplayName,
			StatsStyle.Render(formatElapsed(elapsed)),
		)
	case StatusSuccess:
		return fmt.Sprintf("%s%s %s %s %s",
			TreeIndent, prefix,
			SuccessStyle.Render(IconSuccess),
			p.Info.DisplayName,
			StatsStyle.Render(formatStats(p.Duration, p.Tokens)),
		)
	case StatusError:
		errStr := truncate(p.Error.Error(), 40)
		return fmt.Sprintf("%s%s %s %s: %s",
			TreeIndent, prefix,
			ErrorStyle.Render(IconError),
			p.Info.DisplayName,
			ErrorStyle.Render(errStr),
		)
	}
	return ""
}

// formatStats formats duration and tokens as [00.00s / 000000 tokens]
func formatStats(d time.Duration, tokens int) string {
	if tokens > 0 {
		return fmt.Sprintf("[%05.2fs / %06d tokens]", d.Seconds(), tokens)
	}
	return fmt.Sprintf("[%05.2fs]", d.Seconds())
}

// formatElapsed formats elapsed time for spinner display
func formatElapsed(d time.Duration) string {
	return fmt.Sprintf("[%05.2fs]", d.Seconds())
}

// truncate shortens a string if needed
func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
