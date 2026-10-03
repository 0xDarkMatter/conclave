// Package openrouter tests the wire and cache boundaries for OpenRouter's
// external benchmark sources. The fixtures are deliberately synthetic and
// small: ADR-018 forbids mirroring unlicensed upstream benchmark datasets.
//
// Each test pins a failure mode at ingestion, where loss of nullability,
// provenance, units, or credentials would contaminate every frontier view.
package openrouter

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFeedConvertsPerTokenPricingToPerMillion(t *testing.T) {
	srv := benchmarkServer(t, "test-key", func(source string) string {
		if source == "artificial-analysis" {
			return `{"data":[{"source":"artificial-analysis","model_permaslug":"anthropic/example-20261003","display_name":"Example","intelligence_index":57.6,"pricing":{"prompt":"0.0000044","completion":"0.000022"}}],"meta":{"as_of":"2026-10-01T12:00:40.401Z","citation":"Synthetic AA fixture derived from the 2026-10-03 probe shape."}}`
		}
		return `{"data":[],"meta":{"as_of":"2026-10-02T00:00:00Z"}}`
	})
	defer srv.Close()

	feed, err := loadTestFeed(t, srv, t.TempDir())
	if err != nil {
		t.Fatalf("LoadFeed: %v", err)
	}
	if len(feed.AA) != 1 {
		t.Fatalf("AA rows = %d, want 1", len(feed.AA))
	}
	if feed.AA[0].InputPerM == nil || *feed.AA[0].InputPerM != 4.4 {
		t.Fatalf("InputPerM = %v, want 4.4", feed.AA[0].InputPerM)
	}
	if feed.AA[0].OutputPerM == nil || *feed.AA[0].OutputPerM != 22 {
		t.Fatalf("OutputPerM = %v, want 22", feed.AA[0].OutputPerM)
	}
}

func TestFeedKeepsNullIndicesNil(t *testing.T) {
	srv := benchmarkServer(t, "test-key", func(source string) string {
		if source == "artificial-analysis" {
			return `{"data":[{"source":"artificial-analysis","model_permaslug":"anthropic/example","display_name":"Example","intelligence_index":null,"coding_index":null,"agentic_index":null,"pricing":null}],"meta":{"as_of":"2026-10-01T00:00:00Z"}}`
		}
		return `{"data":[],"meta":{"as_of":"2026-10-02T00:00:00Z"}}`
	})
	defer srv.Close()

	feed, err := loadTestFeed(t, srv, t.TempDir())
	if err != nil {
		t.Fatalf("LoadFeed: %v", err)
	}
	got := feed.AA[0]
	if got.Intelligence != nil || got.Coding != nil || got.Agentic != nil {
		t.Fatalf("null indices became values: intelligence=%v coding=%v agentic=%v", got.Intelligence, got.Coding, got.Agentic)
	}
	if got.InputPerM != nil || got.OutputPerM != nil {
		t.Fatalf("null pricing became values: input=%v output=%v", got.InputPerM, got.OutputPerM)
	}
}

func TestFeedSplitsSourcesWithTheirOwnAsOf(t *testing.T) {
	srv := benchmarkServer(t, "test-key", func(source string) string {
		switch source {
		case "artificial-analysis":
			return `{"data":[],"meta":{"as_of":"2026-10-01T12:00:40.401Z","citation":"AA citation"}}`
		case "openrouter":
			return `{"data":[{"source":"openrouter","model_permaslug":"openai/example","display_name":"Example","benchmark_type":"gpqa_diamond","accuracy":0.81,"accuracy_stddev":0.02,"avg_cost_per_task":0.0123,"total_tasks":198}],"meta":{"as_of":"2026-10-02T09:30:00Z","citation":"OpenRouter citation"}}`
		default:
			t.Fatalf("unexpected source query %q", source)
			return ""
		}
	})
	defer srv.Close()

	feed, err := loadTestFeed(t, srv, t.TempDir())
	if err != nil {
		t.Fatalf("LoadFeed: %v", err)
	}
	if feed.AAAsOf != "2026-10-01T12:00:40.401Z" || feed.EvalsAsOf != "2026-10-02T09:30:00Z" {
		t.Fatalf("source snapshots collapsed: AA=%q evals=%q", feed.AAAsOf, feed.EvalsAsOf)
	}
	if feed.Citation != "AA citation" {
		t.Fatalf("Citation = %q, want AA citation", feed.Citation)
	}
	if len(feed.Evals) != 1 || feed.Evals[0].Accuracy == nil || *feed.Evals[0].Accuracy != 0.81 {
		t.Fatalf("eval row not decoded: %+v", feed.Evals)
	}
}

func TestFeedWithoutKeyMakesNoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("CONCLAVE_OPENROUTER_BENCHMARKS_URL", srv.URL)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("CONCLAVE_NO_PRICING", "")

	_, err := LoadFeed(context.Background(), Options{CacheDir: t.TempDir(), HTTPClient: srv.Client()})
	if err == nil || !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("error = %v, want one naming OPENROUTER_API_KEY", err)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("requests = %d, want 0", got)
	}
}

func TestFeedCacheNeverStoresTheKey(t *testing.T) {
	const secret = "sk-or-secret-that-must-not-reach-disk"
	srv := benchmarkServer(t, secret, func(source string) string {
		return fmt.Sprintf(`{"data":[],"meta":{"as_of":"2026-10-03T00:00:00Z","source":%q}}`, source)
	})
	defer srv.Close()
	cacheDir := t.TempDir()
	t.Setenv("CONCLAVE_OPENROUTER_BENCHMARKS_URL", srv.URL)
	t.Setenv("CONCLAVE_NO_PRICING", "")

	if _, err := LoadFeed(context.Background(), Options{APIKey: secret, CacheDir: cacheDir, HTTPClient: srv.Client()}); err != nil {
		t.Fatalf("LoadFeed: %v", err)
	}
	err := filepath.WalkDir(cacheDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(body, []byte(secret)) {
			t.Errorf("cache file %s contains API key", entry.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect cache: %v", err)
	}
}

func TestDecisionModelsKeepAliasTargets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("output_modalities") != "decisions" {
			t.Errorf("output_modalities = %q, want decisions", r.URL.Query().Get("output_modalities"))
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"liquid/d1","canonical_slug":"liquid/d1-20260930","name":"LiquidAI: D1","context_length":65536,"architecture":{"output_modalities":["decisions"]},"pricing":{"prompt":"0.00000004","completion":"0"}},{"id":"~typesafe/jev-latest","alias_target":{"slug":"typesafe/jev-1.13"}}]}`))
	}))
	defer srv.Close()
	t.Setenv("CONCLAVE_OPENROUTER_MODELS_URL", srv.URL+"?output_modalities=decisions")
	t.Setenv("CONCLAVE_NO_PRICING", "")

	models, err := LoadDecisionModels(context.Background(), Options{CacheDir: t.TempDir(), HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("LoadDecisionModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2", len(models))
	}
	if models[0].InputPerM != 0.04 || models[0].ContextLength != 65536 {
		t.Fatalf("priced model = %+v", models[0])
	}
	if models[1].Slug != "~typesafe/jev-latest" || models[1].AliasTarget != "typesafe/jev-1.13" {
		t.Fatalf("alias model = %+v", models[1])
	}
}

func TestNoPricingEnvMakesNoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("CONCLAVE_NO_PRICING", "1")
	t.Setenv("CONCLAVE_OPENROUTER_BENCHMARKS_URL", srv.URL)
	t.Setenv("CONCLAVE_OPENROUTER_MODELS_URL", srv.URL)

	if _, err := LoadFeed(context.Background(), Options{APIKey: "unused", CacheDir: t.TempDir(), HTTPClient: srv.Client()}); err == nil || !strings.Contains(err.Error(), "CONCLAVE_NO_PRICING") {
		t.Fatalf("LoadFeed error = %v, want disabled error", err)
	}
	if _, err := LoadDecisionModels(context.Background(), Options{CacheDir: t.TempDir(), HTTPClient: srv.Client()}); err == nil || !strings.Contains(err.Error(), "CONCLAVE_NO_PRICING") {
		t.Fatalf("LoadDecisionModels error = %v, want disabled error", err)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("requests = %d, want 0", got)
	}
}

func TestFeedHTTPErrorSurfacesMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"User not found.","code":401}}`))
	}))
	defer srv.Close()
	t.Setenv("CONCLAVE_OPENROUTER_BENCHMARKS_URL", srv.URL)
	t.Setenv("CONCLAVE_NO_PRICING", "")

	_, err := LoadFeed(context.Background(), Options{APIKey: "bad-key", CacheDir: t.TempDir(), HTTPClient: srv.Client()})
	if err == nil || !strings.Contains(err.Error(), "User not found.") {
		t.Fatalf("error = %v, want response error.message", err)
	}
}

func benchmarkServer(t *testing.T, apiKey string, body func(source string) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantAuthorization := "Bearer " + apiKey
		if got := r.Header.Get("Authorization"); got != wantAuthorization {
			t.Errorf("Authorization = %q, want %q", got, wantAuthorization)
		}
		source := r.URL.Query().Get("source")
		if source == "" {
			t.Error("benchmarks request omitted source query")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body(source)))
	}))
}

func loadTestFeed(t *testing.T, srv *httptest.Server, cacheDir string) (*Feed, error) {
	t.Helper()
	t.Setenv("CONCLAVE_OPENROUTER_BENCHMARKS_URL", srv.URL)
	t.Setenv("CONCLAVE_NO_PRICING", "")
	return LoadFeed(context.Background(), Options{APIKey: "test-key", CacheDir: cacheDir, HTTPClient: srv.Client()})
}

// A cache written before DecisionModel.Priced existed decodes every row as
// unpriced; it must never be read as current (seen live 2026-10-03: Kev and
// Tev1 showed no price until --refresh).
func TestPrePricedCacheFileIsNotRead(t *testing.T) {
	dir := t.TempDir()
	old := `{"fetched_at":"` + time.Now().UTC().Format(time.RFC3339) + `","models":[{"Slug":"jaredpalmer/kev-4b","InputPerM":0.042}]}`
	if err := os.WriteFile(filepath.Join(dir, "decision-models.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"data":[{"id":"jaredpalmer/kev-4b","pricing":{"prompt":"0.000000042","completion":"0"}}]}`))
	}))
	defer srv.Close()
	t.Setenv("CONCLAVE_OPENROUTER_MODELS_URL", srv.URL)
	t.Setenv("CONCLAVE_NO_PRICING", "")
	models, err := LoadDecisionModels(context.Background(), Options{CacheDir: dir})
	if err != nil {
		t.Fatalf("LoadDecisionModels: %v", err)
	}
	if hits != 1 || len(models) != 1 || !models[0].Priced {
		t.Fatalf("hits=%d models=%+v; want a fresh fetch with Priced=true", hits, models)
	}
}

// Refuted 2026-10-03: an evals-only outage aborted the whole feed although
// Artificial Analysis supplied every score; it must be advisory and uncached.
func TestFeedEvalsDownKeepsAAAndIsNotCached(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("source") == "openrouter" {
			http.Error(w, `{"error":{"message":"boom","code":500}}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"source":"artificial-analysis","model_permaslug":"a/b-20260101","display_name":"B","intelligence_index":50,"pricing":{"prompt":"0.000001","completion":"0.000002"}}],"meta":{"as_of":"2026-10-01","source":"artificial-analysis"}}`))
	}))
	defer srv.Close()
	t.Setenv("CONCLAVE_OPENROUTER_BENCHMARKS_URL", srv.URL)
	t.Setenv("CONCLAVE_NO_PRICING", "")
	feed, err := LoadFeed(context.Background(), Options{APIKey: "sk-or-test", CacheDir: dir})
	if err != nil {
		t.Fatalf("LoadFeed: %v", err)
	}
	if len(feed.AA) != 1 || feed.EvalsError == "" {
		t.Fatalf("feed = %+v; want AA kept and EvalsError set", feed)
	}
	if _, statErr := os.Stat(filepath.Join(dir, feedCacheFile)); statErr == nil {
		t.Fatal("partial feed was cached")
	}
}
