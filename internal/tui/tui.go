package tui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

// Progress manages the TUI progress display
// It provides the same API as the original progress.Progress for compatibility
type Progress struct {
	program *tea.Program
	model   Model
	out     io.Writer
	mu      sync.Mutex

	quiet   bool
	isTTY   bool
	started bool

	// For non-TTY mode
	providers   []ProviderInfo
	providerMap map[string]int
	startTime   time.Time
	totalTokens int
	doneCount   int

	// Synthesis tracking for non-TTY
	synthStart   time.Time
	synthTokens  int
	synthStarted bool
}

// New creates a new Progress instance
func New(quiet bool) *Progress {
	isTTY := term.IsTerminal(int(os.Stderr.Fd()))

	return &Progress{
		out:         os.Stderr,
		quiet:       quiet,
		isTTY:       isTTY,
		providerMap: make(map[string]int),
		startTime:   time.Now(),
	}
}

// RegisterProviders sets up the providers to track
func (p *Progress) RegisterProviders(providers []ProviderInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.providers = providers
	for i, info := range providers {
		p.providerMap[info.Name] = i
	}
}

// Start begins the progress display
func (p *Progress) Start() {
	if p.quiet || len(p.providers) == 0 {
		return
	}

	p.mu.Lock()
	p.started = true
	p.startTime = time.Now()
	p.mu.Unlock()

	if p.isTTY {
		// Create and run the TUI program
		p.model = NewModel(p.providers)
		p.program = tea.NewProgram(
			p.model,
			tea.WithOutput(p.out),
			tea.WithoutSignalHandler(), // Let the caller handle signals
		)

		// Run in background
		go func() {
			p.program.Run()
		}()
	} else {
		// Non-TTY: print header
		fmt.Fprintf(p.out, "\n▸ Querying %d providers...\n", len(p.providers))
	}
}

// ProviderStart marks a provider as starting
func (p *Progress) ProviderStart(name string) {
	if p.quiet || !p.started {
		return
	}

	if p.isTTY && p.program != nil {
		p.program.Send(ProviderStartMsg{Provider: name})
	}
	// Non-TTY: no output on start
}

// ProviderDone marks a provider as complete
func (p *Progress) ProviderDone(name string, duration time.Duration, tokens int, err error) {
	if p.quiet || !p.started {
		return
	}

	if p.isTTY && p.program != nil {
		p.mu.Lock()
		p.totalTokens += tokens
		p.mu.Unlock()

		p.program.Send(ProviderDoneMsg{
			Provider: name,
			Duration: duration,
			Tokens:   tokens,
			Error:    err,
		})
	} else {
		// Non-TTY: print completion immediately
		p.mu.Lock()
		defer p.mu.Unlock()

		p.totalTokens += tokens
		idx := p.providerMap[name]

		// Count how many are done (including this one)
		p.doneCount++
		prefix := TreeBranch
		if p.doneCount == len(p.providers) {
			prefix = TreeLast
		}

		displayName := ""
		if idx >= 0 && idx < len(p.providers) {
			displayName = p.providers[idx].DisplayName
		}

		if err != nil {
			errStr := truncate(err.Error(), 40)
			fmt.Fprintf(p.out, "%s%s %s %s: %s\n",
				TreeIndent, prefix, IconError, displayName, errStr)
		} else {
			fmt.Fprintf(p.out, "%s%s %s %s %s\n",
				TreeIndent, prefix, IconSuccess, displayName, formatStats(duration, tokens))
		}
	}
}

// Stop stops the progress display
func (p *Progress) Stop() {
	if p.quiet || !p.started {
		return
	}

	// For TTY mode, we don't quit yet - wait for synthesis
	// The program continues running
}

// StartSynthesis begins the synthesis phase
func (p *Progress) StartSynthesis() {
	if p.quiet {
		return
	}

	p.mu.Lock()
	p.synthStart = time.Now()
	p.synthStarted = true
	p.mu.Unlock()

	if p.isTTY && p.program != nil {
		p.program.Send(SynthesisStartMsg{})
	} else {
		fmt.Fprintf(p.out, "\n▸ Synthesizing...\n")
	}
}

// StopSynthesis ends the synthesis phase
func (p *Progress) StopSynthesis(tokens int, err error) {
	if p.quiet || !p.synthStarted {
		return
	}

	p.mu.Lock()
	p.synthTokens = tokens
	p.totalTokens += tokens
	elapsed := time.Since(p.synthStart)
	p.mu.Unlock()

	if p.isTTY && p.program != nil {
		p.program.Send(SynthesisDoneMsg{
			Tokens: tokens,
			Error:  err,
		})
	} else {
		if err != nil {
			errStr := truncate(err.Error(), 50)
			fmt.Fprintf(p.out, "%s Synthesis failed: %s\n", IconError, errStr)
		} else {
			fmt.Fprintf(p.out, "%s Synthesized %s\n", IconSuccess, formatStats(elapsed, tokens))
		}
	}
}

// Phase prints a phase header
func (p *Progress) Phase(message string) {
	if p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.out, "\n▸ %s\n", message)
}

// Success prints a success message
func (p *Progress) Success(message string) {
	if p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.out, "  %s %s\n", IconSuccess, message)
}

// Error prints an error message
func (p *Progress) Error(message string) {
	if p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.out, "  %s %s\n", IconError, message)
}

// Complete prints the final completion message and stops the TUI
func (p *Progress) Complete() {
	if p.quiet {
		return
	}

	p.mu.Lock()
	elapsed := time.Since(p.startTime)
	tokens := p.totalTokens
	p.mu.Unlock()

	if p.isTTY && p.program != nil {
		// Send complete and quit
		p.program.Send(CompleteMsg{TotalTokens: tokens})
		p.program.Wait()
	} else {
		fmt.Fprintf(p.out, "\n%s Complete %s\n\n", IconSuccess, formatStats(elapsed, tokens))
	}
}

// Quit forcefully stops the TUI program
func (p *Progress) Quit() {
	if p.program != nil {
		p.program.Send(QuitMsg{})
		p.program.Wait()
	}
}
