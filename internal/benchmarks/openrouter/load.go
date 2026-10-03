// This file owns OpenRouter HTTP and wire-format ingestion for ADR-018.
// It makes one authenticated request per benchmark source so provenance does
// not collapse, and one unauthenticated request for decision-model prices.
// Null scores and prices remain nil; no external quality value is inferred.
// Ingestion also refuses (and therefore never caches) a 200 that carries no
// data array or an error envelope, keeps a benchmark row only on an exact
// source match, and redacts plus caps vendor error text before it can reach
// a caller's warning. Cache policy and persistence live in cache.go.
package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTTL = 24 * time.Hour
	// maxVendorMessage bounds how many characters of upstream error text may
	// enter an error string (adjudication 2): vendor bodies are data, and a
	// hostile or buggy response must not balloon a caller's warning.
	maxVendorMessage = 300

	aaSource    = "artificial-analysis"
	evalsSource = "openrouter"

	fetchTimeout = 6 * time.Second
	maxBody      = 16 << 20
)

type benchmarkWire struct {
	// Data is a pointer so a missing or null "data" member stays
	// distinguishable from a present-but-empty array: OpenRouter answers some
	// faults with HTTP 200 and an {"error":{...}} envelope, which must not
	// decode as an empty catalog (adjudication 1 on the refuted defects).
	Data *[]benchmarkRowWire `json:"data"`
	Meta benchmarkMetaWire   `json:"meta"`
}

type benchmarkRowWire struct {
	Source            string       `json:"source"`
	ModelPermaslug    string       `json:"model_permaslug"`
	DisplayName       string       `json:"display_name"`
	IntelligenceIndex *float64     `json:"intelligence_index"`
	CodingIndex       *float64     `json:"coding_index"`
	AgenticIndex      *float64     `json:"agentic_index"`
	Pricing           *pricingWire `json:"pricing"`
	BenchmarkType     string       `json:"benchmark_type"`
	Accuracy          *float64     `json:"accuracy"`
	AvgCostPerTask    *float64     `json:"avg_cost_per_task"`
	TotalTasks        int          `json:"total_tasks"`
}

type benchmarkMetaWire struct {
	AsOf     string `json:"as_of"`
	Citation string `json:"citation"`
	Source   string `json:"source"`
}

type pricingWire struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

type modelsWire struct {
	Data *[]decisionModelWire `json:"data"` // pointer: see benchmarkWire.Data
}

type decisionModelWire struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	ContextLength int          `json:"context_length"`
	Pricing       *pricingWire `json:"pricing"`
	AliasTarget   *struct {
		Slug string `json:"slug"`
	} `json:"alias_target"`
}

type apiErrorWire struct {
	Error struct {
		Message string          `json:"message"`
		Code    json.RawMessage `json:"code"`
	} `json:"error"`
}

func loadFeed(ctx context.Context, opts Options) (*Feed, error) {
	if disabled() {
		return nil, errors.New("openrouter benchmarks disabled by CONCLAVE_NO_PRICING")
	}
	apiKey := strings.TrimSpace(opts.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	}
	// Authentication is checked before cache access so a command cannot appear
	// configured merely because another account previously populated the cache.
	if apiKey == "" {
		return nil, errors.New("openrouter benchmarks require OPENROUTER_API_KEY")
	}

	settings, err := resolveSettings(opts)
	if err != nil {
		return nil, err
	}
	cachePath := settings.cachePath(feedCacheFile)
	cached, cacheErr := readFeedCache(cachePath)
	if !opts.Refresh && cacheFresh(cached.fetchedAt(), settings.ttl) {
		return cached.Feed, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	feed, fetchErr := fetchFeed(fetchCtx, settings.client, benchmarksEndpoint(), apiKey)
	if fetchErr != nil {
		if cached != nil {
			return cached.Feed, fmt.Errorf("refresh openrouter benchmarks, using stale cache: %w", fetchErr)
		}
		if cacheErr != nil {
			return nil, fmt.Errorf("openrouter benchmarks unavailable (%v): %w", cacheErr, fetchErr)
		}
		return nil, fmt.Errorf("openrouter benchmarks unavailable: %w", fetchErr)
	}
	entry := &feedCache{FetchedAt: time.Now().UTC(), Feed: feed}
	if err := writeJSONCache(cachePath, entry); err != nil {
		return feed, fmt.Errorf("openrouter benchmarks cache not written: %w", err)
	}
	return feed, nil
}

func loadDecisionModels(ctx context.Context, opts Options) ([]DecisionModel, error) {
	if disabled() {
		return nil, errors.New("openrouter decision-model catalog disabled by CONCLAVE_NO_PRICING")
	}
	settings, err := resolveSettings(opts)
	if err != nil {
		return nil, err
	}
	cachePath := settings.cachePath(modelsCacheFile)
	cached, cacheErr := readModelsCache(cachePath)
	if !opts.Refresh && cacheFresh(cached.fetchedAt(), settings.ttl) {
		return cached.Models, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	models, fetchErr := fetchDecisionModels(fetchCtx, settings.client, modelsEndpoint())
	if fetchErr != nil {
		if cached != nil {
			return cached.Models, fmt.Errorf("refresh openrouter decision models, using stale cache: %w", fetchErr)
		}
		if cacheErr != nil {
			return nil, fmt.Errorf("openrouter decision models unavailable (%v): %w", cacheErr, fetchErr)
		}
		return nil, fmt.Errorf("openrouter decision models unavailable: %w", fetchErr)
	}
	entry := &modelsCache{FetchedAt: time.Now().UTC(), Models: models}
	if err := writeJSONCache(cachePath, entry); err != nil {
		return models, fmt.Errorf("openrouter decision-model cache not written: %w", err)
	}
	return models, nil
}

func fetchFeed(ctx context.Context, client *http.Client, endpoint, apiKey string) (*Feed, error) {
	aaWire, err := fetchBenchmarkSource(ctx, client, endpoint, apiKey, aaSource)
	if err != nil {
		return nil, fmt.Errorf("fetch artificial-analysis source: %w", err)
	}
	evalsWire, err := fetchBenchmarkSource(ctx, client, endpoint, apiKey, evalsSource)
	if err != nil {
		return nil, fmt.Errorf("fetch openrouter source: %w", err)
	}
	aa, err := selectSource(aaWire, aaSource)
	if err != nil {
		return nil, fmt.Errorf("artificial-analysis source: %w", err)
	}
	evals, err := selectSource(evalsWire, evalsSource)
	if err != nil {
		return nil, fmt.Errorf("openrouter source: %w", err)
	}

	feed := &Feed{
		AAAsOf:    aa.asOf,
		EvalsAsOf: evals.asOf,
		Citation:  aa.citation,
	}
	for _, row := range aa.rows {
		input, output := convertPricing(row.Pricing)
		feed.AA = append(feed.AA, AAScore{
			Slug:         row.ModelPermaslug,
			Name:         row.DisplayName,
			Intelligence: row.IntelligenceIndex,
			Coding:       row.CodingIndex,
			Agentic:      row.AgenticIndex,
			InputPerM:    input,
			OutputPerM:   output,
		})
	}
	for _, row := range evals.rows {
		feed.Evals = append(feed.Evals, EvalScore{
			Slug:           row.ModelPermaslug,
			Name:           row.DisplayName,
			Benchmark:      row.BenchmarkType,
			Accuracy:       row.Accuracy,
			AvgCostPerTask: row.AvgCostPerTask,
			TotalTasks:     row.TotalTasks,
		})
	}
	return feed, nil
}

// sourceSelection is one benchmark source after provenance filtering.
type sourceSelection struct {
	rows     []benchmarkRowWire
	asOf     string
	citation string
}

// selectSource applies the provenance rules of adjudication 5: a row belongs
// to a source only when its own source field says so exactly — empty never
// matches, because a response that ignored ?source= serves every row to every
// caller. When meta.source names a different scope, the response's as_of and
// citation describe that other scope and are dropped rather than
// misattributed; that case is fatal only when no row survived, which is the
// "the filter was ignored and nothing usable came back" shape. Separately
// (adjudication 1), a source whose own meta carries no as_of is refused
// outright: unsnapshotted data must not be cached or cited (ADR-018).
func selectSource(wire *benchmarkWire, source string) (sourceSelection, error) {
	var sel sourceSelection
	for _, row := range *wire.Data {
		if row.Source == source {
			sel.rows = append(sel.rows, row)
		}
	}
	if wire.Meta.Source != "" && wire.Meta.Source != source {
		if len(sel.rows) == 0 {
			return sel, fmt.Errorf("no rows carry source %q (meta.source = %q): the source filter looks ignored", source, wire.Meta.Source)
		}
		return sel, nil
	}
	if strings.TrimSpace(wire.Meta.AsOf) == "" {
		return sel, fmt.Errorf("no meta.as_of snapshot for source %q: refusing unsnapshotted data", source)
	}
	sel.asOf = wire.Meta.AsOf
	sel.citation = wire.Meta.Citation
	return sel, nil
}

func fetchBenchmarkSource(ctx context.Context, client *http.Client, endpoint, apiKey, source string) (*benchmarkWire, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse benchmarks URL: %w", err)
	}
	query := u.Query()
	query.Set("source", source)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "conclave-cli (+https://github.com/0xDarkMatter/conclave)")
	body, err := doJSON(req, client)
	if err != nil {
		return nil, err
	}
	var wire benchmarkWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode benchmarks response: %w", err)
	}
	if wire.Data == nil {
		return nil, noDataError(body, req)
	}
	return &wire, nil
}

func fetchDecisionModels(ctx context.Context, client *http.Client, endpoint string) ([]DecisionModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "conclave-cli (+https://github.com/0xDarkMatter/conclave)")
	body, err := doJSON(req, client)
	if err != nil {
		return nil, err
	}
	var wire modelsWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode decision-model response: %w", err)
	}
	if wire.Data == nil {
		return nil, noDataError(body, req)
	}
	models := make([]DecisionModel, 0, len(*wire.Data))
	for _, row := range *wire.Data {
		input, output := convertPricing(row.Pricing)
		model := DecisionModel{
			Slug:          row.ID,
			Name:          row.Name,
			ContextLength: row.ContextLength,
		}
		// Priced demands both legs: adjudication 3 sets it on the prompt price
		// and adjudication 4 drops it when any leg is a negative or
		// non-finite sentinel, so a valid prompt with a sentinel completion
		// reads unpriced rather than "free output".
		if input != nil && output != nil {
			model.Priced = true
			model.InputPerM = *input
			model.OutputPerM = *output
		}
		if row.AliasTarget != nil {
			model.AliasTarget = row.AliasTarget.Slug
		}
		models = append(models, model)
	}
	return models, nil
}

// noDataError reports a 200 body that carries no data array. It prefers the
// upstream error envelope when one is present, so the caller's warning says
// what actually went wrong; the message is redacted and capped like every
// other vendor string.
func noDataError(body []byte, req *http.Request) error {
	var apiErr apiErrorWire
	if json.Unmarshal(body, &apiErr) == nil && strings.TrimSpace(apiErr.Error.Message) != "" {
		return fmt.Errorf("OpenRouter error envelope on HTTP 200: %s", sanitizeVendorMessage(apiErr.Error.Message, req))
	}
	return errors.New("OpenRouter response has no data array")
}

// sanitizeVendorMessage prepares upstream error text for an error string.
// Credentials come first — a 401 body can echo the Authorization header
// verbatim, which is the leak the review demonstrated — then the length cap.
// Redaction runs before the cap so a key straddling the cut is still replaced
// whole.
func sanitizeVendorMessage(message string, req *http.Request) string {
	if req != nil {
		if auth := req.Header.Get("Authorization"); auth != "" {
			token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			message = strings.ReplaceAll(message, auth, "[redacted]")
			if token != "" && token != auth {
				message = strings.ReplaceAll(message, token, "[redacted]")
			}
		}
	}
	if runes := []rune(message); len(runes) > maxVendorMessage {
		message = string(runes[:maxVendorMessage])
	}
	return message
}

// convertPricing maps a wire pricing object to per-million pointers. Nil
// means the source did not publish a usable price; it is never a value
// (ADR-018: never estimate).
func convertPricing(pricing *pricingWire) (input, output *float64) {
	if pricing == nil {
		return nil, nil
	}
	return perMillion(pricing.Prompt), perMillion(pricing.Completion)
}

// perMillion converts a per-token USD price string into USD per million
// tokens, rounded to 1e-9 so binary-float noise (0.041999999999999996 for the
// live 0.000000042 per-token price) does not reach equality checks, dedupe or
// table output (adjudication 7). Anything that is not a plain non-negative
// decimal yields nil: unpriced, never zero-filled. OpenRouter uses "-1" as a
// dynamic-price sentinel (adjudication 4), and strconv.ParseFloat would also
// accept "NaN", "Inf" and hex floats (adjudication 6) — all refused here.
func perMillion(value string) *float64 {
	perToken, ok := plainDecimal(value)
	if !ok || perToken < 0 {
		return nil
	}
	perM := math.Round(perToken*1e6*1e9) / 1e9
	if perM == 0 {
		perM = 0 // normalize a "-0" price to +0 so cache JSON stays plain
	}
	if math.IsNaN(perM) || math.IsInf(perM, 0) {
		// A hostile digit string can still overflow the rounding arithmetic;
		// infinity is not a price the source published.
		return nil
	}
	return &perM
}

// plainDecimal accepts exactly the grammar OpenRouter prices use: an optional
// leading sign, decimal digits, at most one point. Exponents, hex
// ("0x1p-20") and the NaN/Inf spellings are rejected by the character scan
// before ParseFloat can accept them (adjudication 6: non-decimal syntax is
// unpriced, and NaN would poison the cache's json.Marshal).
func plainDecimal(value string) (float64, bool) {
	s := strings.TrimSpace(value)
	if s == "" {
		return 0, false
	}
	digits := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '.':
		case (c == '+' || c == '-') && i == 0:
		default:
			return 0, false
		}
	}
	if digits == 0 || strings.Count(s, ".") > 1 {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// Includes the ±Inf + ErrRange pair a giant digit string produces.
		return 0, false
	}
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

func doJSON(req *http.Request, client *http.Client) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		return body, nil
	}
	var apiErr apiErrorWire
	if json.Unmarshal(body, &apiErr) == nil && strings.TrimSpace(apiErr.Error.Message) != "" {
		return nil, fmt.Errorf("OpenRouter HTTP %d: %s", resp.StatusCode, sanitizeVendorMessage(apiErr.Error.Message, req))
	}
	return nil, fmt.Errorf("OpenRouter HTTP %d", resp.StatusCode)
}

func benchmarksEndpoint() string {
	if endpoint := strings.TrimSpace(os.Getenv("CONCLAVE_OPENROUTER_BENCHMARKS_URL")); endpoint != "" {
		return endpoint
	}
	return BenchmarksURL
}

func modelsEndpoint() string {
	if endpoint := strings.TrimSpace(os.Getenv("CONCLAVE_OPENROUTER_MODELS_URL")); endpoint != "" {
		return endpoint
	}
	return DecisionModelsURL
}
