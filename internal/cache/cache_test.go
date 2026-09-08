package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// fakeProvider counts calls so a test can prove a second query never reached it.
type fakeProvider struct {
	name  string
	reply string
	err   error
	calls int
}

func (f *fakeProvider) Name() string         { return f.name }
func (f *fakeProvider) DefaultModel() string { return "fake-model" }
func (f *fakeProvider) IsAvailable() bool    { return true }
func (f *fakeProvider) Query(ctx context.Context, prompt, model string) (string, time.Duration, *providers.Metrics, error) {
	f.calls++
	if f.err != nil {
		return "", time.Millisecond, nil, f.err
	}
	return f.reply, time.Millisecond, &providers.Metrics{InputTokens: 10, OutputTokens: 20}, nil
}

func newTestCache(t *testing.T, ttl time.Duration) *Cache {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "responses"), ttl)
}

// TestKeyChangesWithEveryComponent defends against the silent-wrong-answer
// failure: any change to what was actually sent must be a miss.
func TestKeyChangesWithEveryComponent(t *testing.T) {
	base := Key(ModeAPI, "openai", "gpt-test", "prompt", "")
	cases := map[string]string{
		"mode":     Key(ModeCLI, "openai", "gpt-test", "prompt", ""),
		"provider": Key(ModeAPI, "claude", "gpt-test", "prompt", ""),
		"model":    Key(ModeAPI, "openai", "gpt-other", "prompt", ""),
		"prompt":   Key(ModeAPI, "openai", "gpt-test", "prompt ", ""),
		"system":   Key(ModeAPI, "openai", "gpt-test", "prompt", "be terse"),
	}
	for name, k := range cases {
		if k == base {
			t.Errorf("changing %s did not change the key", name)
		}
	}
}

// TestKeyIsNotAmbiguousAcrossFieldBoundaries defends against a field-splicing
// collision: "ab"+"c" and "a"+"bc" must not hash alike.
func TestKeyIsNotAmbiguousAcrossFieldBoundaries(t *testing.T) {
	a := Key(ModeAPI, "ab", "c", "p", "")
	b := Key(ModeAPI, "a", "bc", "p", "")
	if a == b {
		t.Fatal("concatenated fields collided; the key is not length-prefixed")
	}
}

// TestContextChangeIsAMiss: the prompt passed to Query already includes file
// and stdin context, so a one-byte change must not be served from cache.
func TestContextChangeIsAMiss(t *testing.T) {
	f := &fakeProvider{name: "openai", reply: "answer"}
	p := Wrap(f, newTestCache(t, time.Hour), ModeAPI)

	if _, _, _, err := p.Query(context.Background(), "FILE A\n\nquestion", "m"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := p.Query(context.Background(), "FILE B\n\nquestion", "m"); err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 {
		t.Fatalf("expected 2 live calls for two different contexts, got %d", f.calls)
	}
}

func TestHitAvoidsTheProviderAndCostsNothing(t *testing.T) {
	f := &fakeProvider{name: "openai", reply: "answer"}
	p := Wrap(f, newTestCache(t, time.Hour), ModeAPI)

	if _, _, m, err := p.Query(context.Background(), "q", "m"); err != nil || m.Cached {
		t.Fatalf("first call should be a live miss (err=%v cached=%v)", err, m.Cached)
	}
	resp, _, m, err := p.Query(context.Background(), "q", "m")
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("second call reached the provider (%d calls)", f.calls)
	}
	if resp != "answer" {
		t.Fatalf("cached response = %q", resp)
	}
	if !m.Cached {
		t.Fatal("hit was not marked Cached")
	}
	if m.CostUSD != 0 {
		t.Fatalf("cached hit was priced at %v", m.CostUSD)
	}
	// Token counts survive so the token footer stays truthful about the work
	// the answer originally represented.
	if m.InputTokens != 10 || m.OutputTokens != 20 {
		t.Fatalf("metrics lost on round trip: %+v", m)
	}
}

// TestExpiredEntryIsAMiss defends against serving a stale answer as current.
func TestExpiredEntryIsAMiss(t *testing.T) {
	c := newTestCache(t, time.Hour)
	key := Key(ModeAPI, "openai", "m", "q", "")
	if err := c.Put(&Entry{Key: key, FetchedAt: time.Now().Add(-2 * time.Hour), Response: "old"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(key); ok {
		t.Fatal("expired entry was served")
	}
}

// TestCorruptEntryDegradesToAMiss: a truncated or garbled file must never
// fail a real question.
func TestCorruptEntryDegradesToAMiss(t *testing.T) {
	c := newTestCache(t, time.Hour)
	key := Key(ModeAPI, "openai", "m", "q", "")
	p := c.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(key); ok {
		t.Fatal("corrupt entry was served")
	}

	f := &fakeProvider{name: "openai", reply: "live"}
	wrapped := Wrap(f, c, ModeAPI)
	got, _, _, err := wrapped.Query(context.Background(), "q", "m")
	if err != nil || got != "live" {
		t.Fatalf("corrupt entry did not fall through to a live call: %q %v", got, err)
	}
}

// TestMismatchedKeyIsRejected: a file whose recorded key does not match its
// own address is corruption, not an answer.
func TestMismatchedKeyIsRejected(t *testing.T) {
	c := newTestCache(t, time.Hour)
	key := Key(ModeAPI, "openai", "m", "q", "")
	p := c.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"key":"deadbeef","response":"x","fetched_at":"` + time.Now().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(key); ok {
		t.Fatal("entry with a mismatched key was served")
	}
}

// TestFailuresAreNotCached: caching an error would make one transient outage
// stick for the whole TTL.
func TestFailuresAreNotCached(t *testing.T) {
	f := &fakeProvider{name: "openai", err: errors.New("429 rate limited")}
	c := newTestCache(t, time.Hour)
	p := Wrap(f, c, ModeAPI)

	if _, _, _, err := p.Query(context.Background(), "q", "m"); err == nil {
		t.Fatal("expected the provider error to surface")
	}
	if _, ok := c.Get(Key(ModeAPI, "openai", "m", "q", "")); ok {
		t.Fatal("a failed call was written to the cache")
	}
}

// TestNilCacheIsAPassThrough keeps the feature genuinely opt-in.
func TestNilCacheIsAPassThrough(t *testing.T) {
	f := &fakeProvider{name: "openai", reply: "x"}
	if got := Wrap(f, nil, ModeAPI); got != providers.Provider(f) {
		t.Fatal("Wrap with a nil cache must return the provider untouched")
	}
	if got := WrapAll([]providers.Provider{f}, nil, ModeAPI); got[0] != providers.Provider(f) {
		t.Fatal("WrapAll with a nil cache must return the list untouched")
	}
}

// TestWrapperDoesNotForwardPreflighter pins the guard documented in
// provider.go: if this ever starts passing, wrapping before RunPreflight would
// silently skip auth checks.
func TestWrapperDoesNotForwardPreflighter(t *testing.T) {
	f := &fakeProvider{name: "openai", reply: "x"}
	w := Wrap(f, newTestCache(t, time.Hour), ModeAPI)
	if _, ok := w.(providers.Preflighter); ok {
		t.Fatal("wrapper now satisfies Preflighter; re-check the wrap-after-preflight ordering in cmd/root.go")
	}
}

func TestStatAndClear(t *testing.T) {
	c := newTestCache(t, time.Hour)
	if info, err := c.Stat(); err != nil || info.Entries != 0 {
		t.Fatalf("empty store: %+v %v", info, err)
	}
	for _, q := range []string{"a", "b", "c"} {
		if err := c.Put(&Entry{Key: Key(ModeAPI, "openai", "m", q, ""), Response: "r"}); err != nil {
			t.Fatal(err)
		}
	}
	info, err := c.Stat()
	if err != nil || info.Entries != 3 || info.Bytes == 0 {
		t.Fatalf("stat = %+v, err = %v", info, err)
	}
	n, err := c.Clear()
	if err != nil || n != 3 {
		t.Fatalf("clear removed %d (%v), want 3", n, err)
	}
	if info, _ := c.Stat(); info.Entries != 0 {
		t.Fatalf("store not empty after clear: %+v", info)
	}
	// Clearing an already-empty store is a no-op, not an error.
	if n, err := c.Clear(); err != nil || n != 0 {
		t.Fatalf("second clear = %d, %v", n, err)
	}
}

func TestEntriesAreSharded(t *testing.T) {
	c := newTestCache(t, time.Hour)
	key := Key(ModeAPI, "openai", "m", "q", "")
	if err := c.Put(&Entry{Key: key, Response: "r"}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(c.Dir(), key[:2], key+".json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("entry not at sharded path %s: %v", want, err)
	}
}
