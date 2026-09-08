package batch

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func tempOutput(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "out.jsonl")
}

func TestCheckpointPathIsDerivedFromOutput(t *testing.T) {
	out := tempOutput(t)
	c := NewCheckpoint(out)
	if c.path != out+".checkpoint" {
		t.Fatalf("checkpoint path = %q, want %q", c.path, out+".checkpoint")
	}
}

// TestLoadMissingFileIsNotAnError: the first run of a job has no checkpoint,
// and treating that as an error would make --resume unusable as a default.
func TestLoadMissingFileIsNotAnError(t *testing.T) {
	c := NewCheckpoint(tempOutput(t))
	if err := c.Load(); err != nil {
		t.Fatalf("Load on a fresh job: %v", err)
	}
	if c.ProcessedCount() != 0 {
		t.Fatalf("ProcessedCount = %d, want 0", c.ProcessedCount())
	}
}

// TestLoadIgnoresBlankLines defends against a checkpoint truncated mid-write by
// a kill: a trailing empty line must not register as a processed item named "".
func TestLoadIgnoresBlankLines(t *testing.T) {
	out := tempOutput(t)
	body := "a\n\nb\n\n\nc\n"
	if err := os.WriteFile(out+".checkpoint", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewCheckpoint(out)
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	if c.ProcessedCount() != 3 {
		t.Fatalf("ProcessedCount = %d, want 3 (blank lines ignored)", c.ProcessedCount())
	}
	if c.IsProcessed("") {
		t.Fatal("empty string registered as a processed id")
	}
	for _, id := range []string{"a", "b", "c"} {
		if !c.IsProcessed(id) {
			t.Errorf("id %q not loaded", id)
		}
	}
}

// TestLoadWithNoTrailingNewline: a run killed between the id and its newline
// must still count that id, or the item is charged for twice.
func TestLoadWithNoTrailingNewline(t *testing.T) {
	out := tempOutput(t)
	if err := os.WriteFile(out+".checkpoint", []byte("a\nb"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewCheckpoint(out)
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	if !c.IsProcessed("b") || c.ProcessedCount() != 2 {
		t.Fatalf("unterminated final line lost: count=%d", c.ProcessedCount())
	}
}

// TestMarkProcessedAppendsAndSurvivesReload is the whole point of the file:
// what one run recorded, the next run must see.
func TestMarkProcessedAppendsAndSurvivesReload(t *testing.T) {
	out := tempOutput(t)
	c := NewCheckpoint(out)
	for _, id := range []string{"a", "b", "c"} {
		if err := c.MarkProcessed(id); err != nil {
			t.Fatalf("MarkProcessed(%q): %v", id, err)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	reloaded := NewCheckpoint(out)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if reloaded.ProcessedCount() != 3 {
		t.Fatalf("after reload ProcessedCount = %d, want 3", reloaded.ProcessedCount())
	}
}

// TestMarkProcessedIsIdempotent: without this a retried write would grow the
// file without bound on a long job.
func TestMarkProcessedIsIdempotent(t *testing.T) {
	out := tempOutput(t)
	c := NewCheckpoint(out)
	for i := 0; i < 5; i++ {
		if err := c.MarkProcessed("a"); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out + ".checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "a\n" {
		t.Fatalf("checkpoint file = %q, want a single entry", string(b))
	}
}

// TestConcurrentMarkProcessedIsSafe: the writer goroutine marks ids while the
// feeder reads IsProcessed, so the map must be guarded.
func TestConcurrentMarkProcessedIsSafe(t *testing.T) {
	c := NewCheckpoint(tempOutput(t))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i%26))
			_ = c.MarkProcessed(id)
			_ = c.IsProcessed(id)
			_ = c.ProcessedCount()
		}(i)
	}
	wg.Wait()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if c.ProcessedCount() != 26 {
		t.Fatalf("ProcessedCount = %d, want 26 distinct ids", c.ProcessedCount())
	}
}

// TestClearRemovesTheFileAndTheMemory covers the fresh-start path.
func TestClearRemovesTheFileAndTheMemory(t *testing.T) {
	out := tempOutput(t)
	c := NewCheckpoint(out)
	if err := c.MarkProcessed("a"); err != nil {
		t.Fatal(err)
	}
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if c.ProcessedCount() != 0 || c.IsProcessed("a") {
		t.Fatal("Clear left processed ids in memory")
	}
	if _, err := os.Stat(out + ".checkpoint"); !os.IsNotExist(err) {
		t.Fatalf("checkpoint file still present: %v", err)
	}
}

// TestCloseIsIdempotent: Processor.Close runs from a defer that can fire twice
// in tests and in error paths.
func TestCloseIsIdempotent(t *testing.T) {
	c := NewCheckpoint(tempOutput(t))
	if err := c.MarkProcessed("a"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestMarkProcessedCreatesMissingDirectories: -o results/run1/out.jsonl must
// not fail on the checkpoint when the directory does not exist yet.
func TestMarkProcessedCreatesMissingDirectories(t *testing.T) {
	out := filepath.Join(t.TempDir(), "nested", "deeper", "out.jsonl")
	c := NewCheckpoint(out)
	if err := c.MarkProcessed("a"); err != nil {
		t.Fatalf("MarkProcessed into a missing directory: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out + ".checkpoint"); err != nil {
		t.Fatalf("checkpoint not written: %v", err)
	}
}
