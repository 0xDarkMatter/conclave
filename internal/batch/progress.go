package batch

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Progress displays a progress bar for batch processing
type Progress struct {
	total     int
	completed int
	started   time.Time
	writer    io.Writer
	mu        sync.Mutex
	ticker    *time.Ticker
	done      chan struct{}
	running   bool
}

// NewProgress creates a new progress display
func NewProgress(total int, writer io.Writer) *Progress {
	return &Progress{
		total:  total,
		writer: writer,
		done:   make(chan struct{}),
	}
}

// Start begins the progress display
func (p *Progress) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.started = time.Now()
	p.running = true

	// Update display at 1 Hz
	p.ticker = time.NewTicker(time.Second)
	go func() {
		for {
			select {
			case <-p.ticker.C:
				p.render()
			case <-p.done:
				return
			}
		}
	}()

	// Initial render
	p.renderLocked()
}

// Increment marks another item as completed
func (p *Progress) Increment() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.completed++
}

// Stop stops the progress display
func (p *Progress) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return
	}

	p.running = false
	if p.ticker != nil {
		p.ticker.Stop()
	}
	close(p.done)

	// Clear the progress line
	fmt.Fprint(p.writer, "\r\033[K")
}

// render displays the current progress
func (p *Progress) render() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.renderLocked()
}

func (p *Progress) renderLocked() {
	if !p.running || p.total == 0 {
		return
	}

	// Calculate percentage
	pct := float64(p.completed) / float64(p.total) * 100

	// Calculate ETA
	elapsed := time.Since(p.started)
	var eta string
	if p.completed > 0 {
		remaining := float64(p.total-p.completed) * elapsed.Seconds() / float64(p.completed)
		eta = formatDuration(time.Duration(remaining) * time.Second)
	} else {
		eta = "calculating..."
	}

	// Build progress bar
	barWidth := 30
	filled := int(float64(barWidth) * float64(p.completed) / float64(p.total))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	// Clear line and print progress
	fmt.Fprintf(p.writer, "\r\033[K[%s] %d/%d (%.0f%%) ETA: %s",
		bar, p.completed, p.total, pct, eta)
}

// formatDuration formats a duration as a human-readable string
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s > 0 {
			return fmt.Sprintf("%dm%ds", m, s)
		}
		return fmt.Sprintf("%dm", m)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}
