package batch

import (
	"bufio"
	"os"
	"path/filepath"
	"sync"
)

// Checkpoint tracks processed items for resume capability
type Checkpoint struct {
	path      string
	processed map[string]bool
	mu        sync.RWMutex
	file      *os.File
}

// NewCheckpoint creates a new checkpoint tracker
func NewCheckpoint(outputPath string) *Checkpoint {
	return &Checkpoint{
		path:      outputPath + ".checkpoint",
		processed: make(map[string]bool),
	}
}

// Load reads existing checkpoint data
func (c *Checkpoint) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if checkpoint file exists
	if _, err := os.Stat(c.path); os.IsNotExist(err) {
		return nil
	}

	file, err := os.Open(c.path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		id := scanner.Text()
		if id != "" {
			c.processed[id] = true
		}
	}

	return scanner.Err()
}

// IsProcessed checks if an item has been processed
func (c *Checkpoint) IsProcessed(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.processed[id]
}

// MarkProcessed records an item as processed
func (c *Checkpoint) MarkProcessed(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.processed[id] {
		return nil // Already marked
	}

	// Open file for appending if not already open
	if c.file == nil {
		// Ensure directory exists
		dir := filepath.Dir(c.path)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
		}

		f, err := os.OpenFile(c.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		c.file = f
	}

	// Write ID to checkpoint file
	if _, err := c.file.WriteString(id + "\n"); err != nil {
		return err
	}
	if err := c.file.Sync(); err != nil {
		return err
	}

	c.processed[id] = true
	return nil
}

// ProcessedCount returns the number of processed items
func (c *Checkpoint) ProcessedCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.processed)
}

// Close closes the checkpoint file
func (c *Checkpoint) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.file != nil {
		err := c.file.Close()
		c.file = nil
		return err
	}
	return nil
}

// Clear removes the checkpoint file (for fresh starts)
func (c *Checkpoint) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.file != nil {
		c.file.Close()
		c.file = nil
	}

	c.processed = make(map[string]bool)
	return os.Remove(c.path)
}
