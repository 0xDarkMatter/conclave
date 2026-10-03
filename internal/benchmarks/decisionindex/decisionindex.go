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
//   - Edition-pinned: the file names below name the edition, AND the data's
//     own identity fields must match it (methodology edition.id, index
//     suite.panel_id, mirror upstream.label); upstream's moving index.json is
//     deliberately not followed.
//   - Never commit upstream or mirror data: the Space has no licence. Tests use
//     synthetic fixtures and cited, hand-typed numbers.
//
// Files: decisionindex.go (contract, Load, mirror join), compute.go (the
// formula and its input validation), fetch.go (cache and HTTP). Contract stub
// fixed by the orchestrator 2026-10-03: exported names may be added to, never
// renamed.
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
// models present in both, joined one-to-one on engine.
type Check struct {
	Compared int
	MaxDelta float64  // largest |ours - mirror|, ours unrounded
	Mismatch []string // names whose delta exceeds Tolerance
}

// Tolerance is the largest |ours - mirror| index difference accepted as
// agreement. Ours is compared unrounded against the mirror's published 2 dp
// value, so the mirror's own rounding (at most 0.005) fits well inside it.
const Tolerance = 0.02

// Board is the loaded, computed, cross-checked edition.
type Board struct {
	Edition      string
	GeneratedUTC string // upstream index file's generated_utc
	Entries      []Entry
	Check        *Check // nil when the cross-check did not run (no mirror, foreign or empty mirror, nothing joined)
	Warnings     []string
}

// Options controls fetching and caching, mirroring internal/pricing.Options.
type Options struct {
	CacheDir   string        // default $XDG_CACHE_HOME/conclave/decision-index/ (files go under <Edition>/)
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

// mirrorFile is the mirror's cache name, stored beside the edition's
// upstream files (fetcher.path); its identity is checked against Edition.
const mirrorFile = "mirror-leaderboard.json"

// minMirrorShare is the share of upstream engines the mirror must carry
// before the cross-check counts as covering the board; under it, a warning.
// Checked 2026-10-03: the v0.2.1 mirror carries all 71 upstream engines.
const minMirrorShare = 0.9

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

	g, warns, err := f.generation(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("decision index %s unavailable: %w", Edition, err)
	}
	b := &Board{Edition: Edition, GeneratedUTC: g.generatedUTC, Entries: g.entries}
	for _, w := range append(warns, g.warnings...) {
		b.warn(w)
	}

	mb, warn, err := f.get(ctx, mirrorFile, mirrorURL, validateMirror)
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

// === Mirror cross-check ===

// mirrorWire is the subset of the mirror's leaderboard.json we read. Rows join
// to upstream by engine, not name: the mirror relabels some models (upstream
// "pplx-decider-v1-27b" is the mirror's "AutoJev-27B") and suffixes others
// with a quantisation ("[bf16]"). Checked 2026-10-03: every upstream engine
// appears in the mirror, and only clef and clef-flash are mirror-only.
type mirrorWire struct {
	// upstream.label names the snapshot the mirror was built from; for
	// v0.2.1 it is "Decision Index 0.2.1".
	Upstream struct {
		Label string `json:"label"`
	} `json:"upstream"`
	Models []mirrorRow `json:"models"`
}

type mirrorRow struct {
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
}

// validateMirror gates the mirror before it is cached or used: it must name
// the pinned edition and list models. The label is matched word by word
// against Edition with or without its "v" ("0.2.1"), so "0.2.10" does not
// pass for "0.2.1". A mirror of another edition is not a cross-check of this
// one; Load then skips the cross-check with a warning and never fails.
func validateMirror(body []byte) error {
	var m mirrorWire
	if err := json.Unmarshal(body, &m); err != nil {
		return err
	}
	bare := strings.TrimPrefix(Edition, "v")
	named := false
	for _, w := range strings.Fields(m.Upstream.Label) {
		named = named || w == bare || w == Edition
	}
	if !named {
		return fmt.Errorf("mirror is built from %q, not edition %s", m.Upstream.Label, Edition)
	}
	if len(m.Models) == 0 {
		return errors.New("mirror lists no models")
	}
	return nil
}

// joinKeys returns the engines usable as one-to-one join keys (engine -> row
// index) and, for the warning, the names of rows excluded for a blank engine
// and the engines excluded as duplicates. A duplicated engine is dropped on
// EVERY copy: keeping one would let an unrelated row stand in for another.
func joinKeys(engines, names []string) (unique map[string]int, blank, dup []string) {
	count := map[string]int{}
	for _, e := range engines {
		count[e]++
	}
	unique = map[string]int{}
	seenDup := map[string]bool{}
	for i, e := range engines {
		switch {
		case e == "":
			blank = append(blank, names[i])
		case count[e] > 1:
			if !seenDup[e] {
				dup = append(dup, e)
				seenDup[e] = true
			}
		default:
			unique[e] = i
		}
	}
	sort.Strings(blank)
	sort.Strings(dup)
	return unique, blank, dup
}

func (b *Board) warnExcluded(side string, blank, dup []string) {
	var parts []string
	if len(blank) > 0 {
		parts = append(parts, "blank engine ("+strings.Join(blank, ", ")+")")
	}
	if len(dup) > 0 {
		parts = append(parts, "duplicate engine ("+strings.Join(dup, ", ")+")")
	}
	if len(parts) > 0 {
		b.warn(fmt.Sprintf("decision index %s rows excluded from the mirror cross-check: %s",
			side, strings.Join(parts, "; ")))
	}
}

// crossCheck compares the recomputed index with the mirror's on a one-to-one
// engine join, then appends the mirror-only rows. Check stays nil when
// nothing joined: an empty or schema-drifted mirror is a cross-check that did
// not run, not a clean one.
func (b *Board) crossCheck(mirror []byte) error {
	var m mirrorWire
	if err := json.Unmarshal(mirror, &m); err != nil {
		return err
	}
	if len(m.Models) == 0 {
		b.warn("decision index mirror lists no models, cross-check skipped")
		return nil
	}

	upEngines, upNames := make([]string, len(b.Entries)), make([]string, len(b.Entries))
	for i, e := range b.Entries {
		upEngines[i], upNames[i] = e.Engine, e.Name
	}
	ours, upBlank, upDup := joinKeys(upEngines, upNames)
	b.warnExcluded("upstream", upBlank, upDup)
	upAmbiguous := map[string]bool{}
	for _, e := range upDup {
		upAmbiguous[e] = true
	}

	mEngines, mNames := make([]string, len(m.Models)), make([]string, len(m.Models))
	for i, r := range m.Models {
		mEngines[i], mNames[i] = r.Engine, r.Name
	}
	theirs, mBlank, mDup := joinKeys(mEngines, mNames)
	b.warnExcluded("mirror", mBlank, mDup)

	c := &Check{}
	var mirrorOnly []Entry
	for _, i := range sortedIdx(theirs) {
		r := m.Models[i]
		if r.Index == nil {
			continue // unscored on the mirror: nothing to compare or plot
		}
		ui, ok := ours[r.Engine]
		if !ok {
			if upAmbiguous[r.Engine] {
				continue // ambiguous upstream: neither comparable nor mirror-only
			}
			// Mirror-only rows cannot be recomputed from upstream, so they are
			// the vendor's own claim: always SelfReported (ADR-018).
			me := Entry{Name: r.Name, Engine: r.Engine, Index: *r.Index, Areas: r.Areas, ECE: r.ECE,
				Benchmarks: r.PanelCoverage, SelfReported: true, FromMirror: true}
			if r.Latency != nil {
				me.MedianMs, me.P95Ms = r.Latency.Median, r.Latency.P95
			}
			mirrorOnly = append(mirrorOnly, me)
			continue
		}
		e := b.Entries[ui]
		// GUARD: ours stays UNROUNDED against the mirror's published 2 dp
		// value. Rounding only ours made the delta direction-dependent and hid
		// gaps of up to 0.005 over Tolerance, which already absorbs the
		// mirror's rounding. The epsilon only absorbs float noise.
		d := math.Abs(e.Index - *r.Index)
		c.Compared++
		c.MaxDelta = math.Max(c.MaxDelta, d)
		if d > Tolerance+1e-9 {
			c.Mismatch = append(c.Mismatch, e.Name)
		}
	}
	if c.Compared == 0 {
		// Nothing joined means this mirror is not describing the same board,
		// so its rows are not trusted as mirror-only additions either.
		b.warn(fmt.Sprintf("decision index mirror shares no scored engine with upstream %s, cross-check skipped", Edition))
		return nil
	}
	b.Entries = append(b.Entries, mirrorOnly...)

	present := 0
	for e := range ours {
		if _, ok := theirs[e]; ok {
			present++
		}
	}
	if float64(present) < minMirrorShare*float64(len(ours)) {
		b.warn(fmt.Sprintf("decision index mirror carries %d of %d upstream engines; the cross-check covers only those",
			present, len(ours)))
	}

	sort.Strings(c.Mismatch)
	b.Check = c
	if len(c.Mismatch) > 0 {
		b.warn(fmt.Sprintf("decision index %s: recomputed index differs from the mirror by more than %.2f for %s",
			Edition, Tolerance, strings.Join(c.Mismatch, ", ")))
	}
	return nil
}

// sortedIdx returns the map's row indices in source order, so mirror-only
// rows append in the mirror's own order rather than map order.
func sortedIdx(m map[string]int) []int {
	out := make([]int, 0, len(m))
	for _, i := range m {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}
