package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Status represents a provider's status
type Status int

const (
	StatusPending Status = iota
	StatusRunning
	StatusSuccess
	StatusError
)

// Progress manages the display of operation progress
type Progress struct {
	out       io.Writer
	mu        sync.Mutex
	quiet     bool
	startTime time.Time
}

// New creates a new Progress instance
func New(quiet bool) *Progress {
	return &Progress{
		out:       os.Stderr,
		quiet:     quiet,
		startTime: time.Now(),
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

// Step prints a step within a phase
func (p *Progress) Step(icon, message string) {
	if p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.out, "  %s %s\n", icon, message)
}

// Success prints a success message
func (p *Progress) Success(message string) {
	p.Step("✓", message)
}

// Error prints an error message
func (p *Progress) Error(message string) {
	p.Step("✗", message)
}

// Waiting prints a waiting message
func (p *Progress) Waiting(message string) {
	p.Step("○", message)
}

// ProviderStart marks a provider as starting
func (p *Progress) ProviderStart(name string) {
	if p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.out, "  ▸ %s: querying...\n", name)
}

// ProviderDone marks a provider as complete
func (p *Progress) ProviderDone(name string, duration time.Duration, err error) {
	if p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if err != nil {
		errStr := truncate(err.Error(), 50)
		fmt.Fprintf(p.out, "  ✗ %s: error (%s)\n", name, errStr)
	} else {
		fmt.Fprintf(p.out, "  ✓ %s: done (%.1fs)\n", name, duration.Seconds())
	}
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

// truncate shortens a string if needed
func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
