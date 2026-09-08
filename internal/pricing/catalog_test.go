package pricing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// feed is a minimal OpenRouter-shaped response covering the naming cases the
// slug rewriter has to survive.
const feed = `{"data":[
 {"id":"anthropic/claude-opus-4.8","name":"Anthropic: Claude Opus 4.8","created":1748300000,"context_length":1000000,"pricing":{"prompt":"0.000005","completion":"0.000025"}},
 {"id":"anthropic/claude-haiku-4.5","name":"Anthropic: Claude Haiku 4.5","created":1760500000,"context_length":200000,"pricing":{"prompt":"0.000001","completion":"0.000005"}},
 {"id":"anthropic/claude-haiku-4.5:batch","name":"batch","created":1760500000,"context_length":200000,"pricing":{"prompt":"0.0000005","completion":"0.0000025"}},
 {"id":"anthropic/claude-fable-5.1","name":"Anthropic: Claude Fable 5.1","created":1756700000,"context_length":1000000,"pricing":{"prompt":"0.00001","completion":"0.00005"}},
 {"id":"z-ai/glm-5.2","name":"Z.ai: GLM 5.2","created":1750000000,"context_length":1048576,"pricing":{"prompt":"0.000000966","completion":"0.000003036"}},
 {"id":"x-ai/grok-4.1-fast","name":"xAI: Grok 4.1 Fast","created":1763000000,"context_length":2000000,"pricing":{"prompt":"0.0000002","completion":"0.0000005"}},
 {"id":"openai/gpt-5-nano","name":"OpenAI: GPT-5 Nano","created":1754500000,"context_length":400000,"pricing":{"prompt":"0.00000005","completion":"0.0000004"}}
]}`

func newServer(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(feed))
	}))
}

func TestCandidateSlugs(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-8":           "claude-opus-4.8",
		"claude-haiku-4-5-20251001": "claude-haiku-4.5",
		"claude-fable-5-1":          "claude-fable-5.1",
		"claude-opus-5":             "claude-opus-5",
		"zai-coding-plan/glm-5.2":   "glm-5.2",
		"grok-4-1-fast-reasoning":   "grok-4.1-fast",
		"gpt-5-nano":                "gpt-5-nano",
	}
	for in, want := range cases {
		got := candidateSlugs(in)
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("candidateSlugs(%q) = %v, want it to contain %q", in, got, want)
		}
		if got[0] != filepathBase(in) {
			t.Errorf("candidateSlugs(%q) must try the literal id first, got %v", in, got)
		}
	}
}

// approx compares USD/M prices that were scaled from per-token strings.
func approx(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

func filepathBase(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return s
}

func TestLoadFetchesThenCaches(t *testing.T) {
	var hits int32
	srv := newServer(t, &hits)
	defer srv.Close()
	dir := t.TempDir()
	opts := Options{URL: srv.URL, CacheDir: dir, TTL: time.Hour}

	c, err := Load(context.Background(), opts)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if c == nil || len(c.Models) != 7 {
		t.Fatalf("expected 7 models, got %+v", c)
	}
	if _, err := os.Stat(filepath.Join(dir, cacheFileName)); err != nil {
		t.Fatalf("cache not written: %v", err)
	}

	// Second load inside the TTL must not hit the network.
	c2, err := Load(context.Background(), opts)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if c2.Stale {
		t.Errorf("fresh cache reported stale")
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("expected 1 fetch, got %d", hits)
	}

	// Price conversion: per-token strings -> per-million floats.
	in, out, ok := c2.Price("claude", "claude-opus-4-8")
	if !ok || in != 5 || out != 25 {
		t.Errorf("Price(claude-opus-4-8) = %v %v %v, want 5 25 true", in, out, ok)
	}
}

func TestLookupNamingDrift(t *testing.T) {
	var hits int32
	srv := newServer(t, &hits)
	defer srv.Close()
	c, err := Load(context.Background(), Options{URL: srv.URL, CacheDir: t.TempDir(), TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	must := []struct{ provider, model, wantID string }{
		{"claude", "claude-haiku-4-5-20251001", "anthropic/claude-haiku-4.5"},
		{"claude", "claude-fable-5-1", "anthropic/claude-fable-5.1"},
		{"glm", "zai-coding-plan/glm-5.2", "z-ai/glm-5.2"},
		{"grok", "grok-4-1-fast-reasoning", "x-ai/grok-4.1-fast"},
		{"grok", "grok-4-1-fast-non-reasoning", "x-ai/grok-4.1-fast"},
		{"openai", "gpt-5-nano", "openai/gpt-5-nano"},
	}
	for _, tc := range must {
		m, ok := c.Lookup(tc.provider, tc.model)
		if !ok || m.ID != tc.wantID {
			t.Errorf("Lookup(%s, %s) = %q %v, want %q", tc.provider, tc.model, m.ID, ok, tc.wantID)
		}
	}
	if c.Has("glm", "glm-4.6v-flashx") {
		t.Errorf("glm-4.6v-flashx should be a miss")
	}
	if c.Has("nope", "anything") {
		t.Errorf("unknown provider should be a miss")
	}
	// A nil catalog is a valid, always-miss catalog.
	var nilCat *Catalog
	if nilCat.Has("claude", "claude-opus-4-8") {
		t.Errorf("nil catalog must miss")
	}
}

func TestByVendorFiltersVariantsNewestFirst(t *testing.T) {
	var hits int32
	srv := newServer(t, &hits)
	defer srv.Close()
	c, err := Load(context.Background(), Options{URL: srv.URL, CacheDir: t.TempDir(), TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	got := c.ByVendor("claude")
	if len(got) != 3 {
		t.Fatalf("expected 3 anthropic models without :batch, got %d", len(got))
	}
	if got[0].ID != "anthropic/claude-haiku-4.5" {
		t.Errorf("newest first: got %s", got[0].ID)
	}
	for _, m := range got {
		if m.ID == "anthropic/claude-haiku-4.5:batch" {
			t.Errorf(":batch variant leaked into ByVendor")
		}
	}
}

func TestStaleCacheServedAndRefreshedInBackground(t *testing.T) {
	var hits int32
	srv := newServer(t, &hits)
	defer srv.Close()
	dir := t.TempDir()

	// Seed the cache, then age it past a tiny TTL.
	if _, err := Load(context.Background(), Options{URL: srv.URL, CacheDir: dir, TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	c, err := Load(context.Background(), Options{URL: srv.URL, CacheDir: dir, TTL: time.Millisecond})
	if err != nil {
		t.Fatalf("stale load must not error: %v", err)
	}
	if !c.Stale {
		t.Errorf("expected stale flag on aged cache")
	}
	WaitBackground(5 * time.Second)
	if atomic.LoadInt32(&hits) != 2 {
		t.Errorf("expected background refresh to fetch once more, hits=%d", hits)
	}
}

func TestOfflineFallsBackToCacheOrNil(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(context.Background(), Options{URL: "http://127.0.0.1:1", CacheDir: dir, Offline: true})
	if err != nil || c != nil {
		t.Errorf("offline with no cache: want nil,nil got %v,%v", c, err)
	}
	// Unreachable feed and no cache: error, nil catalog, never a panic.
	c, err = Load(context.Background(), Options{URL: "http://127.0.0.1:1", CacheDir: dir, TTL: time.Hour,
		Client: &http.Client{Timeout: 300 * time.Millisecond}})
	if err == nil || c != nil {
		t.Errorf("unreachable feed: want error and nil catalog, got %v,%v", c, err)
	}
}

func TestDisabledEnv(t *testing.T) {
	t.Setenv("CONCLAVE_NO_PRICING", "1")
	c, err := Load(context.Background(), Options{URL: "http://127.0.0.1:1", CacheDir: t.TempDir()})
	if c != nil || err != nil {
		t.Errorf("disabled: want nil,nil got %v,%v", c, err)
	}
}

// === Slash-routed OpenRouter tokens (ADR-010) ===

// TestLookup_SlashTokenIsExactID pins the special case: when the provider name
// is itself an OpenRouter slug, Lookup matches it verbatim, never rewrites it,
// and VendorPrefix/ByVendor derive the vendor from the token.
func TestLookup_SlashTokenIsExactID(t *testing.T) {
	var hits int32
	srv := newServer(t, &hits)
	defer srv.Close()
	c, err := Load(context.Background(), Options{URL: srv.URL, CacheDir: t.TempDir(), TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		provider, model string
		wantID          string
		wantOK          bool
	}{
		{"anthropic/claude-fable-5.1", "anthropic/claude-fable-5.1", "anthropic/claude-fable-5.1", true},
		{"x-ai/grok-4.1-fast", "x-ai/grok-4.1-fast", "x-ai/grok-4.1-fast", true},
		// -m override that differs from the token still resolves via the model.
		{"anthropic/claude-opus-4.8", "anthropic/claude-haiku-4.5", "anthropic/claude-opus-4.8", true},
		{"anthropic/nope", "anthropic/claude-haiku-4.5:batch", "anthropic/claude-haiku-4.5:batch", true},
		// No rewriting for slugs: a vendor-style id in a slash token is a miss.
		{"anthropic/claude-opus-4-8", "anthropic/claude-opus-4-8", "", false},
		{"deepseek/deepseek-v4", "deepseek/deepseek-v4", "", false},
	}
	for _, tt := range tests {
		m, ok := c.Lookup(tt.provider, tt.model)
		if ok != tt.wantOK || m.ID != tt.wantID {
			t.Errorf("Lookup(%q, %q) = (%q, %v), want (%q, %v)", tt.provider, tt.model, m.ID, ok, tt.wantID, tt.wantOK)
		}
	}

	if in, out, ok := c.Price("openai/gpt-5-nano", "openai/gpt-5-nano"); !ok || !approx(in, 0.05) || !approx(out, 0.4) {
		t.Errorf("Price via slash token = (%v, %v, %v), want (0.05, 0.4, true)", in, out, ok)
	}

	if v, ok := VendorPrefix("deepseek/deepseek-v4"); !ok || v != "deepseek" {
		t.Errorf("VendorPrefix(slash) = (%q, %v), want (deepseek, true)", v, ok)
	}
	if v, ok := VendorPrefix("claude"); !ok || v != "anthropic" {
		t.Errorf("VendorPrefix(claude) = (%q, %v), want (anthropic, true)", v, ok)
	}
	if _, ok := VendorPrefix("/leading-slash"); ok {
		t.Error("a leading slash is not a vendor/model token")
	}

	alts := c.ByVendor("anthropic/whatever")
	if len(alts) == 0 || alts[0].Vendor() != "anthropic" {
		t.Errorf("ByVendor(slash token) should list the token's vendor; got %v", alts)
	}
	for _, m := range alts {
		if strings.Contains(m.Slug(), ":") {
			t.Errorf("ByVendor must still drop :variants; got %s", m.ID)
		}
	}

	if name, ok := c.NameOf("anthropic/claude-fable-5.1"); !ok || name != "Anthropic: Claude Fable 5.1" {
		t.Errorf("NameOf = (%q, %v)", name, ok)
	}
	if _, ok := c.NameOf("nobody/nothing"); ok {
		t.Error("NameOf miss should report !ok")
	}
	var nilCat *Catalog
	if _, ok := nilCat.NameOf("anthropic/claude-fable-5.1"); ok {
		t.Error("NameOf on a nil catalog must be safe and report !ok")
	}
}
