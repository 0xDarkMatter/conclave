package batch

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/config"
	"github.com/0xDarkMatter/conclave-cli/internal/judge"
	"github.com/0xDarkMatter/conclave-cli/internal/orchestrator"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// Item represents a single input item from the JSONL file
type Item struct {
	ID      string          `json:"id"`
	Prompt  string          `json:"prompt,omitempty"`
	Context string          `json:"context,omitempty"`
	Extra   json.RawMessage `json:"-"` // Preserved for passthrough
	raw     map[string]json.RawMessage
}

// Result represents the output for a single item
type Result struct {
	ID          string                 `json:"id"`
	Verdict     string                 `json:"verdict,omitempty"`
	Confidence  string                 `json:"confidence,omitempty"`
	Reasoning   string                 `json:"reasoning,omitempty"`
	Providers   map[string]interface{} `json:"providers,omitempty"`
	CostUSD     float64                `json:"cost_usd,omitempty"`
	Error       string                 `json:"error,omitempty"`
	DurationMs  int64                  `json:"duration_ms,omitempty"`
	ExtraFields map[string]interface{} `json:"-"` // Passthrough fields
}

// MarshalJSON handles custom JSON marshaling to include extra fields
func (r Result) MarshalJSON() ([]byte, error) {
	// Start with a map containing all the standard fields
	m := map[string]interface{}{
		"id": r.ID,
	}
	if r.Verdict != "" {
		m["verdict"] = r.Verdict
	}
	if r.Confidence != "" {
		m["confidence"] = r.Confidence
	}
	if r.Reasoning != "" {
		m["reasoning"] = r.Reasoning
	}
	if len(r.Providers) > 0 {
		m["providers"] = r.Providers
	}
	if r.CostUSD > 0 {
		m["cost_usd"] = r.CostUSD
	}
	if r.Error != "" {
		m["error"] = r.Error
	}
	if r.DurationMs > 0 {
		m["duration_ms"] = r.DurationMs
	}
	// Merge extra fields
	for k, v := range r.ExtraFields {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return json.Marshal(m)
}

// Stats holds batch processing statistics
type Stats struct {
	Total     int
	Completed int
	Succeeded int
	Failed    int
	StartTime time.Time
	TotalCost float64
}

// Processor handles batch processing of JSONL files
type Processor struct {
	registry      *providers.Registry
	cfg           *config.Config
	providers     []providers.Provider
	judgeProvider providers.Provider
	workers       int
	timeout       int
	checkpoint    *Checkpoint
	rateLimiter   *RateLimiter
	progress      *Progress
	verbose       bool
	blind         bool
	retries       int
}

// Options configures the batch processor
type Options struct {
	Registry       *providers.Registry
	Config         *config.Config
	ProviderNames  []string
	ModelOverrides map[string]string
	JudgeName      string
	Workers        int
	Timeout        int
	OutputPath     string
	Resume         bool
	Verbose        bool
	Blind          bool
	NoRateLimit    bool
	Retries        int
}

// NewProcessor creates a new batch processor
func NewProcessor(opts Options) (*Processor, error) {
	// Get provider instances
	providerList, err := opts.Registry.GetProviders(opts.ProviderNames, opts.ModelOverrides)
	if err != nil {
		return nil, fmt.Errorf("failed to get providers: %w", err)
	}

	// Get judge provider
	judgeProvider, err := opts.Registry.GetProvider(opts.JudgeName, opts.ModelOverrides)
	if err != nil {
		return nil, fmt.Errorf("failed to get judge provider: %w", err)
	}

	// Create checkpoint if resume is enabled
	var checkpoint *Checkpoint
	if opts.Resume && opts.OutputPath != "" {
		checkpoint = NewCheckpoint(opts.OutputPath)
		if err := checkpoint.Load(); err != nil {
			return nil, fmt.Errorf("failed to load checkpoint: %w", err)
		}
	}

	// Create rate limiter based on provider count (or unlimited for high-tier accounts)
	var rateLimiter *RateLimiter
	if opts.NoRateLimit {
		rateLimiter = NewUnlimitedRateLimiter()
	} else {
		rateLimiter = NewRateLimiter(len(providerList))
	}

	return &Processor{
		registry:      opts.Registry,
		cfg:           opts.Config,
		providers:     providerList,
		judgeProvider: judgeProvider,
		workers:       opts.Workers,
		timeout:       opts.Timeout,
		checkpoint:    checkpoint,
		rateLimiter:   rateLimiter,
		verbose:       opts.Verbose,
		blind:         opts.Blind,
		retries:       opts.Retries,
	}, nil
}

// Process runs the batch processing pipeline
func (p *Processor) Process(ctx context.Context, input io.Reader, output io.Writer, defaultPrompt string) (*Stats, error) {
	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		fmt.Fprintln(os.Stderr, "\nShutting down gracefully...")
		cancel()
	}()

	// Count total items first
	items, err := p.readItems(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	if len(items) == 0 {
		return &Stats{}, nil
	}

	// Filter already processed items if resuming
	if p.checkpoint != nil {
		var filtered []Item
		for _, item := range items {
			if !p.checkpoint.IsProcessed(item.ID) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	// Initialize progress
	p.progress = NewProgress(len(items), os.Stderr)
	p.progress.Start()

	// Create channels
	itemChan := make(chan Item, p.workers)
	resultChan := make(chan Result, p.workers)
	doneChan := make(chan struct{})

	// Track stats
	stats := &Stats{
		Total:     len(items),
		StartTime: time.Now(),
	}
	var statsLock sync.Mutex

	// Start result writer
	go func() {
		defer close(doneChan)
		encoder := json.NewEncoder(output)
		for result := range resultChan {
			if err := encoder.Encode(result); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to write result for %s: %v\n", result.ID, err)
			}
			if p.checkpoint != nil {
				_ = p.checkpoint.MarkProcessed(result.ID)
			}

			statsLock.Lock()
			stats.Completed++
			stats.TotalCost += result.CostUSD
			if result.Error == "" {
				stats.Succeeded++
			} else {
				stats.Failed++
			}
			statsLock.Unlock()

			p.progress.Increment()
		}
	}()

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range itemChan {
				result := p.processItem(ctx, item, defaultPrompt)
				select {
				case resultChan <- result:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Feed items to workers
	for _, item := range items {
		select {
		case itemChan <- item:
		case <-ctx.Done():
			break
		}
	}
	close(itemChan)

	// Wait for workers to finish
	wg.Wait()
	close(resultChan)

	// Wait for writer to finish
	<-doneChan

	p.progress.Stop()

	return stats, nil
}

// processItem processes a single item through the pipeline with retry support
func (p *Processor) processItem(ctx context.Context, item Item, defaultPrompt string) Result {
	start := time.Now()

	// Use item prompt or default
	prompt := item.Prompt
	if prompt == "" {
		prompt = defaultPrompt
	}
	if prompt == "" {
		return Result{
			ID:    item.ID,
			Error: "no prompt provided",
		}
	}

	// Build full prompt with context
	fullPrompt := prompt
	if item.Context != "" {
		fullPrompt = item.Context + "\n\n" + prompt
	}

	// Retry loop with exponential backoff
	maxAttempts := p.retries + 1
	var lastErr error
	var responses []providers.Response

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Wait for rate limit
		if err := p.rateLimiter.Wait(ctx); err != nil {
			return Result{
				ID:    item.ID,
				Error: fmt.Sprintf("rate limit error: %v", err),
			}
		}

		// Run orchestration
		orch := orchestrator.New(p.providers, p.timeout)
		var err error
		responses, err = orch.Run(ctx, fullPrompt)
		if err == nil {
			lastErr = nil
			break
		}

		lastErr = err

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			break
		}

		// Don't retry on last attempt
		if attempt == maxAttempts {
			break
		}

		// Exponential backoff: 1s, 2s, 4s, 8s...
		backoff := time.Duration(1<<(attempt-1)) * time.Second
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}

		select {
		case <-ctx.Done():
			return Result{
				ID:         item.ID,
				Error:      fmt.Sprintf("cancelled after %d attempts", attempt),
				DurationMs: time.Since(start).Milliseconds(),
			}
		case <-time.After(backoff):
			// Continue to next attempt
		}
	}

	if lastErr != nil {
		return Result{
			ID:         item.ID,
			Error:      fmt.Sprintf("query error after %d attempts: %v", maxAttempts, lastErr),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}

	// Build result - skip judge for single provider (direct response)
	var result Result
	if len(p.providers) == 1 {
		// Single provider: return response directly, no synthesis needed
		resp := responses[0]
		if resp.Status == "error" {
			// Retry on provider error if retries enabled
			if p.retries > 0 {
				return Result{
					ID:         item.ID,
					Error:      fmt.Sprintf("provider error after retries: %s", resp.Error),
					DurationMs: time.Since(start).Milliseconds(),
				}
			}
			return Result{
				ID:         item.ID,
				Error:      resp.Error,
				DurationMs: time.Since(start).Milliseconds(),
			}
		}
		result = Result{
			ID:         item.ID,
			Verdict:    resp.Response,
			Confidence: "single_provider",
			DurationMs: time.Since(start).Milliseconds(),
			CostUSD:    p.estimateCost(responses, nil),
		}
	} else {
		// Multiple providers: run judge synthesis
		j := judge.New(p.judgeProvider)
		verdict, err := j.Synthesize(ctx, prompt, responses, p.timeout, p.blind)
		if err != nil {
			return Result{
				ID:         item.ID,
				Error:      fmt.Sprintf("judge error: %v", err),
				DurationMs: time.Since(start).Milliseconds(),
			}
		}
		result = Result{
			ID:         item.ID,
			Verdict:    verdict.Result,
			Confidence: verdict.Confidence,
			Reasoning:  verdict.Reasoning,
			DurationMs: time.Since(start).Milliseconds(),
			CostUSD:    p.estimateCost(responses, verdict),
		}
	}

	// Include provider responses if verbose
	if p.verbose {
		providerMap := make(map[string]interface{})
		for _, r := range responses {
			providerMap[r.Provider] = map[string]interface{}{
				"model":    r.Model,
				"response": r.Response,
				"status":   r.Status,
			}
		}
		result.Providers = providerMap
	}

	// Passthrough extra fields from input
	if len(item.raw) > 0 {
		result.ExtraFields = make(map[string]interface{})
		for k, v := range item.raw {
			if k != "id" && k != "prompt" && k != "context" {
				var val interface{}
				json.Unmarshal(v, &val)
				result.ExtraFields[k] = val
			}
		}
	}

	return result
}

// readItems reads all items from the input
func (p *Processor) readItems(input io.Reader) ([]Item, error) {
	var items []Item
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	lineNum := int64(0)
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping malformed line %d: %v\n", lineNum, err)
			continue
		}

		var item Item
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping malformed line %d: %v\n", lineNum, err)
			continue
		}

		// Use line number as ID if not provided
		if item.ID == "" {
			item.ID = fmt.Sprintf("line_%d", lineNum)
			fmt.Fprintf(os.Stderr, "Warning: line %d has no 'id' field, using %s\n", lineNum, item.ID)
		}

		item.raw = raw
		items = append(items, item)
	}

	return items, scanner.Err()
}

// estimateCost estimates the cost of a query based on token usage
func (p *Processor) estimateCost(responses []providers.Response, verdict *judge.Verdict) float64 {
	// Rough cost estimation based on MODEL_REGISTRY.md pricing
	// Using cheap mode defaults ($/1M tokens)
	costs := map[string]struct{ in, out float64 }{
		"gemini":     {0.50, 3.00},    // gemini-3-flash-preview
		"openai":     {0.10, 0.40},    // gpt-5-nano
		"claude":     {1.00, 5.00},    // claude-haiku-4-5
		"perplexity": {1.00, 1.00},    // sonar
		"grok":       {0.20, 0.50},    // grok-4-1-fast-non-reasoning
		"glm":        {0.00, 0.00},    // free tier
	}

	var totalCost float64
	for _, r := range responses {
		if r.Metrics != nil {
			if c, ok := costs[r.Provider]; ok {
				totalCost += float64(r.Metrics.InputTokens) * c.in / 1_000_000
				totalCost += float64(r.Metrics.OutputTokens) * c.out / 1_000_000
			}
		}
	}

	// Add judge cost
	if verdict != nil && verdict.JudgeTokens > 0 {
		if c, ok := costs[verdict.JudgeProvider]; ok {
			// Rough split: 70% input, 30% output
			totalCost += float64(verdict.JudgeTokens) * 0.7 * c.in / 1_000_000
			totalCost += float64(verdict.JudgeTokens) * 0.3 * c.out / 1_000_000
		}
	}

	return totalCost
}

// Summary prints a summary of the batch processing
var completedItems atomic.Int32
