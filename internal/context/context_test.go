package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildWithFiles(t *testing.T) {
	// Create a temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	content := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx, err := Build(Options{
		Files:       []string{testFile},
		MaxSize:     500000,
		IgnoreStdin: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ctx.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(ctx.Sources))
	}

	if ctx.Sources[0].Name != testFile {
		t.Errorf("expected source name %s, got %s", testFile, ctx.Sources[0].Name)
	}

	if ctx.Sources[0].SizeBytes != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), ctx.Sources[0].SizeBytes)
	}

	if !strings.Contains(ctx.Content, content) {
		t.Error("context should contain file content")
	}

	if !strings.Contains(ctx.Content, "<context>") {
		t.Error("context should have XML wrapper")
	}
}

func TestBuildWithMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"a.go": "package a",
		"b.go": "package b",
		"c.go": "package c",
	}

	var paths []string
	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		paths = append(paths, path)
	}

	ctx, err := Build(Options{
		Files:       paths,
		MaxSize:     500000,
		IgnoreStdin: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ctx.Sources) != 3 {
		t.Errorf("expected 3 sources, got %d", len(ctx.Sources))
	}

	// Check all files are included
	for _, content := range files {
		if !strings.Contains(ctx.Content, content) {
			t.Errorf("context should contain %q", content)
		}
	}
}

func TestBuildWithTruncation(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "large.txt")

	// Create content larger than max
	largeContent := strings.Repeat("x", 1000)
	if err := os.WriteFile(testFile, []byte(largeContent), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx, err := Build(Options{
		Files:       []string{testFile},
		MaxSize:     500, // Small max to trigger truncation
		IgnoreStdin: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ctx.Sources[0].Truncated {
		t.Error("source should be marked as truncated")
	}

	if !strings.Contains(ctx.Content, "[truncated:") {
		t.Error("context should contain truncation marker")
	}
}

func TestBuildWithMissingFile(t *testing.T) {
	ctx, err := Build(Options{
		Files:       []string{"/nonexistent/file.txt"},
		MaxSize:     500000,
		IgnoreStdin: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ctx.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(ctx.Sources))
	}

	if ctx.Sources[0].Error == "" {
		t.Error("source should have error for missing file")
	}
}

func TestBuildContextFormat(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	content := "package main"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx, err := Build(Options{
		Files:       []string{testFile},
		MaxSize:     500000,
		IgnoreStdin: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check XML structure
	if !strings.HasPrefix(ctx.Content, "<context>") {
		t.Error("context should start with <context>")
	}
	if !strings.HasSuffix(ctx.Content, "</context>") {
		t.Error("context should end with </context>")
	}
	if !strings.Contains(ctx.Content, "<file name=") {
		t.Error("context should contain <file> elements")
	}
	if !strings.Contains(ctx.Content, "size=") {
		t.Error("file element should have size attribute")
	}
}

func TestBuildEmpty(t *testing.T) {
	ctx, err := Build(Options{
		Files:       nil,
		MaxSize:     500000,
		IgnoreStdin: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ctx.Content != "" {
		t.Error("context should be empty with no files")
	}

	if len(ctx.Sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(ctx.Sources))
	}
}

func TestTruncateIfNeeded(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		maxSize     int64
		wantTrunc   bool
		wantContain string
	}{
		{
			name:      "no truncation needed",
			data:      []byte("short"),
			maxSize:   100,
			wantTrunc: false,
		},
		{
			name:        "truncation needed",
			data:        []byte(strings.Repeat("x", 200)),
			maxSize:     50,
			wantTrunc:   true,
			wantContain: "[truncated:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, truncated := truncateIfNeeded(tt.data, tt.maxSize)

			if truncated != tt.wantTrunc {
				t.Errorf("truncated = %v, want %v", truncated, tt.wantTrunc)
			}

			if tt.wantContain != "" && !strings.Contains(result, tt.wantContain) {
				t.Errorf("result should contain %q", tt.wantContain)
			}
		})
	}
}

func TestFormatFile(t *testing.T) {
	result := formatFile("test.go", "package main")

	if !strings.Contains(result, `name="test.go"`) {
		t.Error("should contain file name")
	}
	if !strings.Contains(result, `size="12"`) {
		t.Error("should contain correct size")
	}
	if !strings.Contains(result, "package main") {
		t.Error("should contain file content")
	}
	if !strings.HasPrefix(result, "<file") {
		t.Error("should start with <file")
	}
	if !strings.HasSuffix(result, "</file>") {
		t.Error("should end with </file>")
	}
}
