// Package decisionindex recomputes the community Decision Index for decision
// models from its upstream Hugging Face Space, pinned to one edition, and
// cross-checks it against Cloudflare's mirror (ADR-018). It never runs a
// model: it combines published per-benchmark scores with the published
// methodology.
//
// Invariants:
//   - Advisory like internal/pricing (ADR-009): a missing mirror is a warning;
//     missing upstream data is an error the caller reads as "no decision
//     scores", never a fatal condition. CONCLAVE_NO_PRICING=1 switches it off.
//   - Edition-pinned: the file names below name the edition; upstream's moving
//     index.json is deliberately not followed.
//   - Never commit upstream or mirror data: the Space has no licence. Tests use
//     synthetic fixtures and cited, hand-typed numbers.
//
// Files: decisionindex.go (contract + Load), compute.go (the formula),
// fetch.go (cache and HTTP). Contract stub fixed by the orchestrator
// 2026-10-03: exported names may be added to, never renamed.
package decisionindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
)

// Edition is the pinned Decision Index edition.
//
// GUARD: moving it is a deliberate edition bump, not a version tweak. Change
// Edition, IndexFile and MethodologyFile together, re-type the new
// methodology's worked example into TestComputeReproducesWorkedExample, re-run
// the live mirror check by hand, and re-read the new `index.steps` against
// compute.go. Following upstream's unversioned index.json instead would let
// an upstream formula change silently move Conclave's numbers (ADR-018).
const Edition = "v0.2.1"

// Upstream and mirror locations for the pinned edition.
const (
	UpstreamBase    = "https://huggingface.co/spaces/multimodalart/jev-decision-index/resolve/main/data/"
	IndexFile       = "index-v0.2.1.json"
	MethodologyFile = "methodology-v0.2.1.json"
	MirrorURL       = "https://clef-evals.workers-ai-mle.workers.dev/data/leaderboard.json"
)

// Entry is one model's recomputed (or, for mirror-only rows, mirrored) result.
type Entry struct {
	Name         string  // board display name, e.g. "Kev 4B"
	Engine       string  // upstream engine id; the stable key shared with the mirror
	Index        float64 // 0..100, chance-corrected, edition formula
	Areas        map[string]float64
	ECE          *float64 // calibration error, when published
	MedianMs     *float64
	P95Ms        *float64
	Benchmarks   int  // benchmarks with a score (coverage)
	SelfReported bool // true only for mirror-only rows (Clef, Clef-flash)
	FromMirror   bool // true when the row exists only in the mirror
}

// Check is the result of comparing recomputed indices to the mirror's for
// models present in both.
type Check struct {
	Compared int
	MaxDelta float64  // largest |ours - mirror|
	Mismatch []string // names whose delta exceeds Tolerance
}

// Tolerance is the largest |ours - mirror| index difference accepted as
// agreement (both sides round to 2 decimals).
const Tolerance = 0.02

// Board is the loaded, computed, cross-checked edition.
type Board struct {
	Edition      string
	GeneratedUTC string // upstream index file's generated_utc
	Entries      []Entry
	Check        *Check // nil when the mirror was unavailable
	Warnings     []string
}

// Options controls fetching and caching, mirroring internal/pricing.Options.
type Options struct {
	CacheDir   string        // default $XDG_CACHE_HOME/conclave/decision-index/
	TTL        time.Duration // default 24h
	Refresh    bool          // ignore the cache
	HTTPClient interface{}   // *http.Client; interface{} keeps this stub import-free

	UpstreamBase string // overrides UpstreamBase (tests); must end in "/"
	MirrorURL    string // overrides MirrorURL (tests)
}

// ErrNotImplemented was returned by the contract stub. Nothing returns it now;
// it stays because the stub's exported names are fixed.
var ErrNotImplemented = errors.New("decisionindex: not implemented")

// === Load ===

// mirrorFile is the mirror's cache name; the upstream files keep their own
// (edition-bearing) names in the cache directory.
const mirrorFile = "mirror-leaderboard.json"

// Load fetches (or reads cached) upstream files and the mirror, computes,
// cross-checks, and appends mirror-only self-reported rows. Advisory: a nil
// Board with an error means "no decision scores", never a fatal condition
// for the caller.
func Load(ctx context.Context, opts Options) (*Board, error) {
	if pricing.Disabled() {
		return nil, errors.New("decision index disabled: CONCLAVE_NO_PRICING is set")
	}
	f := fetcher{dir: opts.CacheDir, ttl: opts.TTL, refresh: opts.Refresh}
	if f.dir == "" {
		// Beside the pricing cache, so the XDG rules live in one place.
		f.dir = filepath.Join(pricing.CacheDir(), "decision-index")
	}
	if f.ttl == 0 {
		f.ttl = ttlFromEnv()
	}
	if c, ok := opts.HTTPClient.(*http.Client); ok && c != nil {
		f.client = c
	} else {
		f.client = &http.Client{Timeout: fetchTimeout}
	}
	base := opts.UpstreamBase
	if base == "" {
		base = UpstreamBase
	}
	mirrorURL := opts.MirrorURL
	if mirrorURL == "" {
		mirrorURL = MirrorURL
	}

	b := &Board{Edition: Edition}
	meth, warn, err := f.get(ctx, MethodologyFile, base+MethodologyFile)
	if err != nil {
		return nil, fmt.Errorf("decision index %s methodology unavailable: %w", Edition, err)
	}
	b.warn(warn)
	idx, warn, err := f.get(ctx, IndexFile, base+IndexFile)
	if err != nil {
		return nil, fmt.Errorf("decision index %s scores unavailable: %w", Edition, err)
	}
	b.warn(warn)
	if b.Entries, err = Compute(meth, idx); err != nil {
		return nil, fmt.Errorf("decision index %s: %w", Edition, err)
	}
	var head struct {
		GeneratedUTC string `json:"generated_utc"`
	}
	_ = json.Unmarshal(idx, &head) // Compute already proved it decodes
	b.GeneratedUTC = head.GeneratedUTC

	mb, warn, err := f.get(ctx, mirrorFile, mirrorURL)
	if err != nil {
		b.warn("decision index mirror unavailable, cross-check skipped: " + err.Error())
		return b, nil
	}
	b.warn(warn)
	if err := b.crossCheck(mb); err != nil {
		b.warn("decision index mirror unreadable, cross-check skipped: " + err.Error())
	}
	return b, nil
}

func (b *Board) warn(w string) {
	if w != "" {
		b.Warnings = append(b.Warnings, w)
	}
}

// mirrorWire is the subset of the mirror's leaderboard.json we read. Rows join
// to upstream by engine, not name: the mirror relabels some models (upstream
// "pplx-decider-v1-27b" is the mirror's "AutoJev-27B") and suffixes others
// with a quantisation ("[bf16]"). Checked 2026-10-03: every upstream engine
// appears in the mirror, and only clef and clef-flash are mirror-only.
type mirrorWire struct {
	Models []struct {
		Engine  string   `json:"engine"`
		Name    string   `json:"name"`
		Index   *float64 `json:"index"`
		ECE     *float64 `json:"ece"`
		Latency *struct {
			Median *float64 `json:"median"`
			P95    *float64 `json:"p95"`
		} `json:"latency"`
		Areas         map[string]float64 `json:"areas"`
		PanelCoverage int                `json:"panel_coverage"`
	} `json:"models"`
}

// crossCheck compares the recomputed index with the mirror's for engines in
// both, then appends the mirror-only rows.
func (b *Board) crossCheck(mirror []byte) error {
	var m mirrorWire
	if err := json.Unmarshal(mirror, &m); err != nil {
		return err
	}
	ours := make(map[string]Entry, len(b.Entries))
	for _, e := range b.Entries {
		ours[e.Engine] = e
	}
	c := &Check{}
	for _, r := range m.Models {
		if r.Index == nil {
			continue // unscored on the mirror: nothing to compare or plot
		}
		e, ok := ours[r.Engine]
		if !ok {
			// Mirror-only rows cannot be recomputed from upstream, so they are
			// the vendor's own claim: always SelfReported (ADR-018).
			me := Entry{Name: r.Name, Engine: r.Engine, Index: *r.Index, Areas: r.Areas, ECE: r.ECE,
				Benchmarks: r.PanelCoverage, SelfReported: true, FromMirror: true}
			if r.Latency != nil {
				me.MedianMs, me.P95Ms = r.Latency.Median, r.Latency.P95
			}
			b.Entries = append(b.Entries, me)
			continue
		}
		// The mirror publishes 2 dp; compare like with like.
		d := math.Abs(math.Round(e.Index*100)/100 - *r.Index)
		c.Compared++
		c.MaxDelta = math.Max(c.MaxDelta, d)
		if d > Tolerance+1e-9 {
			c.Mismatch = append(c.Mismatch, e.Name)
		}
	}
	sort.Strings(c.Mismatch)
	b.Check = c
	if len(c.Mismatch) > 0 {
		b.warn(fmt.Sprintf("decision index %s: recomputed index differs from the mirror by more than %.2f for %s",
			Edition, Tolerance, strings.Join(c.Mismatch, ", ")))
	}
	return nil
}
