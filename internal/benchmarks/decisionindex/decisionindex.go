// Package decisionindex recomputes the community Decision Index for decision
// models from its upstream Hugging Face Space, pinned to one edition, and
// cross-checks it against Cloudflare's mirror (ADR-018). It never runs a
// model: it combines published per-benchmark scores with the published
// methodology.
//
// CONTRACT STUB (orchestrator, 2026-10-03): the types and signatures below are
// fixed; the lane that owns this package implements them and may add
// unexported helpers and new files. Do not change exported names.
package decisionindex

import (
	"context"
	"errors"
	"time"
)

// Edition is the pinned Decision Index edition. Moving it is a deliberate
// change: update both file names, re-run the worked-example test.
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
	Name         string   // board display name, e.g. "Kev 4B"
	Index        float64  // 0..100, chance-corrected, edition formula
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
}

// ErrNotImplemented is returned by the stub until the lane lands.
var ErrNotImplemented = errors.New("decisionindex: not implemented")

// Compute applies the edition's methodology to the upstream index file. Pure:
// no I/O. The worked example in the methodology file is its fixed test.
func Compute(methodology, index []byte) ([]Entry, error) { return nil, ErrNotImplemented }

// Load fetches (or reads cached) upstream files and the mirror, computes,
// cross-checks, and appends mirror-only self-reported rows. Advisory: a nil
// Board with an error means "no decision scores", never a fatal condition
// for the caller.
func Load(ctx context.Context, opts Options) (*Board, error) { return nil, ErrNotImplemented }
