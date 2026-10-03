// Tests for Load: fetch-or-cache, mirror cross-check, mirror-only rows and the
// advisory failure rules (ADR-018, ADR-009). All HTTP is httptest; all data is
// synthetic (the Decision Index carries no licence, so none is committed).
package decisionindex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	return map[string]any{"generated_utc": "2026-10-01", "models": rows}
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
	srv := fixtureServer(t, mirrorRows(), http.StatusOK)
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

func anyContains(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
