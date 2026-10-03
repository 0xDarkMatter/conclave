// Tests for Load: fetch-or-cache, mirror cross-check, mirror-only rows and the
// advisory failure rules (ADR-018, ADR-009). All HTTP is httptest; all data is
// synthetic (the Decision Index carries no licence, so none is committed).
package decisionindex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fixtureServer serves a one-benchmark edition with two upstream models
// (a: index 40, b: index 80) and the given mirror body. mirrorStatus != 200
// makes the mirror fail.
func fixtureServer(t *testing.T, mirror any, mirrorStatus int) *httptest.Server {
	t.Helper()
	meth := buildMethodology(t, singleArea(tBench{id: 1}))
	idx := buildIndex(t,
		row("a", map[int]float64{1: 0.4}, nil),
		row("b", map[int]float64{1: 0.8}, nil))
	mb, _ := json.Marshal(mirror)
	mux := http.NewServeMux()
	mux.HandleFunc("/data/"+IndexFile, func(w http.ResponseWriter, _ *http.Request) { w.Write(idx) })
	mux.HandleFunc("/data/"+MethodologyFile, func(w http.ResponseWriter, _ *http.Request) { w.Write(meth) })
	mux.HandleFunc("/mirror", func(w http.ResponseWriter, _ *http.Request) {
		if mirrorStatus != http.StatusOK {
			http.Error(w, "down", mirrorStatus)
			return
		}
		w.Write(mb)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func loadFrom(t *testing.T, srv *httptest.Server) (*Board, error) {
	t.Helper()
	t.Setenv("CONCLAVE_NO_PRICING", "")
	return Load(context.Background(), Options{
		CacheDir:     t.TempDir(),
		HTTPClient:   srv.Client(),
		UpstreamBase: srv.URL + "/data/",
		MirrorURL:    srv.URL + "/mirror",
	})
}

func mirrorRows(rows ...map[string]any) map[string]any {
	return map[string]any{"generated_utc": "2026-10-01",
		"upstream": map[string]any{"label": "Decision Index " + strings.TrimPrefix(Edition, "v")}, "models": rows}
}

func TestLoadCrossCheckFlagsMismatch(t *testing.T) {
	srv := fixtureServer(t, mirrorRows(
		map[string]any{"engine": "a", "name": "A", "index": 40.0},
		map[string]any{"engine": "b", "name": "B", "index": 81.5}, // 1.5 off
	), http.StatusOK)
	b, err := loadFrom(t, srv)
	if err != nil {
		t.Fatal(err)
	}
	if b.Check == nil {
		t.Fatal("Check is nil with a healthy mirror")
	}
	if b.Check.Compared != 2 || len(b.Check.Mismatch) != 1 || b.Check.Mismatch[0] != "b" {
		t.Errorf("Check = %+v, want 2 compared and mismatch [b]", *b.Check)
	}
	if !near(b.Check.MaxDelta, 1.5, 1e-9) {
		t.Errorf("MaxDelta = %v, want 1.5", b.Check.MaxDelta)
	}
	if !anyContains(b.Warnings, "b") {
		t.Errorf("a mismatch must surface as a warning, got %q", b.Warnings)
	}
}

func TestLoadMirrorDownIsAWarningNotAnError(t *testing.T) {
	srv := fixtureServer(t, nil, http.StatusInternalServerError)
	b, err := loadFrom(t, srv)
	if err != nil {
		t.Fatalf("mirror outage must not fail Load: %v", err)
	}
	if b.Check != nil {
		t.Errorf("Check = %+v, want nil without a mirror", *b.Check)
	}
	if len(b.Entries) != 2 {
		t.Errorf("upstream entries lost: %d", len(b.Entries))
	}
	if !anyContains(b.Warnings, "mirror") {
		t.Errorf("want a mirror warning, got %q", b.Warnings)
	}
}

func TestLoadAppendsMirrorOnlyRowsAsSelfReported(t *testing.T) {
	srv := fixtureServer(t, mirrorRows(
		map[string]any{"engine": "a", "name": "A", "index": 40.0},
		map[string]any{"engine": "clef", "name": "Clef", "index": 61.21, "ece": nil,
			"latency": map[string]any{"median": 209.3, "p95": 238.6}, "self_reported": true,
			"areas": map[string]any{"knowledge": 0.5}, "panel_coverage": 36},
	), http.StatusOK)
	b, err := loadFrom(t, srv)
	if err != nil {
		t.Fatal(err)
	}
	var clef *Entry
	for i := range b.Entries {
		if b.Entries[i].Engine == "clef" {
			clef = &b.Entries[i]
		}
	}
	if clef == nil {
		t.Fatalf("mirror-only row not appended: %+v", b.Entries)
	}
	if !clef.FromMirror || !clef.SelfReported {
		t.Errorf("Clef must be FromMirror and SelfReported: %+v", *clef)
	}
	if clef.Index != 61.21 || clef.ECE != nil || clef.MedianMs == nil || *clef.MedianMs != 209.3 || clef.Benchmarks != 36 {
		t.Errorf("Clef fields not taken from mirror: %+v", *clef)
	}
	for _, e := range b.Entries {
		if e.Engine != "clef" && (e.FromMirror || e.SelfReported) {
			t.Errorf("upstream row %q marked mirror/self-reported", e.Name)
		}
	}
}

func TestLoadUpstreamDownIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	if b, err := loadFrom(t, srv); err == nil {
		t.Fatalf("want an error without upstream data, got board %+v", b)
	}
}

func TestLoadHonoursNoPricing(t *testing.T) {
	t.Setenv("CONCLAVE_NO_PRICING", "1")
	_, err := Load(context.Background(), Options{CacheDir: t.TempDir(), UpstreamBase: "http://127.0.0.1:1/"})
	if err == nil || !strings.Contains(err.Error(), "CONCLAVE_NO_PRICING") {
		t.Fatalf("err = %v, want one naming CONCLAVE_NO_PRICING", err)
	}
}

// A second Load inside the TTL must not touch the network: the cache is what
// keeps --frontier usable offline and off Hugging Face's rate limits.
func TestLoadServesFreshCacheWithoutRefetching(t *testing.T) {
	srv := fixtureServer(t, mirrorRows(
		map[string]any{"engine": "a", "name": "A", "index": 40.0},
		map[string]any{"engine": "b", "name": "B", "index": 80.0},
	), http.StatusOK)
	t.Setenv("CONCLAVE_NO_PRICING", "")
	opts := Options{CacheDir: t.TempDir(), HTTPClient: srv.Client(), UpstreamBase: srv.URL + "/data/", MirrorURL: srv.URL + "/mirror"}
	if _, err := Load(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	b, err := Load(context.Background(), opts)
	if err != nil {
		t.Fatalf("cached Load hit the network: %v", err)
	}
	if len(b.Entries) != 2 || b.Check == nil {
		t.Errorf("cached board incomplete: %+v", b)
	}
	// A refetch would fail against the closed server and fall back to the
	// stale-cache path, which warns; a fresh cache must not warn at all.
	if len(b.Warnings) != 0 {
		t.Errorf("fresh cache was refetched: %q", b.Warnings)
	}
}

// === Refuted defects (Codex review, 2026-10-03) ===

// swapServer serves upstream and mirror bodies that a test can replace
// between Loads.
type swapServer struct {
	mu                sync.Mutex
	meth, idx, mirror []byte
	srv               *httptest.Server
}

func newSwapServer(t *testing.T, meth, idx, mirror []byte) *swapServer {
	t.Helper()
	s := &swapServer{meth: meth, idx: idx, mirror: mirror}
	serve := func(b *[]byte) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			s.mu.Lock()
			defer s.mu.Unlock()
			w.Write(*b)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/data/"+IndexFile, serve(&s.idx))
	mux.HandleFunc("/data/"+MethodologyFile, serve(&s.meth))
	mux.HandleFunc("/mirror", serve(&s.mirror))
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *swapServer) set(f func(s *swapServer)) { s.mu.Lock(); f(s); s.mu.Unlock() }

func (s *swapServer) opts(dir string, refresh bool) Options {
	return Options{CacheDir: dir, Refresh: refresh, HTTPClient: s.srv.Client(),
		UpstreamBase: s.srv.URL + "/data/", MirrorURL: s.srv.URL + "/mirror"}
}

func goodGeneration(t *testing.T) (meth, idx, mirror []byte) {
	t.Helper()
	meth = buildMethodology(t, singleArea(tBench{id: 1}))
	idx = buildIndex(t, row("a", map[int]float64{1: 0.4}, nil), row("b", map[int]float64{1: 0.8}, nil))
	mirror, _ = json.Marshal(mirrorRows(
		map[string]any{"engine": "a", "name": "A", "index": 40.0},
		map[string]any{"engine": "b", "name": "B", "index": 80.0}))
	return meth, idx, mirror
}

// Data declaring another edition used to be relabelled as the pinned one.
// The methodology's edition.id, and the index file's suite.panel_id against
// the methodology's panel_id, must both match or Load refuses the data.
func TestLoadRejectsPayloadFromAnotherEdition(t *testing.T) {
	t.Setenv("CONCLAVE_NO_PRICING", "")
	meth, idx, mirror := goodGeneration(t)
	cases := map[string]func(s *swapServer){
		"methodology edition.id": func(s *swapServer) {
			s.meth = mutate(t, meth, func(m map[string]any) { m["edition"].(map[string]any)["id"] = "v9.0.0" })
		},
		"index suite.panel_id": func(s *swapServer) {
			s.idx = mutate(t, idx, func(m map[string]any) { m["suite"].(map[string]any)["panel_id"] = "decision-index-9.0.0" })
		},
		"index without suite": func(s *swapServer) {
			s.idx = mutate(t, idx, func(m map[string]any) { delete(m, "suite") })
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			s := newSwapServer(t, meth, idx, mirror)
			s.set(corrupt)
			if b, err := Load(context.Background(), s.opts(t.TempDir(), false)); err == nil {
				t.Fatalf("foreign edition accepted and labelled %s: %+v", Edition, b.Entries)
			}
		})
	}
}

// A mirror built from another edition is not a cross-check of this one: the
// load still succeeds, without a Check, with a warning.
func TestLoadSkipsCrossCheckForMirrorOfAnotherEdition(t *testing.T) {
	t.Setenv("CONCLAVE_NO_PRICING", "")
	meth, idx, mirror := goodGeneration(t)
	s := newSwapServer(t, meth, idx, mutate(t, mirror, func(m map[string]any) {
		m["upstream"].(map[string]any)["label"] = "Decision Index 9.0"
	}))
	b, err := Load(context.Background(), s.opts(t.TempDir(), false))
	if err != nil {
		t.Fatalf("a foreign mirror must not fail Load: %v", err)
	}
	if b.Check != nil || !anyContains(b.Warnings, "mirror") {
		t.Errorf("Check = %+v, warnings %q; want nil Check and a mirror warning", b.Check, b.Warnings)
	}
}

// Edition-specific cache keys: a later edition bump must never read this
// edition's cached files under the same name.
func TestCacheFilesAreKeyedByEdition(t *testing.T) {
	t.Setenv("CONCLAVE_NO_PRICING", "")
	meth, idx, mirror := goodGeneration(t)
	s := newSwapServer(t, meth, idx, mirror)
	dir := t.TempDir()
	if _, err := Load(context.Background(), s.opts(dir, false)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{MethodologyFile, IndexFile} {
		if _, err := os.Stat(filepath.Join(dir, Edition, f)); err != nil {
			t.Errorf("%s not cached under %s/: %v", f, Edition, err)
		}
	}
}

// A refresh that returns valid JSON but a corrupt edition ({}) used to
// overwrite a good cache before validation, poisoning every later Load. The
// whole generation must validate and compute before anything is replaced.
func TestLoadCorruptRefreshKeepsGoodCache(t *testing.T) {
	t.Setenv("CONCLAVE_NO_PRICING", "")
	meth, idx, mirror := goodGeneration(t)
	s := newSwapServer(t, meth, idx, mirror)
	dir := t.TempDir()
	if _, err := Load(context.Background(), s.opts(dir, false)); err != nil {
		t.Fatal(err)
	}
	s.set(func(s *swapServer) { s.meth = []byte("{}"); s.mirror = []byte("{}") })
	b, err := Load(context.Background(), s.opts(dir, true))
	if err != nil {
		t.Fatalf("corrupt refresh with a good cache must serve the cache: %v", err)
	}
	if len(b.Entries) != 2 || b.Check == nil || !anyContains(b.Warnings, "refresh") {
		t.Errorf("want the cached board plus a refresh warning, got entries %d check %v warnings %q",
			len(b.Entries), b.Check, b.Warnings)
	}
	// And the cache itself is intact: a plain Load with the server gone works.
	s.srv.Close()
	if b, err := Load(context.Background(), s.opts(dir, false)); err != nil || len(b.Entries) != 2 {
		t.Fatalf("cache was poisoned by the corrupt refresh: %v", err)
	}
}

// Blank or duplicate engines used to join anything to anything: upstream
// (same,10),(same,90) against mirror (same,90) reported a clean match. Such
// rows are excluded from the join, by name, on both sides.
func TestCrossCheckExcludesBlankAndDuplicateEngines(t *testing.T) {
	b := &Board{Entries: []Entry{
		{Name: "Same1", Engine: "same", Index: 10},
		{Name: "Same2", Engine: "same", Index: 90},
		{Name: "Blank", Engine: "", Index: 40},
		{Name: "A", Engine: "a", Index: 40},
		{Name: "C", Engine: "c", Index: 50},
	}}
	mirror, _ := json.Marshal(mirrorRows(
		map[string]any{"engine": "same", "name": "Same", "index": 90.0},
		map[string]any{"engine": "", "name": "MirrorBlank", "index": 40.0},
		map[string]any{"engine": "a", "name": "A", "index": 40.0},
		map[string]any{"engine": "c", "name": "C1", "index": 50.0},
		map[string]any{"engine": "c", "name": "C2", "index": 99.0},
	))
	if err := b.crossCheck(mirror); err != nil {
		t.Fatal(err)
	}
	if b.Check == nil || b.Check.Compared != 1 {
		t.Fatalf("Check = %+v, want exactly one (a) compared", b.Check)
	}
	for _, name := range []string{"same", "c", "Blank", "MirrorBlank"} {
		if !anyContains(b.Warnings, name) {
			t.Errorf("no warning names %q: %q", name, b.Warnings)
		}
	}
	for _, e := range b.Entries {
		if e.FromMirror {
			t.Errorf("excluded mirror row %q appended as mirror-only", e.Name)
		}
	}
}

// An empty or schema-drifted mirror ({}) used to install a clean
// Check{Compared:0}. No joined rows means the cross-check did not run.
func TestCrossCheckWithNothingJoinedIsNotACheck(t *testing.T) {
	noShared, _ := json.Marshal(mirrorRows(map[string]any{"engine": "z", "name": "Z", "index": 1.0}))
	for name, mirror := range map[string][]byte{"empty object": []byte("{}"), "no shared engine": noShared} {
		t.Run(name, func(t *testing.T) {
			b := &Board{Entries: []Entry{{Name: "A", Engine: "a", Index: 40}}}
			_ = b.crossCheck(mirror)
			if b.Check != nil {
				t.Errorf("Check = %+v, want nil when nothing joined", *b.Check)
			}
			if !anyContains(b.Warnings, "cross-check") {
				t.Errorf("want a cross-check warning, got %q", b.Warnings)
			}
		})
	}
}

func TestCrossCheckWarnsWhenMirrorMissesUpstreamEngines(t *testing.T) {
	var es []Entry
	var rows []map[string]any
	for i := 0; i < 10; i++ {
		e := string(rune('a' + i))
		es = append(es, Entry{Name: e, Engine: e, Index: 50})
		if i < 8 { // 80% present, under the 90% floor
			rows = append(rows, map[string]any{"engine": e, "name": e, "index": 50.0})
		}
	}
	b := &Board{Entries: es}
	mirror, _ := json.Marshal(mirrorRows(rows...))
	if err := b.crossCheck(mirror); err != nil {
		t.Fatal(err)
	}
	if !anyContains(b.Warnings, "8 of 10") {
		t.Errorf("want a coverage warning naming 8 of 10, got %q", b.Warnings)
	}
}

// Rounding only our side hid real disagreement: ours 40.0149 vs mirror 39.99
// is a 0.0249 gap, over Tolerance, but rounding ours to 40.01 reported 0.02.
func TestCrossCheckComparesUnroundedIndex(t *testing.T) {
	b := &Board{Entries: []Entry{{Name: "A", Engine: "a", Index: 40.0149}}}
	mirror, _ := json.Marshal(mirrorRows(map[string]any{"engine": "a", "name": "A", "index": 39.99}))
	if err := b.crossCheck(mirror); err != nil {
		t.Fatal(err)
	}
	if b.Check == nil || !near(b.Check.MaxDelta, 0.0249, 1e-9) || len(b.Check.Mismatch) != 1 {
		t.Errorf("Check = %+v, want MaxDelta 0.0249 and A mismatched", b.Check)
	}
}

// CONCLAVE_DECISION_INDEX_TTL=+Inf used to overflow into a negative duration,
// so every cache read as stale. TTL is finite and clamped to [1m, 30d].
func TestTTLFromEnvRejectsNonFiniteAndClamps(t *testing.T) {
	for v, want := range map[string]time.Duration{
		"+Inf": defaultTTL, "NaN": defaultTTL, "-3": defaultTTL, "abc": defaultTTL,
		"1e300": maxTTL, "100000": maxTTL, "0.0001": minTTL, "2": 2 * time.Hour,
	} {
		t.Setenv("CONCLAVE_DECISION_INDEX_TTL", v)
		if got := ttlFromEnv(); got != want {
			t.Errorf("TTL %q = %v, want %v", v, got, want)
		}
	}
}

func anyContains(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
