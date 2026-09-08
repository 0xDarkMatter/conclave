package cache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// countingProvider is safe to call concurrently, unlike fakeProvider's
// scripted answer function.
type countingProvider struct {
	name  string
	calls atomic.Int32
}

func (c *countingProvider) Name() string         { return c.name }
func (c *countingProvider) DefaultModel() string { return "m" }
func (c *countingProvider) IsAvailable() bool    { return true }
func (c *countingProvider) Query(ctx context.Context, prompt, model string) (string, time.Duration, *providers.Metrics, error) {
	c.calls.Add(1)
	return "answer:" + prompt, time.Millisecond, &providers.Metrics{InputTokens: 1, OutputTokens: 2}, nil
}

// TestConcurrentQueriesNeverServeTheWrongAnswer is the adversary that matters
// most for a content-addressed store: a torn or interleaved write handing one
// prompt another prompt's answer. Conclave queries providers in parallel, and
// two conclave processes can share the store, so writes genuinely overlap.
//
// Rounds run SEQUENTIALLY on purpose. Firing every round at once leaves no
// ordering between a round's Put and the next round's Get, so a run where all
// 16 prompts miss in every round is legal and the hit-rate assertion below
// would flake, most visibly under the race detector. Within a round the
// queries are still concurrent, which is what actually exercises the store.
func TestConcurrentQueriesNeverServeTheWrongAnswer(t *testing.T) {
	c := newTestCache(t, time.Hour)
	prov := &countingProvider{name: "openai"}
	p := Wrap(prov, c, ModeAPI)

	const prompts = 16
	const rounds = 4

	for r := 0; r < rounds; r++ {
		var wg sync.WaitGroup
		errs := make(chan string, prompts)
		for i := 0; i < prompts; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				prompt := fmt.Sprintf("prompt-%d", i)
				got, _, _, err := p.Query(context.Background(), prompt, "m")
				if err != nil {
					errs <- fmt.Sprintf("query %s: %v", prompt, err)
					return
				}
				if want := "answer:" + prompt; got != want {
					errs <- fmt.Sprintf("got %q, want %q", got, want)
				}
			}(i)
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			t.Error(e)
		}

		// After the first round every prompt is on disk, so no further round
		// may reach the provider at all. This is exact, not a heuristic,
		// because the rounds are ordered.
		if r == 0 {
			continue
		}
		if calls := int(prov.calls.Load()); calls > prompts {
			t.Fatalf("round %d: provider called %d times in total, want at most %d: the cache stopped hitting",
				r, calls, prompts)
		}
	}

	if calls := int(prov.calls.Load()); calls != prompts {
		t.Fatalf("provider called %d times, want exactly %d (one per distinct prompt)", calls, prompts)
	}
}

// TestReadersNeverSeeAPartialEntry is the guarantee Put actually makes: a
// reader sees a whole entry or nothing, never a truncated file decoded as a
// valid answer. Writes themselves may be refused under contention on Windows
// (a reader holding the destination open blocks the rename), which is why Put
// returns an error the caller is free to ignore: a refused write costs a cache
// miss, and a miss is always safe. What must never happen is a short read.
func TestReadersNeverSeeAPartialEntry(t *testing.T) {
	c := newTestCache(t, time.Hour)
	key := Key(ModeAPI, "openai", "m", "q", "")
	body := strings.Repeat("x", 4000)

	var wg sync.WaitGroup
	var reads, writeOK, writeFail atomic.Int32
	stop := make(chan struct{})

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if e, ok := c.Get(key); ok {
					reads.Add(1)
					if e.Response != body {
						t.Errorf("reader saw a partial entry: %d bytes, want %d", len(e.Response), len(body))
						return
					}
				}
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if err := c.Put(&Entry{Key: key, Response: body, FetchedAt: time.Now()}); err != nil {
					writeFail.Add(1)
				} else {
					writeOK.Add(1)
				}
			}
		}()
	}
	time.Sleep(150 * time.Millisecond)
	close(stop)
	wg.Wait()

	if writeOK.Load() == 0 {
		t.Fatalf("every write was refused (%d failures); the retry is not working", writeFail.Load())
	}
	if reads.Load() == 0 {
		t.Fatal("no reader ever observed the entry, so nothing was actually tested")
	}
}

// TestFailedPutLeavesNoTempFile: Stat counts only *.json, so a leaked temp is
// invisible to `conclave cache stats` and would accumulate silently until
// someone ran `cache clear`.
func TestFailedPutLeavesNoTempFile(t *testing.T) {
	c := newTestCache(t, time.Hour)
	key := Key(ModeAPI, "openai", "m", "q", "")

	// Force the rename to fail by putting a DIRECTORY where the entry belongs.
	p := c.path(key)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.Put(&Entry{Key: key, Response: "x"}); err == nil {
		t.Fatal("expected Put to fail when the destination is a directory")
	}

	matches, err := filepath.Glob(filepath.Join(filepath.Dir(p), ".resp-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("failed Put leaked %d temp file(s): %v", len(matches), matches)
	}
}

// TestSequentialOverwriteReplacesTheEntry: the uncontended path must replace
// cleanly, which is what dropping the pre-rename Remove is meant to preserve.
func TestSequentialOverwriteReplacesTheEntry(t *testing.T) {
	c := newTestCache(t, time.Hour)
	key := Key(ModeAPI, "openai", "m", "q", "")
	for _, want := range []string{"first", "second", "third"} {
		if err := c.Put(&Entry{Key: key, Response: want, FetchedAt: time.Now()}); err != nil {
			t.Fatalf("Put(%q): %v", want, err)
		}
		e, ok := c.Get(key)
		if !ok || e.Response != want {
			t.Fatalf("after Put(%q) got %v/%q", want, ok, e.Response)
		}
	}
	if info, _ := c.Stat(); info.Entries != 1 {
		t.Fatalf("overwrites produced %d entries, want 1", info.Entries)
	}
}

// TestSlashRoutedModelGetsItsOwnKey: an OpenRouter token is both the provider
// name and the model id (ADR-010), so two different slugs must not collide,
// and a slug must not collide with the same model reached through a direct
// provider, whose request shape differs.
func TestSlashRoutedModelGetsItsOwnKey(t *testing.T) {
	viaOpenRouter := Key(ModeAPI, "anthropic/claude-opus-5", "anthropic/claude-opus-5", "q", "")
	viaDirect := Key(ModeAPI, "claude", "claude-opus-5", "q", "")
	otherSlug := Key(ModeAPI, "deepseek/deepseek-v4", "deepseek/deepseek-v4", "q", "")

	if viaOpenRouter == viaDirect {
		t.Error("OpenRouter routing collided with the direct provider for the same model")
	}
	if viaOpenRouter == otherSlug {
		t.Error("two different OpenRouter slugs produced the same key")
	}
}
