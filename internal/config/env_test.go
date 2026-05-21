package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadEnvFromFile(t *testing.T) {
	// Create temp dir
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")

	// Write test env file
	content := `# Test env file
GEMINI_API_KEY=test-gemini-key
OPENAI_API_KEY=test-openai-key
# Comment line
ANTHROPIC_API_KEY="quoted-value"

EMPTY_LINE_ABOVE=works
`
	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	// Clear any existing env vars
	os.Unsetenv("GEMINI_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("EMPTY_LINE_ABOVE")

	// Load the file
	if err := loadEnvFromFile(envFile, false); err != nil {
		t.Fatalf("loadEnvFromFile: %v", err)
	}

	// Verify values
	tests := []struct {
		key      string
		expected string
	}{
		{"GEMINI_API_KEY", "test-gemini-key"},
		{"OPENAI_API_KEY", "test-openai-key"},
		{"ANTHROPIC_API_KEY", "quoted-value"},
		{"EMPTY_LINE_ABOVE", "works"},
	}

	for _, tt := range tests {
		got := os.Getenv(tt.key)
		if got != tt.expected {
			t.Errorf("%s = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestLoadEnvFromFile_NoOverride(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")

	content := `EXISTING_KEY=from-file`
	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	// Set existing value
	os.Setenv("EXISTING_KEY", "already-set")
	defer os.Unsetenv("EXISTING_KEY")

	// Load without override
	if err := loadEnvFromFile(envFile, false); err != nil {
		t.Fatalf("loadEnvFromFile: %v", err)
	}

	// Should keep original value
	if got := os.Getenv("EXISTING_KEY"); got != "already-set" {
		t.Errorf("EXISTING_KEY = %q, want %q", got, "already-set")
	}
}

func TestLoadEnvFromFile_Override(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")

	content := `OVERRIDE_KEY=from-file`
	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	// Set existing value
	os.Setenv("OVERRIDE_KEY", "already-set")
	defer os.Unsetenv("OVERRIDE_KEY")

	// Load with override
	if err := loadEnvFromFile(envFile, true); err != nil {
		t.Fatalf("loadEnvFromFile: %v", err)
	}

	// Should use new value
	if got := os.Getenv("OVERRIDE_KEY"); got != "from-file" {
		t.Errorf("OVERRIDE_KEY = %q, want %q", got, "from-file")
	}
}

func TestSaveEnvFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Override EnvFilePath for testing
	origEnvFilePath := EnvFilePath
	defer func() { EnvFilePath = origEnvFilePath }()

	testEnvPath := filepath.Join(tmpDir, "conclave", ".env")
	EnvFilePath = func() (string, error) { return testEnvPath, nil }

	// Save some keys
	keys := map[string]string{
		"GEMINI_API_KEY":  "gemini-test",
		"OPENAI_API_KEY":  "openai-test",
		"XAI_API_KEY":     "xai-test",
	}

	if err := SaveEnvFile(keys); err != nil {
		t.Fatalf("SaveEnvFile: %v", err)
	}

	// Verify file exists with correct permissions
	info, err := os.Stat(testEnvPath)
	if err != nil {
		t.Fatalf("stat env file: %v", err)
	}
	// POSIX permissions don't map onto Windows ACLs; the Go stdlib reports
	// 0666 there regardless of the mode passed to OpenFile. Skip the check.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("file permissions = %o, want 0600", perm)
		}
	}

	// Read back and verify
	os.Unsetenv("GEMINI_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("XAI_API_KEY")

	if err := loadEnvFromFile(testEnvPath, false); err != nil {
		t.Fatalf("loadEnvFromFile: %v", err)
	}

	for key, expected := range keys {
		if got := os.Getenv(key); got != expected {
			t.Errorf("%s = %q, want %q", key, got, expected)
		}
	}
}

func TestSaveEnvFile_PreservesExisting(t *testing.T) {
	tmpDir := t.TempDir()

	origEnvFilePath := EnvFilePath
	defer func() { EnvFilePath = origEnvFilePath }()

	testEnvPath := filepath.Join(tmpDir, "conclave", ".env")
	EnvFilePath = func() (string, error) { return testEnvPath, nil }

	// Create initial file
	os.MkdirAll(filepath.Dir(testEnvPath), 0700)
	initial := "EXISTING_KEY=preserve-me\n"
	os.WriteFile(testEnvPath, []byte(initial), 0600)

	// Save new key
	if err := SaveEnvFile(map[string]string{"NEW_KEY": "new-value"}); err != nil {
		t.Fatalf("SaveEnvFile: %v", err)
	}

	// Read back
	os.Unsetenv("EXISTING_KEY")
	os.Unsetenv("NEW_KEY")
	if err := loadEnvFromFile(testEnvPath, false); err != nil {
		t.Fatalf("loadEnvFromFile: %v", err)
	}

	// Both should exist
	if got := os.Getenv("EXISTING_KEY"); got != "preserve-me" {
		t.Errorf("EXISTING_KEY = %q, want %q", got, "preserve-me")
	}
	if got := os.Getenv("NEW_KEY"); got != "new-value" {
		t.Errorf("NEW_KEY = %q, want %q", got, "new-value")
	}
}
