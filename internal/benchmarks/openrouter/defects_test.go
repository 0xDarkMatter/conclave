// Pins for the adversarial-review defects refuted in this package (2026-10-03).
// Every test is named for the defect it blocks, not the code it exercises: a
// 200 that blanks the catalog for a day, an error string that leaks the API
// key, an unpriced model plotted as free, a row that leaks across sources,
// float noise in per-million prices, and a TTL that silently disables the
// cache. Adjudication numbers in the comments match the review's numbering;
// new ingestion rules get pinned here in the same shape.
package openrouter

import (
	"context"
	"encoding/json"
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

// Adjudication 1 (high): OpenRouter answers some faults with HTTP 200 and an
// error envelope (or no data member at all). That must surface as an error and
// must never be cached: an empty catalog cached for 24h blanks every frontier.
func TestModels200WithoutDataIsAnErrorAndIsNotCached(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"error envelope", `{"error":{"message":"Internal error","code":500}}`},
		{"null data", `{"data":null}`},
		{"data absent", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			t.Setenv("CONCLAVE_OPENROUTER_MODELS_URL", srv.URL)
			t.Setenv("CONCLAVE_NO_PRICING", "")
			cacheDir := t.TempDir()

			models, err := LoadDecisionModels(context.Background(), Options{CacheDir: cacheDir, HTTPClient: srv.Client()})
			if err == nil {
				t.Fatalf("err = nil, want the blank catalog refused")
			}
			if len(models) != 0 {
				t.Fatalf("models = %d rows, want none", len(models))
			}
			if _, err := LoadDecisionModels(context.Background(), Options{CacheDir: cacheDir, HTTPClient: srv.Client()}); err == nil {
				t.Fatalf("second call err = nil, want the fault to stay an error")
			}
			if got := hits.Load(); got != 2 {
				t.Fatalf("requests = %d, want 2 (a cached blank would have made it 1)", got)
			}
			assertCacheDirEmpty(t, cacheDir)
		})
	}
}

// Adjudication 1 (high): the feed path is refused the same way — a 200 error
// envelope on a benchmarks source is a fetch failure, not empty scores.
func TestFeed200WithoutDataIsAnErrorAndIsNotCached(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"message":"Internal error","code":500}}`))
	}))
	defer srv.Close()
	t.Setenv("CONCLAVE_OPENROUTER_BENCHMARKS_URL", srv.URL)
	t.Setenv("CONCLAVE_NO_PRICING", "")
	cacheDir := t.TempDir()

	feed, err := LoadFeed(context.Background(), Options{APIKey: "test-key", CacheDir: cacheDir, HTTPClient: srv.Client()})
	if err == nil {
		t.Fatalf("err = nil, want the blank feed refused")
	}
	if feed != nil {
		t.Fatalf("feed = %+v, want nil", feed)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	assertCacheDirEmpty(t, cacheDir)
}

// Adjudication 1: a source whose meta carries no as_of has no snapshot date;
// unsnapshotted data is refused rather than cached or cited (ADR-018).
func TestFeedSourceWithoutSnapshotDateIsRefused(t *testing.T) {
	srv := benchmarkServer(t, "test-key", func(source string) string {
		if source == "artificial-analysis" {
			// Rows are fine; only the snapshot date is missing.
			return `{"data":[{"source":"artificial-analysis","model_permaslug":"anthropic/example","display_name":"Example"}],"meta":{"citation":"c"}}`
		}
		return `{"data":[],"meta":{"as_of":"2026-10-02T00:00:00Z"}}`
	})
	defer srv.Close()
	cacheDir := t.TempDir()

	_, err := loadTestFeed(t, srv, cacheDir)
	if err == nil || !strings.Contains(err.Error(), "as_of") {
		t.Fatalf("error = %v, want one naming the missing as_of", err)
	}
	assertCacheDirEmpty(t, cacheDir)
}

// Adjudication 2: a 401 body can echo the Authorization header verbatim; the
// key must come back redacted, and vendor text is capped at 300 characters so
// a hostile body cannot balloon a caller's warning.
func TestVendorErrorsAreRedactedAndCapped(t *testing.T) {
	const key = "redaction-sentinel-not-a-real-credential-KEY-LEAK" // deliberately not key-shaped: secret scanners flag sk-or-… literals
	serve := func(message func(r *http.Request) string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":{"message":%q}}`, message(r))))
		}))
	}
	t.Run("key redacted", func(t *testing.T) {
		srv := serve(func(r *http.Request) string {
			return "Invalid Authorization header: " + r.Header.Get("Authorization")
		})
		defer srv.Close()
		t.Setenv("CONCLAVE_OPENROUTER_BENCHMARKS_URL", srv.URL)
		t.Setenv("CONCLAVE_NO_PRICING", "")

		_, err := LoadFeed(context.Background(), Options{APIKey: key, CacheDir: t.TempDir(), HTTPClient: srv.Client()})
		if err == nil {
			t.Fatalf("err = nil, want the 401 surfaced")
		}
		if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "Bearer "+key) {
			t.Fatalf("error leaks the API key: %v", err)
		}
		if !strings.Contains(err.Error(), "[redacted]") {
			t.Fatalf("error does not mark the redaction: %v", err)
		}
	})
	t.Run("message capped", func(t *testing.T) {
		srv := serve(func(*http.Request) string { return strings.Repeat("q", 500) })
		defer srv.Close()
		t.Setenv("CONCLAVE_OPENROUTER_BENCHMARKS_URL", srv.URL)
		t.Setenv("CONCLAVE_NO_PRICING", "")

		_, err := LoadFeed(context.Background(), Options{APIKey: key, CacheDir: t.TempDir(), HTTPClient: srv.Client()})
		if err == nil {
			t.Fatalf("err = nil, want the 401 surfaced")
		}
		// No 'q' occurs in the wrapping text, so the count is the vendor's chars.
		if got := strings.Count(err.Error(), "q"); got > 300 {
			t.Fatalf("vendor message delivered %d chars, want <= 300", got)
		}
	})
}

// Adjudications 3, 4, 6 and 7 for the decision path: a price the source did
// not publish is unknown, never 0-as-free. Only plain, finite, non-negative
// decimals price a row; "0"/"0" stays priced (genuinely free), and the live
// per-token values 0.000000042 / 0.00000005 must survive as exactly 0.042 /
// 0.05 per million.
func TestUnpricedDecisionModelsNeverLookFree(t *testing.T) {
	rows := []struct {
		slug       string
		pricing    string // raw JSON for the pricing member; "OMIT" leaves it out
		wantPriced bool
		wantIn     float64
		wantOut    float64
	}{
		{"x/nullpricing", `null`, false, 0, 0},
		{"x/nopricekey", `OMIT`, false, 0, 0},
		{"x/empty", `{"prompt":"","completion":""}`, false, 0, 0},
		{"x/nan-inf", `{"prompt":"NaN","completion":"Inf"}`, false, 0, 0},
		{"x/hex", `{"prompt":"0x1p-20","completion":"0"}`, false, 0, 0},
		{"x/exponent", `{"prompt":"1e-8","completion":"0"}`, false, 0, 0},
		{"x/negative-completion", `{"prompt":"0.5","completion":"-1"}`, false, 0, 0},
		{"x/sentinel", `{"prompt":"-1","completion":"-1"}`, false, 0, 0},
		{"x/free", `{"prompt":"0","completion":"0"}`, true, 0, 0},
		{"x/rounding", `{"prompt":"0.000000042","completion":"0.00000005"}`, true, 0.042, 0.05},
	}
	var data strings.Builder
	data.WriteByte('[')
	for i, row := range rows {
		if i > 0 {
			data.WriteByte(',')
		}
		if row.pricing == "OMIT" {
			fmt.Fprintf(&data, `{"id":%q,"name":%q}`, row.slug, row.slug)
			continue
		}
		fmt.Fprintf(&data, `{"id":%q,"name":%q,"pricing":%s}`, row.slug, row.slug, row.pricing)
	}
	data.WriteByte(']')
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":%s}`, data.String())
	}))
	defer srv.Close()
	t.Setenv("CONCLAVE_OPENROUTER_MODELS_URL", srv.URL)
	t.Setenv("CONCLAVE_NO_PRICING", "")

	models, err := LoadDecisionModels(context.Background(), Options{CacheDir: t.TempDir(), HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("LoadDecisionModels: %v (a NaN price must not break the cache write either)", err)
	}
	bySlug := map[string]DecisionModel{}
	for _, m := range models {
		bySlug[m.Slug] = m
	}
	for _, row := range rows {
		got, ok := bySlug[row.slug]
		if !ok {
			t.Fatalf("%s missing from catalog", row.slug)
		}
		if got.Priced != row.wantPriced {
			t.Errorf("%s: Priced = %v, want %v", row.slug, got.Priced, row.wantPriced)
		}
		if got.InputPerM != row.wantIn {
			t.Errorf("%s: InputPerM = %v, want %v", row.slug, got.InputPerM, row.wantIn)
		}
		if got.OutputPerM != row.wantOut {
			t.Errorf("%s: OutputPerM = %v, want %v", row.slug, got.OutputPerM, row.wantOut)
		}
	}
}

// Adjudication 4 on the AA path: OpenRouter's "-1" dynamic-price sentinel (and
// non-finite spellings) stay nil pointers, never -1e6 values that would sit at
// the front of every price-performance frontier.
func TestAANegativeSentinelPricesStayNil(t *testing.T) {
	srv := benchmarkServer(t, "test-key", func(source string) string {
		if source == "artificial-analysis" {
			return `{"data":[{"source":"artificial-analysis","model_permaslug":"typesafe/jev-router","display_name":"Router","pricing":{"prompt":"-1","completion":"-1"}},{"source":"artificial-analysis","model_permaslug":"x/nan","display_name":"NaN","pricing":{"prompt":"NaN","completion":"Inf"}}],"meta":{"as_of":"2026-10-01T00:00:00Z"}}`
		}
		return `{"data":[],"meta":{"as_of":"2026-10-02T00:00:00Z"}}`
	})
	defer srv.Close()

	feed, err := loadTestFeed(t, srv, t.TempDir())
	if err != nil {
		t.Fatalf("LoadFeed: %v", err)
	}
	if len(feed.AA) != 2 {
		t.Fatalf("AA rows = %d, want 2 (rows survive; only their prices are unknown)", len(feed.AA))
	}
	for _, row := range feed.AA {
		if row.InputPerM != nil || row.OutputPerM != nil {
			t.Fatalf("%s: sentinel price became a value: input=%v output=%v", row.Slug, row.InputPerM, row.OutputPerM)
		}
	}
}

// Adjudication 5: a row belongs to a source only when its own source field
// says so exactly — a response that ignored ?source= must not leak foreign or
// sourceless rows, and its as_of/citation describe another scope, so they are
// dropped. Only when NO row survives is the source an error.
func TestFeedRowsRequireExactSourceMatch(t *testing.T) {
	mixed := `{"data":[
		{"source":"artificial-analysis","model_permaslug":"a/keep","display_name":"A"},
		{"source":"openrouter","model_permaslug":"o/row","display_name":"O"},
		{"source":"design-arena","model_permaslug":"d/row","display_name":"D"},
		{"model_permaslug":"n/nosrc","display_name":"N"}
	],"meta":{"as_of":"X","source":"all"}}`
	ignoreFilter := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
	}

	t.Run("surviving rows keep no snapshot", func(t *testing.T) {
		srv := ignoreFilter(mixed)
		defer srv.Close()
		feed, err := loadTestFeed(t, srv, t.TempDir())
		if err != nil {
			t.Fatalf("LoadFeed: %v (rows survived; the mismatch is not fatal)", err)
		}
		if len(feed.AA) != 1 || feed.AA[0].Slug != "a/keep" {
			t.Fatalf("AA = %+v, want only a/keep", feed.AA)
		}
		if len(feed.Evals) != 1 || feed.Evals[0].Slug != "o/row" {
			t.Fatalf("Evals = %+v, want only o/row", feed.Evals)
		}
		if feed.AAAsOf != "" || feed.EvalsAsOf != "" || feed.Citation != "" {
			t.Fatalf("foreign-scope meta kept: AAAsOf=%q EvalsAsOf=%q Citation=%q, want all empty", feed.AAAsOf, feed.EvalsAsOf, feed.Citation)
		}
	})

	t.Run("no surviving rows is an error", func(t *testing.T) {
		srv := ignoreFilter(`{"data":[{"source":"design-arena","model_permaslug":"d/only","display_name":"D"}],"meta":{"as_of":"X","source":"all"}}`)
		defer srv.Close()
		_, err := loadTestFeed(t, srv, t.TempDir())
		if err == nil {
			t.Fatalf("err = nil, want the ignored source filter refused")
		}
	})
}

// Adjudication 7: multiplying a binary float by 1e6 adds noise (the live
// catalog really returns 0.041999999999999996 for a 0.000000042 per-token
// price); per-million prices must round to 1e-9 USD so 0.042 is 0.042.
func TestAAPerMillionPricesRoundToNineDecimals(t *testing.T) {
	srv := benchmarkServer(t, "test-key", func(source string) string {
		if source == "artificial-analysis" {
			return `{"data":[{"source":"artificial-analysis","model_permaslug":"x/round","display_name":"Round","pricing":{"prompt":"0.000000042","completion":"0.00000005"}}],"meta":{"as_of":"2026-10-01T00:00:00Z"}}`
		}
		return `{"data":[],"meta":{"as_of":"2026-10-02T00:00:00Z"}}`
	})
	defer srv.Close()

	feed, err := loadTestFeed(t, srv, t.TempDir())
	if err != nil {
		t.Fatalf("LoadFeed: %v", err)
	}
	if got := feed.AA[0].InputPerM; got == nil || *got != 0.042 {
		t.Fatalf("InputPerM = %v, want exactly 0.042", got)
	}
	if got := feed.AA[0].OutputPerM; got == nil || *got != 0.05 {
		t.Fatalf("OutputPerM = %v, want exactly 0.05", got)
	}
}

// Adjudication 8: CONCLAVE_BENCHMARKS_TTL values that overflow time.Duration
// ("Inf", "1e300") used to turn into a negative TTL, silently disabling the
// cache; every resolved TTL is clamped into [1 minute, 30 days].
func TestTTLOutOfRangeIsClamped(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want time.Duration
	}{
		{"Inf", 30 * 24 * time.Hour},
		{"1e300", 30 * 24 * time.Hour},
		{"1000000", 30 * 24 * time.Hour},
		{"0.0001", time.Minute}, // 0.36s would make every entry instantly stale-loopable
		{"2", 2 * time.Hour},    // in-range values pass through untouched
	} {
		t.Setenv("CONCLAVE_BENCHMARKS_TTL", tc.env)
		if got := ttlFromEnv(); got != tc.want {
			t.Errorf("ttlFromEnv(%q) = %v, want %v", tc.env, got, tc.want)
		}
	}
	settings, err := resolveSettings(Options{TTL: 100 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("resolveSettings: %v", err)
	}
	if settings.ttl != 30*24*time.Hour {
		t.Errorf("opts TTL 100d resolved to %v, want clamped to 30d", settings.ttl)
	}
}

// Adjudication 8: a future-dated fetched_at (clock skew or a hand-edited
// cache) must not make an entry immortal — more than 5 minutes ahead is stale.
func TestFutureDatedCacheEntryIsStale(t *testing.T) {
	if cacheFresh(time.Now().Add(time.Hour), time.Hour) {
		t.Errorf("cacheFresh(future) = true, want stale")
	}
	if !cacheFresh(time.Now().Add(maxClockSkew-time.Minute), time.Hour) {
		t.Errorf("cacheFresh(within skew) = false, want fresh")
	}
	if !cacheFresh(time.Now().Add(-time.Minute), time.Hour) {
		t.Errorf("cacheFresh(1m ago) = false, want fresh")
	}

	// End to end: a cache file stamped 2099 must not suppress the refetch.
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"fresh/new","name":"Fresh","pricing":{"prompt":"0.000000042","completion":"0"}}]}`))
	}))
	defer srv.Close()
	t.Setenv("CONCLAVE_OPENROUTER_MODELS_URL", srv.URL)
	t.Setenv("CONCLAVE_NO_PRICING", "")
	cacheDir := t.TempDir()
	stale, err := json.Marshal(modelsCache{
		FetchedAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
		Models:    []DecisionModel{{Slug: "stale/old"}},
	})
	if err != nil {
		t.Fatalf("marshal stale cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, modelsCacheFile), stale, 0o644); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}

	models, err := LoadDecisionModels(context.Background(), Options{CacheDir: cacheDir, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("LoadDecisionModels: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1 (the future-dated entry must not count as fresh)", got)
	}
	if len(models) != 1 || models[0].Slug != "fresh/new" {
		t.Fatalf("models = %+v, want the refetched fresh/new row", models)
	}
}

// assertCacheDirEmpty fails when any regular file survived under dir: every
// refused response in this file must also have left nothing cached.
func assertCacheDirEmpty(t *testing.T, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		t.Errorf("cache file %s written for a refused response", entry.Name())
		return nil
	})
	if err != nil {
		t.Fatalf("inspect cache dir: %v", err)
	}
}
