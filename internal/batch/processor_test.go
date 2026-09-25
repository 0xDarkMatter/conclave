package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xDarkMatter/conclave-cli/internal/judge"
	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// === Fakes ===

// fakeProvider answers from a caller-supplied function so a test can script
// failures, delays and per-prompt behaviour without a network or a CLI binary.
type fakeProvider struct {
	name    string
	model   string
	metrics *providers.Metrics
	delay   time.Duration
	calls   atomic.Int32
	// finished counts calls that ran to completion, i.e. work that was paid
	// for. Every one of these must end up in the output.
	finished atomic.Int32
	// answer returns the response for one call. attempt is 1-based per process.
	answer func(attempt int32, prompt string) (string, error)
}

func (f *fakeProvider) Name() string         { return f.name }
func (f *fakeProvider) DefaultModel() string { return f.model }
func (f *fakeProvider) IsAvailable() bool    { return true }
func (f *fakeProvider) Query(ctx context.Context, prompt, model string) (string, time.Duration, *providers.Metrics, error) {
	n := f.calls.Add(1)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", 0, nil, ctx.Err()
		}
	}
	resp, err := f.answer(n, prompt)
	if err != nil {
		return "", time.Millisecond, nil, err
	}
	f.finished.Add(1)
	return resp, time.Millisecond, f.metrics, nil
}

func okProvider(name string) *fakeProvider {
	return &fakeProvider{
		name:  name,
		model: "fake-model",
		answer: func(_ int32, prompt string) (string, error) {
			return "answer for: " + prompt, nil
		},
	}
}

// judgeProvider returns a well-formed verdict so multi-provider paths work.
func judgeProvider() *fakeProvider {
	return &fakeProvider{
		name:  "claude",
		model: "fake-judge",
		answer: func(_ int32, _ string) (string, error) {
			return `{"verdict":"YES","confidence":"high","reasoning":"because"}`, nil
		},
	}
}

// newTestProcessor builds a processor over injected fakes: no registry, no
// credentials, no rate limiting (which would otherwise add seconds per item).
func newTestProcessor(t *testing.T, opts Options, list ...providers.Provider) *Processor {
	t.Helper()
	opts.Providers = list
	if opts.Judge == nil {
		opts.Judge = judgeProvider()
	}
	if opts.Workers == 0 {
		opts.Workers = 2
	}
	if opts.Timeout == 0 {
		// The orchestrator derives a per-provider context deadline from this.
		// Leaving it zero gives every query an already-expired context, which
		// fails every item for a reason that has nothing to do with the test.
		opts.Timeout = 30
	}
	opts.NoRateLimit = true
	p, err := NewProcessor(opts)
	if err != nil {
		t.Fatalf("NewProcessor: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// runBatch processes input and returns the decoded result lines.
func runBatch(t *testing.T, p *Processor, input string, prompt string) ([]map[string]any, *Stats) {
	t.Helper()
	var out bytes.Buffer
	stats, err := p.Process(context.Background(), strings.NewReader(input), &out, prompt)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	var results []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("undecodable result line %q: %v", line, err)
		}
		results = append(results, m)
	}
	return results, stats
}

// === Item parsing ===

// TestMalformedLineIsSkippedNotFatal defends against one bad row killing a
// 10,000-line job.
func TestMalformedLineIsSkippedNotFatal(t *testing.T) {
	p := newTestProcessor(t, Options{}, okProvider("openai"))
	input := `{"id":"a","prompt":"one"}
this is not json
{"id":"b","prompt":"two"}
{"id":"c","prompt":
`
	results, stats := runBatch(t, p, input, "")
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (the two well-formed lines)", len(results))
	}
	if stats.Total != 2 || stats.Failed != 0 {
		t.Fatalf("stats = %+v, want 2 total and 0 failed", stats)
	}
}

// TestMissingIDGetsALineNumber: results are matched back to input by id, so an
// item without one must still be addressable rather than silently merged.
func TestMissingIDGetsALineNumber(t *testing.T) {
	p := newTestProcessor(t, Options{}, okProvider("openai"))
	input := `{"prompt":"first"}
{"id":"named","prompt":"second"}
{"prompt":"third"}
`
	results, _ := runBatch(t, p, input, "")
	ids := map[string]bool{}
	for _, r := range results {
		ids[r["id"].(string)] = true
	}
	for _, want := range []string{"line_1", "named", "line_3"} {
		if !ids[want] {
			t.Errorf("missing id %q in %v", want, ids)
		}
	}
}

// TestDuplicateIDIsSkipped: two rows with one id would produce two output rows
// claiming to be the same item, and a checkpoint that cannot distinguish them.
func TestDuplicateIDIsSkipped(t *testing.T) {
	p := newTestProcessor(t, Options{}, okProvider("openai"))
	input := `{"id":"a","prompt":"one"}
{"id":"a","prompt":"again"}
{"id":"b","prompt":"two"}
`
	results, _ := runBatch(t, p, input, "")
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (duplicate id dropped)", len(results))
	}
}

// TestExtraFieldsPassThrough keeps a batch joinable to its source rows.
func TestExtraFieldsPassThrough(t *testing.T) {
	p := newTestProcessor(t, Options{}, okProvider("openai"))
	results, _ := runBatch(t, p, `{"id":"a","prompt":"q","ticket":"T-42"}`+"\n", "")
	if len(results) != 1 || results[0]["ticket"] != "T-42" {
		t.Fatalf("passthrough field lost: %v", results)
	}
}

// === Fan-out ===

// TestWorkerFanOutCompletesEveryItem defends against the classic worker-pool
// bug where the feeder or the writer drops the tail of the queue.
func TestWorkerFanOutCompletesEveryItem(t *testing.T) {
	const n = 50
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "{\"id\":\"item-%d\",\"prompt\":\"q%d\"}\n", i, i)
	}
	p := newTestProcessor(t, Options{Workers: 8}, okProvider("openai"))
	results, stats := runBatch(t, p, sb.String(), "")

	if len(results) != n {
		t.Fatalf("wrote %d results, want %d", len(results), n)
	}
	if stats.Completed != n || stats.Succeeded != n || stats.Skipped != 0 {
		t.Fatalf("stats = %+v, want %d completed and succeeded", stats, n)
	}
	seen := map[string]bool{}
	for _, r := range results {
		seen[r["id"].(string)] = true
	}
	for i := 0; i < n; i++ {
		if !seen[fmt.Sprintf("item-%d", i)] {
			t.Fatalf("item-%d never produced a result", i)
		}
	}
}

// TestMultiProviderRunsTheJudge covers the synthesis path, which single-provider
// batches skip entirely.
func TestMultiProviderRunsTheJudge(t *testing.T) {
	p := newTestProcessor(t, Options{}, okProvider("openai"), okProvider("gemini"))
	results, _ := runBatch(t, p, `{"id":"a","prompt":"q"}`+"\n", "")
	if len(results) != 1 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0]["verdict"] != "YES" || results[0]["confidence"] != "high" {
		t.Fatalf("judge verdict not carried through: %v", results[0])
	}
}

// === Retries ===

// TestRateLimitErrorIsRetried defends against a single 429 discarding an item
// that would have succeeded a second later.
func TestRateLimitErrorIsRetried(t *testing.T) {
	flaky := &fakeProvider{
		name: "openai", model: "fake-model",
		answer: func(attempt int32, prompt string) (string, error) {
			if attempt == 1 {
				return "", fmt.Errorf("HTTP 429: rate limit exceeded")
			}
			return "recovered", nil
		},
	}
	p := newTestProcessor(t, Options{Workers: 1, Retries: 2}, flaky)
	results, stats := runBatch(t, p, `{"id":"a","prompt":"q"}`+"\n", "")

	if stats.Succeeded != 1 || stats.Failed != 0 {
		t.Fatalf("stats = %+v, want the retry to succeed", stats)
	}
	if results[0]["verdict"] != "recovered" {
		t.Fatalf("result did not come from the retry: %v", results[0])
	}
	if got := flaky.calls.Load(); got != 2 {
		t.Fatalf("provider called %d times, want 2 (one failure, one retry)", got)
	}
}

// TestRetriesExhaustedMarksFailure: a permanently failing item must be recorded
// as a failure, not silently dropped, so --resume does not chase it forever.
func TestRetriesExhaustedMarksFailure(t *testing.T) {
	dead := &fakeProvider{
		name: "openai", model: "fake-model",
		answer: func(int32, string) (string, error) {
			return "", fmt.Errorf("HTTP 500: upstream exploded")
		},
	}
	// Retries=1 means 2 attempts, with a 1s backoff between them.
	p := newTestProcessor(t, Options{Workers: 1, Retries: 1}, dead)
	results, stats := runBatch(t, p, `{"id":"a","prompt":"q"}`+"\n", "")

	if stats.Failed != 1 || stats.Succeeded != 0 {
		t.Fatalf("stats = %+v, want one failure", stats)
	}
	errStr, _ := results[0]["error"].(string)
	if !strings.Contains(errStr, "after 2 attempts") {
		t.Fatalf("error does not name the attempt count: %q", errStr)
	}
	if got := dead.calls.Load(); got != 2 {
		t.Fatalf("provider called %d times, want 2", got)
	}
}

func TestIsRateLimitError(t *testing.T) {
	cases := map[string]bool{
		"HTTP 429 Too Many Requests":     true,
		"openai: rate limit exceeded":    true,
		"Too Many Requests":              true,
		"HTTP 500 internal server error": false,
		"context deadline exceeded":      false,
	}
	for msg, want := range cases {
		if got := isRateLimitError(fmt.Errorf("%s", msg)); got != want {
			t.Errorf("isRateLimitError(%q) = %v, want %v", msg, got, want)
		}
	}
	if isRateLimitError(nil) {
		t.Error("nil error must not be a rate limit")
	}
}

// === Checkpoint / resume ===

// TestResumeSkipsProcessedIDs defends against paying twice for work a previous
// run already completed.
func TestResumeSkipsProcessedIDs(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.jsonl")
	if err := os.WriteFile(outPath+".checkpoint", []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prov := okProvider("openai")
	p := newTestProcessor(t, Options{OutputPath: outPath, Resume: true}, prov)
	input := `{"id":"a","prompt":"one"}
{"id":"b","prompt":"two"}
{"id":"c","prompt":"three"}
`
	results, stats := runBatch(t, p, input, "")

	if len(results) != 1 || results[0]["id"] != "c" {
		t.Fatalf("expected only item c to run, got %v", results)
	}
	if stats.Total != 1 {
		t.Fatalf("stats.Total = %d, want 1 (a and b filtered before dispatch)", stats.Total)
	}
	if got := prov.calls.Load(); got != 1 {
		t.Fatalf("provider called %d times, want 1", got)
	}

	// The checkpoint must now contain all three ids for the next resume.
	cp := NewCheckpoint(outPath)
	if err := cp.Load(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if !cp.IsProcessed(id) {
			t.Errorf("checkpoint lost id %q", id)
		}
	}
}

// === Cost ===

// TestEstimateCostPrefersCatalogOverFallback pins the precedence: the live
// catalog is the source of truth and fallbackCosts is a last resort.
func TestEstimateCostPrefersCatalogOverFallback(t *testing.T) {
	cat := pricing.NewCatalog([]pricing.Model{
		{ID: "openai/gpt-test", InputPerM: 2, OutputPerM: 4},
	})
	p := &Processor{pricing: cat}

	resp := []providers.Response{{
		Provider: "openai", Model: "gpt-test", Status: "success",
		Metrics: &providers.Metrics{InputTokens: 1_000_000, OutputTokens: 1_000_000},
	}}
	// Catalog: 1M * $2 + 1M * $4 = $6. The fallback table would say $0.45.
	if got := p.estimateCost(resp, nil); got < 5.999 || got > 6.001 {
		t.Fatalf("estimateCost = %v, want 6.00 from the catalog", got)
	}
}

func TestEstimateCostFallsBackWhenCatalogMisses(t *testing.T) {
	// Nil catalog: the compiled fallback table must still produce a number, so
	// a fully offline batch can be budgeted at all.
	p := &Processor{pricing: nil}
	resp := []providers.Response{{
		Provider: "openai", Model: "gpt-5-nano", Status: "success",
		Metrics: &providers.Metrics{InputTokens: 1_000_000, OutputTokens: 1_000_000},
	}}
	want := fallbackCosts["openai"].in + fallbackCosts["openai"].out
	if got := p.estimateCost(resp, nil); got < want-0.001 || got > want+0.001 {
		t.Fatalf("estimateCost = %v, want %v from fallbackCosts", got, want)
	}
}

func TestEstimateCostIgnoresCachedResponses(t *testing.T) {
	cat := pricing.NewCatalog([]pricing.Model{{ID: "openai/gpt-test", InputPerM: 2, OutputPerM: 4}})
	p := &Processor{pricing: cat}
	resp := []providers.Response{{
		Provider: "openai", Model: "gpt-test", Status: "success", Cached: true,
		Metrics: &providers.Metrics{InputTokens: 1_000_000, OutputTokens: 1_000_000},
	}}
	if got := p.estimateCost(resp, nil); got != 0 {
		t.Fatalf("a cache hit was billed at %v", got)
	}
}

func TestEstimateCostIncludesTheJudge(t *testing.T) {
	cat := pricing.NewCatalog([]pricing.Model{{ID: "anthropic/judge-test", InputPerM: 10, OutputPerM: 10}})
	p := &Processor{pricing: cat}
	v := &judge.Verdict{JudgeProvider: "claude", JudgeModel: "judge-test", JudgeTokens: 1_000_000}
	// Flat $10/M both ways, so the 70/30 split is irrelevant: $10 total.
	if got := p.estimateCost(nil, v); got < 9.99 || got > 10.01 {
		t.Fatalf("judge cost = %v, want 10.00", got)
	}
}

// === Budget ===

// TestBudgetStopsDispatchAndLeavesTheRestResumable is the adversary this flag
// exists for: an overnight batch that quietly spends far more than intended.
func TestBudgetStopsDispatchAndLeavesTheRestResumable(t *testing.T) {
	const n = 30
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "{\"id\":\"item-%d\",\"prompt\":\"q\"}\n", i)
	}

	// $1.00 per item: 1M input tokens at $1/M, nothing on output.
	cat := pricing.NewCatalog([]pricing.Model{{ID: "openai/gpt-test", InputPerM: 1, OutputPerM: 0}})
	prov := okProvider("openai")
	prov.model = "gpt-test"
	prov.metrics = &providers.Metrics{InputTokens: 1_000_000}
	// Real providers take seconds; an instant fake lets the feeder outrun the
	// bookkeeping goroutine and inflates the overshoot beyond anything a real
	// run would see. A small delay keeps the test honest about the mechanism.
	prov.delay = 20 * time.Millisecond

	outPath := filepath.Join(t.TempDir(), "out.jsonl")
	p := newTestProcessor(t, Options{
		Workers: 2, Budget: 3.00, Pricing: cat, OutputPath: outPath, Resume: true,
	}, prov)
	results, stats := runBatch(t, p, sb.String(), "")

	if !stats.BudgetStopped {
		t.Fatalf("budget was not reported as hit: %+v", stats)
	}
	if stats.Completed >= n {
		t.Fatalf("every item ran despite a $3 cap on $%d of work", n)
	}
	if stats.Skipped != n-stats.Completed || stats.Skipped == 0 {
		t.Fatalf("stats = %+v, want Skipped = Total - Completed and non-zero", stats)
	}
	if stats.TotalCost < 3.00 {
		t.Fatalf("stopped at $%.2f, before reaching the $3.00 cap", stats.TotalCost)
	}
	// The cap is enforced against RECORDED spend, so the overshoot window is
	// everything dispatched but not yet accounted: items in flight, items
	// buffered in the channel (also `workers`), and results still being
	// written. The bound below is deliberately generous — this assertion
	// exists to catch a cap that is ignored outright, not to pin a race.
	if maxItems := 3 + 4*2; stats.Completed > maxItems {
		t.Fatalf("completed %d items, far past what the $3 cap plus in-flight work allows (%d)", stats.Completed, maxItems)
	}

	// Undispatched items must be absent from the checkpoint so --resume runs them.
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	cp := NewCheckpoint(outPath)
	if err := cp.Load(); err != nil {
		t.Fatal(err)
	}
	if cp.ProcessedCount() != stats.Completed {
		t.Fatalf("checkpoint holds %d ids but %d items completed", cp.ProcessedCount(), stats.Completed)
	}
	for _, r := range results {
		if !cp.IsProcessed(r["id"].(string)) {
			t.Fatalf("completed item %v missing from the checkpoint", r["id"])
		}
	}
}

// TestNoBudgetRunsEverything guards the default: 0 means uncapped, not "stop
// immediately".
func TestNoBudgetRunsEverything(t *testing.T) {
	cat := pricing.NewCatalog([]pricing.Model{{ID: "openai/gpt-test", InputPerM: 1000, OutputPerM: 1000}})
	prov := okProvider("openai")
	prov.model = "gpt-test"
	prov.metrics = &providers.Metrics{InputTokens: 1_000_000}

	p := newTestProcessor(t, Options{Workers: 2, Budget: 0, Pricing: cat}, prov)
	input := `{"id":"a","prompt":"q"}
{"id":"b","prompt":"q"}
{"id":"c","prompt":"q"}
`
	_, stats := runBatch(t, p, input, "")
	if stats.BudgetStopped || stats.Completed != 3 {
		t.Fatalf("stats = %+v, want all 3 items with no budget stop", stats)
	}
}

// TestEmptyInputIsNotAnError guards the divide-by-zero the summary used to hit.
func TestEmptyInputIsNotAnError(t *testing.T) {
	p := newTestProcessor(t, Options{}, okProvider("openai"))
	_, stats := runBatch(t, p, "\n\n", "")
	if stats.Total != 0 || stats.Completed != 0 {
		t.Fatalf("stats = %+v, want an empty run", stats)
	}
}

// TestDefaultPromptIsUsedWhenItemHasNone covers `--batch file "prompt"`.
func TestDefaultPromptIsUsedWhenItemHasNone(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	prov := &fakeProvider{
		name: "openai", model: "fake-model",
		answer: func(_ int32, prompt string) (string, error) {
			mu.Lock()
			seen = append(seen, prompt)
			mu.Unlock()
			return "ok", nil
		},
	}
	p := newTestProcessor(t, Options{Workers: 1}, prov)
	runBatch(t, p, `{"id":"a","context":"CTX"}`+"\n", "the default question")

	if len(seen) != 1 || !strings.Contains(seen[0], "the default question") || !strings.Contains(seen[0], "CTX") {
		t.Fatalf("prompt assembly wrong: %q", seen)
	}
}

// TestItemWithNoPromptAtAllFails: silently answering nothing would produce a
// confident-looking empty verdict.
func TestItemWithNoPromptAtAllFails(t *testing.T) {
	p := newTestProcessor(t, Options{Workers: 1}, okProvider("openai"))
	results, stats := runBatch(t, p, `{"id":"a"}`+"\n", "")
	if stats.Failed != 1 {
		t.Fatalf("stats = %+v, want one failure", stats)
	}
	if errStr, _ := results[0]["error"].(string); !strings.Contains(errStr, "no prompt") {
		t.Fatalf("error = %q, want it to name the missing prompt", errStr)
	}
}

// TestCancellationMarksTheRunPartial: a batch cut short by Ctrl-C leaves a
// PARTIAL output file. If that is not recorded, a pipeline treats a
// half-finished JSONL as the complete answer.
func TestCancellationMarksTheRunPartial(t *testing.T) {
	const n = 40
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "{\"id\":\"item-%d\",\"prompt\":\"q\"}\n", i)
	}

	prov := okProvider("openai")
	prov.delay = 30 * time.Millisecond
	p := newTestProcessor(t, Options{Workers: 2}, prov)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()

	var out bytes.Buffer
	stats, err := p.Process(ctx, strings.NewReader(sb.String()), &out, "")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !stats.Cancelled {
		t.Fatalf("stats = %+v, want Cancelled", stats)
	}
	if stats.Skipped == 0 {
		t.Fatalf("stats = %+v, want undispatched items recorded", stats)
	}
	if stats.BudgetStopped {
		t.Fatal("a cancellation must not be reported as a budget stop")
	}
	if stats.Completed+stats.Skipped != stats.Total {
		t.Fatalf("completed %d + skipped %d != total %d", stats.Completed, stats.Skipped, stats.Total)
	}
}

// TestCleanRunIsNotMarkedPartial is the other half: a run that finishes must
// not look interrupted, or every batch would exit non-zero.
func TestCleanRunIsNotMarkedPartial(t *testing.T) {
	p := newTestProcessor(t, Options{Workers: 2}, okProvider("openai"))
	input := `{"id":"a","prompt":"q"}
{"id":"b","prompt":"q"}
`
	_, stats := runBatch(t, p, input, "")
	if stats.Cancelled || stats.Skipped != 0 || stats.BudgetStopped {
		t.Fatalf("stats = %+v, want a clean run", stats)
	}
}

// TestBudgetStopIsNotMarkedCancelled keeps the two exit reasons distinct: the
// summary and the error message differ, and so does what the user should do.
func TestBudgetStopIsNotMarkedCancelled(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "{\"id\":\"item-%d\",\"prompt\":\"q\"}\n", i)
	}
	cat := pricing.NewCatalog([]pricing.Model{{ID: "openai/gpt-test", InputPerM: 1, OutputPerM: 0}})
	prov := okProvider("openai")
	prov.model = "gpt-test"
	prov.metrics = &providers.Metrics{InputTokens: 1_000_000}
	prov.delay = 20 * time.Millisecond

	p := newTestProcessor(t, Options{Workers: 2, Budget: 2.00, Pricing: cat}, prov)
	_, stats := runBatch(t, p, sb.String(), "")

	if !stats.BudgetStopped {
		t.Fatalf("stats = %+v, want a budget stop", stats)
	}
	if stats.Cancelled {
		t.Fatal("a budget stop must not be reported as a cancellation")
	}
}

// slowWriter models a slow output sink (a file on a busy disk, a pipe whose
// reader is behind). It is what makes the result channel back up, which is the
// only condition under which a worker's send can lose a select race against a
// cancelled context.
type slowWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	delay time.Duration
}

func (w *slowWriter) Write(b []byte) (int, error) {
	time.Sleep(w.delay)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(b)
}

func (w *slowWriter) lines() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, l := range strings.Split(strings.TrimSpace(w.buf.String()), "\n") {
		if l != "" {
			n++
		}
	}
	return n
}

// TestCancellationDiscardsNoPaidWork is the adversary a shutdown path invites:
// a worker that has already called the provider, been billed, and produced a
// result, then throws it away because the context ended while its send was
// waiting on a backed-up writer. The item leaves no output line, no checkpoint
// entry and no trace that it ran.
//
// The slow writer is load-bearing. With a fast sink the send never blocks, the
// race never happens, and the test passes whether or not the bug is present.
func TestCancellationDiscardsNoPaidWork(t *testing.T) {
	const n = 24
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "{\"id\":\"item-%d\",\"prompt\":\"q\"}\n", i)
	}

	prov := okProvider("openai")
	p := newTestProcessor(t, Options{Workers: 2}, prov)

	out := &slowWriter{delay: 8 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(40 * time.Millisecond)
		cancel()
	}()

	stats, err := p.Process(ctx, strings.NewReader(sb.String()), out, "")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	if lines := out.lines(); lines != stats.Completed {
		t.Fatalf("wrote %d result lines but Completed says %d", lines, stats.Completed)
	}
	if got := int(prov.finished.Load()); got > stats.Completed {
		t.Fatalf("%d provider calls completed but only %d results were written: %d paid results were discarded",
			got, stats.Completed, got-stats.Completed)
	}
	if stats.Completed == 0 {
		t.Fatal("nothing completed before cancellation; the test proves nothing")
	}
	if stats.Completed+stats.Skipped != stats.Total {
		t.Fatalf("completed %d + skipped %d != total %d", stats.Completed, stats.Skipped, stats.Total)
	}
}

// TestCancelledItemsAreNotCheckpointed is the worst failure this package can
// have: an item that never ran being recorded as done, so --resume skips it
// forever and it silently vanishes from the batch. After a cancellation the
// workers still drain a buffered channel, and every one of those items would
// otherwise produce a "context canceled" result that got written AND marked.
func TestCancelledItemsAreNotCheckpointed(t *testing.T) {
	const n = 40
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "{\"id\":\"item-%d\",\"prompt\":\"q\"}\n", i)
	}

	outPath := filepath.Join(t.TempDir(), "out.jsonl")
	prov := okProvider("openai")
	prov.delay = 20 * time.Millisecond
	p := newTestProcessor(t, Options{Workers: 2, OutputPath: outPath}, prov)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	var out bytes.Buffer
	stats, err := p.Process(ctx, strings.NewReader(sb.String()), &out, "")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !stats.Cancelled || stats.Skipped == 0 {
		t.Fatalf("stats = %+v, want an interrupted run", stats)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	cp := NewCheckpoint(outPath)
	if err := cp.Load(); err != nil {
		t.Fatal(err)
	}

	// Every checkpointed id must correspond to a real, successful result line.
	succeeded := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad result line %q: %v", line, err)
		}
		if _, isErr := m["error"]; !isErr {
			succeeded[m["id"].(string)] = true
		}
	}
	if cp.ProcessedCount() > len(succeeded) {
		t.Fatalf("checkpoint holds %d ids but only %d items produced a real result: %d items would be skipped forever",
			cp.ProcessedCount(), len(succeeded), cp.ProcessedCount()-len(succeeded))
	}
	for id := range succeeded {
		if !cp.IsProcessed(id) {
			t.Fatalf("successful item %s is missing from the checkpoint, so it would be re-paid on resume", id)
		}
	}
}

// TestCheckpointExistsWithoutResumeSoTheHintIsTrue: the interrupted and
// budget-stop summaries both tell the user to rerun with --resume. That advice
// is only true if the FIRST run left a checkpoint behind.
func TestCheckpointExistsWithoutResumeSoTheHintIsTrue(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.jsonl")
	p := newTestProcessor(t, Options{Workers: 1, OutputPath: outPath}, okProvider("openai"))
	runBatch(t, p, "{\"id\":\"a\",\"prompt\":\"q\"}\n{\"id\":\"b\",\"prompt\":\"q\"}\n", "")
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	cp := NewCheckpoint(outPath)
	if err := cp.Load(); err != nil {
		t.Fatal(err)
	}
	if !cp.IsProcessed("a") || !cp.IsProcessed("b") {
		t.Fatalf("a run without --resume left no usable checkpoint (%d ids), so the resume hint is a lie", cp.ProcessedCount())
	}
}

// TestFreshRunClearsAStaleCheckpoint: without --resume the output file is
// truncated, so a checkpoint left from a previous run would make a LATER
// --resume skip items that are no longer in the output.
func TestFreshRunClearsAStaleCheckpoint(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.jsonl")
	if err := os.WriteFile(outPath+".checkpoint", []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prov := okProvider("openai")
	p := newTestProcessor(t, Options{Workers: 1, OutputPath: outPath}, prov)
	results, _ := runBatch(t, p, "{\"id\":\"a\",\"prompt\":\"q\"}\n", "")
	if len(results) != 1 {
		t.Fatalf("a fresh run honoured a stale checkpoint and skipped work: %v", results)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	cp := NewCheckpoint(outPath)
	if err := cp.Load(); err != nil {
		t.Fatal(err)
	}
	if cp.IsProcessed("b") || cp.IsProcessed("c") {
		t.Fatal("stale ids survived a fresh run; a later --resume would skip items absent from the output")
	}
}

// TestJudgeFailureStillCountsThePanelSpend: the providers answered and were
// billed. If that cost never reaches stats.TotalCost, a broken judge model
// makes --budget unenforceable.
func TestJudgeFailureStillCountsThePanelSpend(t *testing.T) {
	// Both vendors are priced so the expected total is exact; a model missing
	// from the catalog would fall back to the compiled table and muddy it.
	cat := pricing.NewCatalog([]pricing.Model{
		{ID: "openai/gpt-test", InputPerM: 1, OutputPerM: 0},
		{ID: "google/gpt-test", InputPerM: 1, OutputPerM: 0},
	})
	prov := okProvider("openai")
	prov.model = "gpt-test"
	prov.metrics = &providers.Metrics{InputTokens: 1_000_000}
	second := okProvider("gemini")
	second.model = "gpt-test"
	second.metrics = &providers.Metrics{InputTokens: 1_000_000}

	brokenJudge := &fakeProvider{
		name: "claude", model: "judge",
		answer: func(int32, string) (string, error) { return "", fmt.Errorf("judge model not found") },
	}

	p := newTestProcessor(t, Options{Workers: 1, Pricing: cat, Judge: brokenJudge}, prov, second)
	_, stats := runBatch(t, p, "{\"id\":\"a\",\"prompt\":\"q\"}\n", "")

	if stats.Failed != 1 {
		t.Fatalf("stats = %+v, want the judge failure recorded", stats)
	}
	if stats.TotalCost < 1.999 {
		t.Fatalf("TotalCost = %v, want the two billed provider calls (~2.00) counted despite the judge failing", stats.TotalCost)
	}
}

// TestNonPositiveWorkerCountIsRefused: Process sizes its channel and worker
// pool by Workers, so 0 deadlocked the feeder forever and -1 panicked in
// make(chan). NewProcessor must refuse both before anything runs.
func TestNonPositiveWorkerCountIsRefused(t *testing.T) {
	for _, n := range []int{0, -1} {
		if _, err := NewProcessor(Options{Workers: n}); err == nil {
			t.Fatalf("Workers=%d was accepted", n)
		}
	}
}

// failingWriter models a full disk or a closed pipe: every write fails.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }

// TestUnwrittenResultsAreNotCheckpointedOrCounted defends against a batch that
// loses its results and reports success. A failed Encode was only a stderr
// warning: the item was still checkpointed (so --resume skipped it for good,
// with no line in the output) and still counted as Succeeded, and the command
// exited 0 with an empty output file.
func TestUnwrittenResultsAreNotCheckpointedOrCounted(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.jsonl")
	p := newTestProcessor(t, Options{Workers: 1, OutputPath: outPath}, okProvider("openai"))
	stats, err := p.Process(context.Background(),
		strings.NewReader("{\"id\":\"a\",\"prompt\":\"q\"}\n{\"id\":\"b\",\"prompt\":\"q\"}\n"), failingWriter{}, "")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if stats.WriteFailed != 2 || stats.Succeeded != 0 {
		t.Fatalf("stats = %+v, want 2 write failures and 0 successes", stats)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	cp := NewCheckpoint(outPath)
	if err := cp.Load(); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if cp.ProcessedCount() != 0 {
		t.Fatalf("checkpoint holds %d ids whose results were never written; --resume would skip them forever", cp.ProcessedCount())
	}
}

// TestJudgeParseErrorIsAFailedItem: an unparseable synthesis became a result
// with verdict "PARSE_ERROR", counted as Succeeded, so a judge that never once
// produced JSON still reported a 100% success rate.
func TestJudgeParseErrorIsAFailedItem(t *testing.T) {
	prose := &fakeProvider{name: "claude", model: "fake-judge",
		answer: func(int32, string) (string, error) { return "I think both are right.", nil }}
	p := newTestProcessor(t, Options{Judge: prose}, okProvider("openai"), okProvider("gemini"))
	results, stats := runBatch(t, p, `{"id":"a","prompt":"q"}`+"\n", "")
	if stats.Succeeded != 0 || stats.Failed != 1 {
		t.Fatalf("stats = %+v, want the item counted as failed", stats)
	}
	if e, _ := results[0]["error"].(string); !strings.Contains(e, "I think both are right.") {
		t.Fatalf("error %q should carry the judge's text", e)
	}
}

// TestOverlongLineNamesTheLine: a line past the scanner limit aborted the run
// with a bare "bufio.Scanner: token too long", leaving the user to bisect a
// large JSONL by hand to find it.
func TestOverlongLineNamesTheLine(t *testing.T) {
	p := newTestProcessor(t, Options{}, okProvider("openai"))
	long := `{"id":"big","prompt":"` + strings.Repeat("x", maxBatchLine) + `"}`
	_, err := p.Process(context.Background(), strings.NewReader(`{"id":"a","prompt":"q"}`+"\n"+long+"\n"), &bytes.Buffer{}, "")
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("got %v, want an error naming line 2", err)
	}
}

// TestResumeRetriesFailedItems: failed items were checkpointed like successes,
// so --resume skipped them for good and the only way to retry a rate-limited
// or timed-out item was to hand-edit the checkpoint.
func TestResumeRetriesFailedItems(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.jsonl")
	flaky := &fakeProvider{name: "openai", model: "m",
		answer: func(_ int32, prompt string) (string, error) {
			if prompt == "bad" {
				return "", errors.New("upstream 500")
			}
			return "ok", nil
		}}
	p := newTestProcessor(t, Options{Workers: 1, OutputPath: outPath}, flaky)
	_, stats := runBatch(t, p, "{\"id\":\"good\",\"prompt\":\"fine\"}\n{\"id\":\"flaky\",\"prompt\":\"bad\"}\n", "")
	if stats.Failed != 1 {
		t.Fatalf("stats = %+v, want one failure", stats)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	cp := NewCheckpoint(outPath)
	if err := cp.Load(); err != nil {
		t.Fatal(err)
	}
	if !cp.IsProcessed("good") {
		t.Fatal("the successful item is missing from the checkpoint and would be re-paid")
	}
	if cp.IsProcessed("flaky") {
		t.Fatal("the failed item is checkpointed, so --resume will never retry it")
	}
}
