package context

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Options configures context building
type Options struct {
	Files       []string
	MaxSize     int64
	IgnoreStdin bool
}

// Source describes a context source
type Source struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error,omitempty"`
}

// Context holds the assembled context
type Context struct {
	Content    string   `json:"-"`
	Sources    []Source `json:"sources"`
	TotalBytes int64    `json:"total_bytes"`
}

// Build assembles context from stdin and files
func Build(opts Options) (*Context, error) {
	ctx := &Context{}
	var parts []string
	var totalSize int64

	// Check if stdin has data (not a terminal)
	if !opts.IgnoreStdin && hasStdinData() {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}

		if len(data) > 0 {
			content, truncated := truncateIfNeeded(data, opts.MaxSize-totalSize)
			parts = append(parts, formatFile("stdin", content))
			ctx.Sources = append(ctx.Sources, Source{
				Name:      "stdin",
				SizeBytes: int64(len(data)),
				Truncated: truncated,
			})
			totalSize += int64(len(content))
		}
	}

	// Process files
	for _, path := range opts.Files {
		data, err := os.ReadFile(path)
		if err != nil {
			ctx.Sources = append(ctx.Sources, Source{
				Name:  path,
				Error: err.Error(),
			})
			continue
		}

		// Check if we have room
		remaining := opts.MaxSize - totalSize
		if remaining <= 0 {
			ctx.Sources = append(ctx.Sources, Source{
				Name:  path,
				Error: "context size limit reached",
			})
			continue
		}

		content, truncated := truncateIfNeeded(data, remaining)
		parts = append(parts, formatFile(path, content))
		ctx.Sources = append(ctx.Sources, Source{
			Name:      path,
			SizeBytes: int64(len(data)),
			Truncated: truncated,
		})
		totalSize += int64(len(content))

		// Warn about large files to stderr
		if int64(len(data)) > 51200 { // 50KB
			fmt.Fprintf(os.Stderr, "Warning: %s is %d bytes (may affect response quality)\n", path, len(data))
		}
	}

	ctx.TotalBytes = totalSize

	// Build context block
	if len(parts) > 0 {
		ctx.Content = "<context>\n" + strings.Join(parts, "\n") + "\n</context>"
	}

	return ctx, nil
}

// hasStdinData checks if stdin has data to read
func hasStdinData() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// formatFile creates an XML file block
func formatFile(name string, content string) string {
	return fmt.Sprintf(`<file name="%s" size="%d">
%s
</file>`, name, len(content), content)
}

// truncateIfNeeded truncates content if it exceeds max size
func truncateIfNeeded(data []byte, maxSize int64) (string, bool) {
	if maxSize <= 0 {
		maxSize = 102400 // 100KB default
	}

	if int64(len(data)) <= maxSize {
		return string(data), false
	}

	// Truncate at a reasonable boundary
	truncated := data[:maxSize]
	remaining := int64(len(data)) - maxSize

	return string(truncated) + fmt.Sprintf("\n[truncated: %d more bytes]", remaining), true
}
