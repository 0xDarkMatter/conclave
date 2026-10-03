// Package openrouter reads OpenRouter's external quality and decision-model
// price data for frontier views (ADR-018): the keyed benchmarks feed
// (Artificial Analysis indices, OpenRouter evals, Design Arena) and the public
// catalog filtered to decision models, which internal/pricing's text-only
// catalog does not include. Advisory and cached like internal/pricing.
//
// CONTRACT STUB (orchestrator, 2026-10-03): the types and signatures below are
// fixed; the lane that owns this package implements them and may add
// unexported helpers and new files. Do not change exported names.
package openrouter

import (
	"context"
	"errors"
	"time"
)

const (
	BenchmarksURL     = "https://openrouter.ai/api/v1/benchmarks"
	DecisionModelsURL = "https://openrouter.ai/api/v1/models?output_modalities=decisions"
)

// AAScore is one Artificial Analysis row. Slug is OpenRouter's permaslug
// (dated, e.g. "anthropic/claude-opus-5.5-20260921").
type AAScore struct {
	Slug         string
	Name         string
	Intelligence *float64
	Coding       *float64
	Agentic      *float64
	InputPerM    *float64 // from the row's pricing, USD per million
	OutputPerM   *float64
}

// EvalScore is one OpenRouter-evals row (source "openrouter").
type EvalScore struct {
	Slug           string
	Name           string
	Benchmark      string // benchmark_type, e.g. "gpqa_diamond"
	Accuracy       *float64
	AvgCostPerTask *float64 // USD
	TotalTasks     int
}

// Feed is the loaded benchmarks feed.
type Feed struct {
	AA        []AAScore
	Evals     []EvalScore
	AAAsOf    string // meta.as_of for source=artificial-analysis; empty when upstream's meta.source named another scope
	EvalsAsOf string
	Citation  string // meta.citation for Artificial Analysis
	// EvalsError is set when the OpenRouter-evals sub-source failed while
	// Artificial Analysis loaded. Evals only feed the per_task cost basis, so
	// their failure is advisory, never a reason to drop the AA scores. A feed
	// with EvalsError is not cached, so the next run retries evals.
	EvalsError string
}

// DecisionModel is one catalog entry with output modality "decisions".
type DecisionModel struct {
	Slug string // canonical id as listed, e.g. "liquid/d1"
	Name string
	// Priced MUST be checked before the price fields are read. It is true only
	// when both the prompt and completion prices parsed to finite, non-negative
	// plain decimals. When it is false the row is unpriced — null, missing,
	// empty, OpenRouter's "-1" dynamic-price sentinels, NaN/Inf or hex syntax —
	// and InputPerM/OutputPerM carry 0 as "unknown", never as a price (ADR-018:
	// never estimate). InputPerM == 0 with Priced true means genuinely free
	// (e.g. "inception/mercury-decide:free").
	Priced        bool
	InputPerM     float64 // USD per million input tokens; meaningful only when Priced
	OutputPerM    float64 // USD per million output tokens; meaningful only when Priced
	ContextLength int
	AliasTarget   string // for "~" aliases, the slug they point to
}

// Options controls fetching and caching.
type Options struct {
	APIKey     string // OPENROUTER_API_KEY; the benchmarks feed needs it, the catalog does not
	CacheDir   string // default $XDG_CACHE_HOME/conclave/openrouter-benchmarks/
	TTL        time.Duration
	Refresh    bool
	HTTPClient interface{} // *http.Client
}

// ErrNotImplemented is returned by the stub until the lane lands.
var ErrNotImplemented = errors.New("openrouter benchmarks: not implemented")

// LoadFeed fetches (or reads cached) the benchmarks feed. A missing key is an
// error naming OPENROUTER_API_KEY; callers treat any error as "no chat scores".
func LoadFeed(ctx context.Context, opts Options) (*Feed, error) { return loadFeed(ctx, opts) }

// LoadDecisionModels fetches (or reads cached) the decision-model catalog.
func LoadDecisionModels(ctx context.Context, opts Options) ([]DecisionModel, error) {
	return loadDecisionModels(ctx, opts)
}
