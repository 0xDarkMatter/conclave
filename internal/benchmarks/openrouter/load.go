// This file owns OpenRouter HTTP and wire-format ingestion for ADR-018.
// It makes one authenticated request per benchmark source so provenance does
// not collapse, and one unauthenticated request for decision-model prices.
// Null scores and prices remain nil; no external quality value is inferred.
// Cache policy and persistence live in cache.go.
package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTTL   = 24 * time.Hour
	fetchTimeout = 6 * time.Second
	maxBody      = 16 << 20
)

type benchmarkWire struct {
	Data []benchmarkRowWire `json:"data"`
	Meta benchmarkMetaWire  `json:"meta"`
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
}

type pricingWire struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

type modelsWire struct {
	Data []decisionModelWire `json:"data"`
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
	aaWire, err := fetchBenchmarkSource(ctx, client, endpoint, apiKey, "artificial-analysis")
	if err != nil {
		return nil, fmt.Errorf("fetch artificial-analysis source: %w", err)
	}
	evalsWire, err := fetchBenchmarkSource(ctx, client, endpoint, apiKey, "openrouter")
	if err != nil {
		return nil, fmt.Errorf("fetch openrouter source: %w", err)
	}

	feed := &Feed{
		AAAsOf:    aaWire.Meta.AsOf,
		EvalsAsOf: evalsWire.Meta.AsOf,
		Citation:  aaWire.Meta.Citation,
	}
	for _, row := range aaWire.Data {
		if row.Source != "" && row.Source != "artificial-analysis" {
			continue
		}
		input, output, err := convertPricing(row.Pricing)
		if err != nil {
			return nil, fmt.Errorf("decode pricing for %q: %w", row.ModelPermaslug, err)
		}
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
	for _, row := range evalsWire.Data {
		if row.Source != "" && row.Source != "openrouter" {
			continue
		}
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
	models := make([]DecisionModel, 0, len(wire.Data))
	for _, row := range wire.Data {
		input, output, err := convertPricing(row.Pricing)
		if err != nil {
			return nil, fmt.Errorf("decode pricing for %q: %w", row.ID, err)
		}
		model := DecisionModel{
			Slug:          row.ID,
			Name:          row.Name,
			ContextLength: row.ContextLength,
		}
		if input != nil {
			model.InputPerM = *input
		}
		if output != nil {
			model.OutputPerM = *output
		}
		if row.AliasTarget != nil {
			model.AliasTarget = row.AliasTarget.Slug
		}
		models = append(models, model)
	}
	return models, nil
}

func convertPricing(pricing *pricingWire) (input, output *float64, err error) {
	if pricing == nil {
		return nil, nil, nil
	}
	input, err = perMillion(pricing.Prompt)
	if err != nil {
		return nil, nil, fmt.Errorf("prompt price: %w", err)
	}
	output, err = perMillion(pricing.Completion)
	if err != nil {
		return nil, nil, fmt.Errorf("completion price: %w", err)
	}
	return input, output, nil
}

func perMillion(value string) (*float64, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", value, err)
	}
	parsed *= 1_000_000
	return &parsed, nil
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
		return nil, fmt.Errorf("OpenRouter HTTP %d: %s", resp.StatusCode, apiErr.Error.Message)
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
