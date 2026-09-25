package batch

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/cache"
	"github.com/0xDarkMatter/conclave-cli/internal/config"
	"github.com/0xDarkMatter/conclave-cli/internal/judge"
	"github.com/0xDarkMatter/conclave-cli/internal/orchestrator"
	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// === Types ===

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

	// cancelled marks a result produced by a shutdown rather than by real
	// work. Such an item must NOT be checkpointed: --resume would skip it
	// forever, silently dropping it from the batch. Unexported so it cannot
	// reach the JSON output, which MarshalJSON builds from named fields.
	cancelled bool
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

	// Budget is the cap that was in force (0 = uncapped) and BudgetStopped
	// records that dispatch was cut short because TotalCost reached it.
	// Skipped counts the items never dispatched; they stay absent from the
	// checkpoint so --resume picks them up next run.
	Budget        float64
	BudgetStopped bool
	Skipped       int
	// Cancelled records that the run was cut short by a signal or a cancelled
	// context rather than by the budget. Either way Skipped is non-zero and the
	// output file is a PARTIAL result, which callers must be able to detect.
	Cancelled bool
	// WriteFailed counts results that were computed (and paid for) but could
	// not be written to the output. They count as Failed and stay out of the
	// checkpoint, so --resume re-runs them; callers must treat any non-zero
	// value as a failed run.
	WriteFailed int
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
	// resume is whether this run should SKIP ids the checkpoint already holds.
	// A checkpoint is written either way; see NewProcessor.
	resume bool
	// budget caps cumulative ESTIMATED spend in USD; 0 disables the cap.
	// The estimate is post-hoc (an item is priced only once it has returned),
	// so up to `workers` items may already be in flight when the cap trips.
	budget float64
	// pricing is the OpenRouter catalog used for cost estimates. May be nil
	// (offline / disabled); estimateCost then falls back to fallbackCosts.
	pricing *pricing.Catalog
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
	// Budget stops dispatch once estimated spend reaches this many USD.
	// 0 means uncapped.
	Budget float64
	// Cache is the opt-in response cache; nil disables it. Wrapping happens
	// here rather than in the caller so every item in the batch shares it.
	Cache *cache.Cache
	// Providers and Judge bypass the registry when set. This is the injection
	// seam the package's own tests use to run a batch against fake providers
	// with no credentials, no network, and no CLI binaries installed.
	Providers []providers.Provider
	Judge     providers.Provider
	// Pricing is optional; nil means "use the compiled fallback table".
	Pricing *pricing.Catalog
}

// === Construction ===

// NewProcessor creates a new batch processor
func NewProcessor(opts Options) (*Processor, error) {
	// Process starts exactly opts.Workers goroutines and sizes its item
	// channel by it: 0 deadlocks the feeder and a negative value panics in
	// make(chan). cmd validates the flag too; this guards other callers.
	if opts.Workers < 1 {
		return nil, fmt.Errorf("workers must be at least 1 (got %d)", opts.Workers)
	}

	// Get provider instances (explicit injection wins; see Options.Providers)
	providerList := opts.Providers
	if providerList == nil {
		var err error
		providerList, err = opts.Registry.GetProviders(opts.ProviderNames, opts.ModelOverrides)
		if err != nil {
			return nil, fmt.Errorf("failed to get providers: %w", err)
		}
	}

	// Get judge provider
	judgeProvider := opts.Judge
	if judgeProvider == nil {
		var err error
		judgeProvider, err = opts.Registry.GetProvider(opts.JudgeName, opts.ModelOverrides)
		if err != nil {
			return nil, fmt.Errorf("failed to get judge provider: %w", err)
		}
	}

	// The judge is deliberately NOT wrapped: a verdict depends on the SET of
	// responses it saw, which is not part of any single provider's cache key.
	// Caching it would serve a synthesis of a different panel. See ADR-011.
	providerList = cache.WrapAll(providerList, opts.Cache, cache.ModeAPI)

	// A checkpoint is written whenever there is an output file to resume into,
	// not only when --resume was passed. Recording it only on resumed runs made
	// the "Resume with: --resume" hint a lie for the FIRST run: there would be
	// no checkpoint, so a resumed run re-pays for everything and appends
	// duplicates to the output.
	//
	// Resume decides how the existing file is treated, not whether one is kept:
	//   --resume  -> load it, and skip ids it already holds
	//   otherwise -> clear it, because the output file is truncated too and a
	//                stale checkpoint would make a later --resume skip items
	//                that are no longer in the output.
	var checkpoint *Checkpoint
	if opts.OutputPath != "" {
		checkpoint = NewCheckpoint(opts.OutputPath)
		if opts.Resume {
			if err := checkpoint.Load(); err != nil {
				return nil, fmt.Errorf("failed to load checkpoint: %w", err)
			}
		} else if err := checkpoint.Clear(); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to reset checkpoint: %w", err)
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
		resume:        opts.Resume,
		budget:        opts.Budget,
		pricing:       opts.Pricing,
	}, nil
}

// === Pipeline ===

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

	// Filter duplicates and already-processed items
	seen := make(map[string]bool)
	var filtered []Item
	for _, item := range items {
		// Warn about duplicate IDs in input
		if seen[item.ID] {
			fmt.Fprintf(os.Stderr, "Warning: duplicate ID '%s' in input, skipping\n", item.ID)
			continue
		}
		seen[item.ID] = true

		// Skip already-processed items if resuming. Gated on resume, not on the
		// checkpoint existing: a fresh run keeps a checkpoint too, and must not
		// skip anything.
		if p.resume && p.checkpoint != nil && p.checkpoint.IsProcessed(item.ID) {
			continue
		}
		filtered = append(filtered, item)
	}
	items = filtered

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
		Budget:    p.budget,
	}
	var statsLock sync.Mutex

	// Start result writer
	go func() {
		defer close(doneChan)
		encoder := json.NewEncoder(output)
		for result := range resultChan {
			// A result that never reached the output must not be checkpointed
			// or counted as a success: --resume would skip it and the run
			// would report a clean finish over an empty file
			// (TestUnwrittenResultsAreNotCheckpointedOrCounted).
			writeErr := encoder.Encode(result)
			if writeErr != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to write result for %s: %v\n", result.ID, writeErr)
			}
			// A cancelled item never really ran, so recording it would make
			// --resume skip work that was never done.
			if p.checkpoint != nil && !result.cancelled && writeErr == nil {
				_ = p.checkpoint.MarkProcessed(result.ID)
			}

			statsLock.Lock()
			stats.Completed++
			stats.TotalCost += result.CostUSD
			if writeErr != nil {
				stats.WriteFailed++
				stats.Failed++
			} else if result.Error == "" {
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
				// Drain without working once the run is over. Starting an item
				// against a dead context burns a rate-limit slot to produce a
				// "context canceled" result that is worth nothing; leaving it
				// undispatched instead lets Skipped count it and --resume run
				// it for real.
				if ctx.Err() != nil {
					continue
				}
				result := p.processItem(ctx, item, defaultPrompt)
				// Send unconditionally. A select on ctx.Done() here would throw
				// away a result that has ALREADY been paid for and computed,
				// which is the one thing a shutdown should not do: the item
				// leaves no output line, no checkpoint entry, and no trace that
				// it ran. This cannot block, because the writer goroutine
				// consumes resultChan until it is closed, and closing only
				// happens after every worker has returned.
				resultChan <- result
			}
		}()
	}

	// Feed items to workers.
	//
	// The budget check reads the running total the writer goroutine maintains,
	// so it only ever sees items that have already COMPLETED. That is the whole
	// design: cost is known post-hoc, from real token counts, not guessed up
	// front. The consequence is documented overshoot — the workers already
	// holding items will finish them, so actual spend can exceed the cap by up
	// to `workers` items. Items never dispatched are left out of the
	// checkpoint, so --resume continues exactly where the cap bit.
feed:
	for _, item := range items {
		if p.budget > 0 {
			statsLock.Lock()
			spent := stats.TotalCost
			statsLock.Unlock()
			if spent >= p.budget {
				statsLock.Lock()
				stats.BudgetStopped = true
				statsLock.Unlock()
				break feed
			}
		}
		select {
		case itemChan <- item:
		case <-ctx.Done():
			// A bare `break` here would only leave the select, leaving the
			// feeder spinning through the remaining items after cancellation.
			break feed
		}
	}
	close(itemChan)

	// Wait for workers to finish
	wg.Wait()
	close(resultChan)

	// Wait for writer to finish
	<-doneChan

	p.progress.Stop()

	statsLock.Lock()
	// Skipped is measured against COMPLETED items, not dispatched ones, so it
	// is exactly the set --resume will re-run: only completed ids reach the
	// checkpoint. Counting dispatches instead would under-report by any item
	// that was dispatched but produced no result line.
	stats.Skipped = stats.Total - stats.Completed
	if ctx.Err() != nil {
		stats.Cancelled = true
	}
	statsLock.Unlock()

	return stats, nil
}

// === Per-item work ===

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
				ID:        item.ID,
				Error:     fmt.Sprintf("rate limit error: %v", err),
				cancelled: ctx.Err() != nil,
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

		// Adaptive rate limiting: slow down on rate limit errors
		if isRateLimitError(err) {
			p.rateLimiter.RecordRateLimit()
		}

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
				cancelled:  true,
			}
		case <-time.After(backoff):
			// Continue to next attempt
		}
	}

	if lastErr != nil {
		// Failed attempts still consumed tokens on any provider that answered,
		// so the cost must reach stats.TotalCost or --budget cannot see it.
		return Result{
			ID:         item.ID,
			Error:      fmt.Sprintf("query error after %d attempts: %v", maxAttempts, lastErr),
			DurationMs: time.Since(start).Milliseconds(),
			CostUSD:    p.estimateCost(responses, nil),
			cancelled:  ctx.Err() != nil,
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
					CostUSD:    p.estimateCost(responses, nil),
				}
			}
			return Result{
				ID:         item.ID,
				Error:      resp.Error,
				DurationMs: time.Since(start).Milliseconds(),
				CostUSD:    p.estimateCost(responses, nil),
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
				// The panel answered and was billed even though synthesis
				// failed; without this a broken judge model makes --budget
				// unenforceable.
				CostUSD:   p.estimateCost(responses, nil),
				cancelled: ctx.Err() != nil,
			}
		}
		// An unparseable synthesis is a failed item, not a verdict of
		// "PARSE_ERROR": counting it as a success hid a broken judge across a
		// whole batch (TestJudgeParseErrorIsAFailedItem).
		if verdict.Result == "PARSE_ERROR" {
			return Result{
				ID:         item.ID,
				Error:      "judge returned no parseable verdict: " + verdict.Reasoning,
				DurationMs: time.Since(start).Milliseconds(),
				CostUSD:    p.estimateCost(responses, verdict),
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
				if err := json.Unmarshal(v, &val); err != nil {
					// Store raw string if unmarshal fails
					result.ExtraFields[k] = string(v)
				} else {
					result.ExtraFields[k] = val
				}
			}
		}
	}

	// Adaptive rate limiting: record success for potential speedup
	p.rateLimiter.RecordSuccess()

	return result
}

// === Input parsing ===

// readItems reads all items from the input
func (p *Processor) readItems(input io.Reader) ([]Item, error) {
	var items []Item
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxBatchLine)

	lineNum := int64(0)
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		// TrimSpace, not == "": a CRLF file's blank lines are "\r".
		if strings.TrimSpace(line) == "" {
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

	if err := scanner.Err(); err != nil {
		// Scan stopped ON the line after the last one it returned; name it,
		// or the user bisects a large JSONL by hand (TestOverlongLineNamesTheLine).
		return nil, fmt.Errorf("reading batch input at line %d (lines are limited to %d MB): %w", lineNum+1, maxBatchLine>>20, err)
	}
	return items, nil
}

// maxBatchLine bounds one JSONL item. It matches the default --max-context
// ceiling with headroom, so an item carrying its own large prompt fits.
const maxBatchLine = 4 << 20

// === Cost estimation ===

// fallbackCosts is the LAST-RESORT price table ($/1M tokens), used only when
// the OpenRouter catalog is unavailable or does not list the model. It mirrors
// the cheap-mode defaults in config.go at the time of writing; the live catalog
// is the source of truth and this table is expected to drift. Keep it, do not
// grow it: batch mode must estimate something even fully offline.
var fallbackCosts = map[string]struct{ in, out float64 }{
	"gemini":     {0.50, 3.00}, // gemini-3-flash-preview
	"openai":     {0.05, 0.40}, // gpt-5-nano
	"claude":     {1.00, 5.00}, // claude-haiku-4-5
	"perplexity": {1.00, 1.00}, // sonar
	"grok":       {0.20, 0.50}, // grok-4-1-fast-non-reasoning
	"glm":        {0.00, 0.00}, // free tier
}

// estimateCost estimates the cost of a query from token usage. The arithmetic
// and the judge split live in internal/pricing, which is the single cost
// engine; this function only adds the layer pricing deliberately does not have.
//
// That layer is the fallback table, and the difference from internal/output is
// intentional rather than drift. Display must OMIT a price it cannot look up,
// because a printed $0.00 reads as "this was free". A budget cap must produce
// SOME number or it silently never binds, so batch mode falls back to the
// compiled table. Both call the same engine; only the miss policy differs.
//
// Prices are pay-as-you-go API prices, which is the right basis because batch
// mode always forces API mode.
func (p *Processor) estimateCost(responses []providers.Response, verdict *judge.Verdict) float64 {
	var totalCost float64
	for _, r := range responses {
		// A cache hit was not billed by anyone; counting it would inflate the
		// running total the budget cap reads. A CLI-transport response (a
		// "@cli" token in a batch, ADR-012) is subscription-billed: same rule.
		if r.Cached || r.Metrics == nil || r.Transport == string(providers.TransportCLI) {
			continue
		}
		if cost, ok := p.pricing.CostOf(r.Provider, r.Model, r.Metrics.InputTokens, r.Metrics.OutputTokens); ok {
			totalCost += cost
			continue
		}
		if c, ok := fallbackCosts[r.Provider]; ok {
			totalCost += float64(r.Metrics.InputTokens)*c.in/1_000_000 +
				float64(r.Metrics.OutputTokens)*c.out/1_000_000
		}
	}

	if verdict != nil && verdict.JudgeTokens > 0 && verdict.JudgeTransport != string(providers.TransportCLI) {
		if cost, ok := p.pricing.JudgeCostOf(verdict.JudgeProvider, verdict.JudgeModel, verdict.JudgeTokens); ok {
			totalCost += cost
		} else if c, ok := fallbackCosts[verdict.JudgeProvider]; ok {
			t := float64(verdict.JudgeTokens)
			totalCost += t*pricing.JudgeInputShare*c.in/1_000_000 +
				t*(1-pricing.JudgeInputShare)*c.out/1_000_000
		}
	}

	return totalCost
}

// === Lifecycle and helpers ===

// Close cleans up processor resources (checkpoint file handle)
func (p *Processor) Close() error {
	if p.checkpoint != nil {
		return p.checkpoint.Close()
	}
	return nil
}

// isRateLimitError checks if an error is a rate limit (429) error
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "429") ||
		strings.Contains(strings.ToLower(s), "rate limit") ||
		strings.Contains(strings.ToLower(s), "too many requests")
}
