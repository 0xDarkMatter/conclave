package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// Spinner frames for animation
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Status represents a provider's status
type Status int

const (
	StatusPending Status = iota
	StatusRunning
	StatusSuccess
	StatusError
)

// ProviderInfo holds display info for a provider
type ProviderInfo struct {
	Name        string // short name like "claude"
	DisplayName string // full name like "Anthropic Claude Opus 4.5"
}

// providerState tracks the current state of a provider
type providerState struct {
	info      ProviderInfo
	status    Status
	startTime time.Time
	duration  time.Duration
	tokens    int
	err       error
}

// Progress manages the display of operation progress
type Progress struct {
	out         io.Writer
	mu          sync.Mutex
	quiet       bool
	isTTY       bool
	startTime   time.Time
	providers   []providerState
	providerMap map[string]int // name -> index
	spinnerIdx  int
	stopSpinner chan struct{}
	spinnerDone chan struct{}
	started     bool

	// Synthesis phase
	synthStart      time.Time
	synthStop       chan struct{}
	synthDone       chan struct{}
	synthInProgress bool
}

// New creates a new Progress instance
func New(quiet bool) *Progress {
	// Check if stderr is a terminal
	isTTY := term.IsTerminal(int(os.Stderr.Fd()))

	return &Progress{
		out:         os.Stderr,
		quiet:       quiet,
		isTTY:       isTTY,
		startTime:   time.Now(),
		providerMap: make(map[string]int),
	}
}

// RegisterProviders sets up the providers to track
func (p *Progress) RegisterProviders(providers []ProviderInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.providers = make([]providerState, len(providers))
	for i, info := range providers {
		p.providers[i] = providerState{
			info:   info,
			status: StatusPending,
		}
		p.providerMap[info.Name] = i
	}
}

// Start begins the progress display with spinner
func (p *Progress) Start() {
	if p.quiet || len(p.providers) == 0 {
		return
	}

	p.mu.Lock()
	p.started = true

	// Print header
	fmt.Fprintf(p.out, "\n▸ Querying %d providers...\n\n", len(p.providers))

	if p.isTTY {
		// In TTY mode, print initial state for each provider (will be updated in-place)
		for _, ps := range p.providers {
			fmt.Fprintf(p.out, "  ○ %s\n", ps.info.DisplayName)
		}
	}
	p.mu.Unlock()

	// Only use spinner animation if we're in a TTY
	if p.isTTY {
		p.stopSpinner = make(chan struct{})
		p.spinnerDone = make(chan struct{})
		go p.runSpinner()
	}
}

// runSpinner updates the spinner animation
func (p *Progress) runSpinner() {
	defer close(p.spinnerDone)

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopSpinner:
			return
		case <-ticker.C:
			p.mu.Lock()
			p.spinnerIdx = (p.spinnerIdx + 1) % len(spinnerFrames)
			p.redraw()
			p.mu.Unlock()
		}
	}
}

// redraw updates all provider lines (must be called with lock held)
func (p *Progress) redraw() {
	if len(p.providers) == 0 {
		return
	}

	// Move cursor up to first provider line
	fmt.Fprintf(p.out, "\033[%dA", len(p.providers))

	for _, ps := range p.providers {
		// Clear line and move to start
		fmt.Fprintf(p.out, "\033[2K\r")

		switch ps.status {
		case StatusPending:
			fmt.Fprintf(p.out, "  ○ %s", ps.info.DisplayName)
		case StatusRunning:
			elapsed := time.Since(ps.startTime)
			spinner := spinnerFrames[p.spinnerIdx]
			fmt.Fprintf(p.out, "  %s %s %s", spinner, ps.info.DisplayName, formatElapsed(elapsed))
		case StatusSuccess:
			fmt.Fprintf(p.out, "  ✓ %s %s", ps.info.DisplayName, formatStats(ps.duration, ps.tokens))
		case StatusError:
			errStr := truncate(ps.err.Error(), 40)
			fmt.Fprintf(p.out, "  ✗ %s: %s", ps.info.DisplayName, errStr)
		}

		// Move to next line
		fmt.Fprintf(p.out, "\n")
	}
}

// ProviderStart marks a provider as starting
func (p *Progress) ProviderStart(name string) {
	if p.quiet {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if idx, ok := p.providerMap[name]; ok {
		p.providers[idx].status = StatusRunning
		p.providers[idx].startTime = time.Now()
	}
}

// ProviderDone marks a provider as complete
func (p *Progress) ProviderDone(name string, duration time.Duration, tokens int, err error) {
	if p.quiet {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if idx, ok := p.providerMap[name]; ok {
		if err != nil {
			p.providers[idx].status = StatusError
			p.providers[idx].err = err
		} else {
			p.providers[idx].status = StatusSuccess
		}
		p.providers[idx].duration = duration
		p.providers[idx].tokens = tokens

		// In non-TTY mode, print completion immediately
		if !p.isTTY {
			ps := p.providers[idx]
			if ps.status == StatusSuccess {
				fmt.Fprintf(p.out, "  ✓ %s %s\n", ps.info.DisplayName, formatStats(ps.duration, ps.tokens))
			} else {
				errStr := truncate(ps.err.Error(), 40)
				fmt.Fprintf(p.out, "  ✗ %s: %s\n", ps.info.DisplayName, errStr)
			}
		}
	}
}

// Stop stops the spinner and does final redraw
func (p *Progress) Stop() {
	if p.quiet || !p.started {
		return
	}

	// Only stop spinner if it was started (TTY mode)
	if p.isTTY && p.stopSpinner != nil {
		close(p.stopSpinner)
		<-p.spinnerDone

		// Final redraw to ensure all states are shown
		p.mu.Lock()
		if len(p.providers) > 0 {
			p.redraw()
		}
		p.mu.Unlock()
	}
}

// StartSynthesis begins the synthesis phase with spinner
func (p *Progress) StartSynthesis() {
	if p.quiet {
		return
	}

	p.mu.Lock()
	p.synthStart = time.Now()
	p.synthInProgress = true

	if p.isTTY {
		fmt.Fprintf(p.out, "\n▸ Synthesizing... %s", spinnerFrames[0])
	} else {
		fmt.Fprintf(p.out, "\n▸ Synthesizing...\n")
	}
	p.mu.Unlock()

	if p.isTTY {
		p.synthStop = make(chan struct{})
		p.synthDone = make(chan struct{})
		go p.runSynthSpinner()
	}
}

// runSynthSpinner animates the synthesis spinner
func (p *Progress) runSynthSpinner() {
	defer close(p.synthDone)

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	idx := 0
	for {
		select {
		case <-p.synthStop:
			return
		case <-ticker.C:
			idx = (idx + 1) % len(spinnerFrames)
			elapsed := time.Since(p.synthStart)
			p.mu.Lock()
			// Move to start of line, clear, and rewrite
			fmt.Fprintf(p.out, "\r\033[2K▸ Synthesizing... %s (%.1fs)", spinnerFrames[idx], elapsed.Seconds())
			p.mu.Unlock()
		}
	}
}

// StopSynthesis ends the synthesis phase
func (p *Progress) StopSynthesis(err error) {
	if p.quiet || !p.synthInProgress {
		return
	}

	if p.isTTY && p.synthStop != nil {
		close(p.synthStop)
		<-p.synthDone
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	elapsed := time.Since(p.synthStart)
	if p.isTTY {
		fmt.Fprintf(p.out, "\r\033[2K") // Clear the spinner line
	}

	if err != nil {
		errStr := truncate(err.Error(), 50)
		fmt.Fprintf(p.out, "✗ Synthesis failed: %s\n", errStr)
	} else {
		fmt.Fprintf(p.out, "✓ Synthesized (%.1fs)\n", elapsed.Seconds())
	}
	p.synthInProgress = false
}

// Phase prints a phase header (for other phases)
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
	fmt.Fprintf(p.out, "  ✓ %s\n", message)
}

// Error prints an error message
func (p *Progress) Error(message string) {
	if p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.out, "  ✗ %s\n", message)
}

// Complete prints the final completion message
func (p *Progress) Complete() {
	if p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	elapsed := time.Since(p.startTime)
	fmt.Fprintf(p.out, "\n✓ Complete (%.1fs)\n\n", elapsed.Seconds())
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
