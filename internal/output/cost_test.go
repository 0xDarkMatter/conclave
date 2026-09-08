package output

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/judge"
	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// testCatalog prices openai/gpt-test at $1/M in, $10/M out. claude is
// deliberately absent so the "unknown price" path is exercised.
func testCatalog() *pricing.Catalog {
	return pricing.NewCatalog([]pricing.Model{
		{ID: "openai/gpt-test", Name: "GPT Test", InputPerM: 1, OutputPerM: 10},
		{ID: "anthropic/claude-judge", Name: "Judge", InputPerM: 2, OutputPerM: 4},
	})
}

func resp(provider, model string, in, out int) providers.Response {
	return providers.Response{
		Provider: provider, Model: model, Status: "success",
		Metrics: &providers.Metrics{InputTokens: in, OutputTokens: out},
	}
}

// TestCLIModeNeverShowsDollars defends against the subscription-billing lie:
// CLI mode pays nothing per token, so a price there would be fiction.
func TestCLIModeNeverShowsDollars(t *testing.T) {
	c := computeCosts(testCatalog(), Result{Responses: []providers.Response{resp("openai", "gpt-test", 1000, 1000)}}, false)
	if c.enabled() {
		t.Fatalf("CLI mode produced a cost: %v", c.formatTotal())
	}
}

// TestUnknownPriceIsOmittedNotZero defends against printing $0.00 for a model
// the catalog has never heard of, which reads as "this was free".
func TestUnknownPriceIsOmittedNotZero(t *testing.T) {
	r := Result{Responses: []providers.Response{resp("claude", "claude-unlisted", 1000, 1000)}}
	c := computeCosts(testCatalog(), r, true)
	if c.byIndex[0] != nil {
		t.Fatalf("unlisted model was priced: %v", *c.byIndex[0])
	}
	if c.enabled() {
		t.Fatal("total should be unknown when nothing could be priced")
	}
}

// TestNilCatalogIsTolerated: the catalog is advisory and may be nil offline.
func TestNilCatalogIsTolerated(t *testing.T) {
	c := computeCosts(nil, Result{Responses: []providers.Response{resp("openai", "gpt-test", 10, 10)}}, true)
	if c.enabled() {
		t.Fatal("nil catalog must produce no cost figures")
	}
}

func TestPerResponseAndTotalCost(t *testing.T) {
	r := Result{
		Responses: []providers.Response{resp("openai", "gpt-test", 1_000_000, 100_000)},
		Verdict: &judge.Verdict{
			JudgeProvider: "claude", JudgeModel: "claude-judge", JudgeTokens: 1_000_000,
		},
	}
	c := computeCosts(testCatalog(), r, true)
	if c.byIndex[0] == nil {
		t.Fatal("expected a priced response")
	}
	// 1M in @ $1 + 0.1M out @ $10 = 1.00 + 1.00
	if got := *c.byIndex[0]; got < 1.999 || got > 2.001 {
		t.Fatalf("response cost = %v, want 2.00", got)
	}
	// judge: 700k in @ $2 + 300k out @ $4 = 1.40 + 1.20
	if c.judge == nil || *c.judge < 2.599 || *c.judge > 2.601 {
		t.Fatalf("judge cost = %v, want 2.60", c.judge)
	}
	if c.total == nil || *c.total < 4.599 || *c.total > 4.601 {
		t.Fatalf("total = %v, want 4.60", c.total)
	}
	if c.partial {
		t.Fatal("nothing was unpriceable; partial must be false")
	}
}

// TestPartialTotalIsMarked: a mixed panel must not present an understated sum
// as if it were the whole bill.
func TestPartialTotalIsMarked(t *testing.T) {
	r := Result{Responses: []providers.Response{
		resp("openai", "gpt-test", 1_000_000, 0),
		resp("claude", "claude-unlisted", 1_000_000, 0),
	}}
	c := computeCosts(testCatalog(), r, true)
	if !c.partial {
		t.Fatal("expected partial=true")
	}
	if got := c.formatTotal(); got != "$1.0000+" {
		t.Fatalf("formatTotal = %q, want $1.0000+", got)
	}
}

func TestFormatUSDSubCentIsNotRoundedToFree(t *testing.T) {
	if got := pricing.FormatUSD(0.00001); got != "<$0.0001" {
		t.Fatalf("FormatUSD(0.00001) = %q", got)
	}
	if got := pricing.FormatUSD(0); got != "$0.0000" {
		t.Fatalf("FormatUSD(0) = %q", got)
	}
}

// TestFailedResponseIsNotBilled: an errored call produced no output tokens we
// received, so pricing it would inflate the total.
func TestFailedResponseIsNotBilled(t *testing.T) {
	bad := resp("openai", "gpt-test", 1_000_000, 1_000_000)
	bad.Status = "error"
	c := computeCosts(testCatalog(), Result{Responses: []providers.Response{bad}}, true)
	if c.enabled() {
		t.Fatalf("errored response was billed: %v", c.formatTotal())
	}
}

// TestSlashRoutedModelIsPriced: an OpenRouter token is both the provider name
// and the model id (ADR-010), and the whole point of the catalog is that it
// lists exactly those slugs. Failing to price them would silently drop cost
// reporting for every OpenRouter query.
func TestSlashRoutedModelIsPriced(t *testing.T) {
	cat := pricing.NewCatalog([]pricing.Model{
		{ID: "deepseek/deepseek-v4", Name: "DeepSeek V4", InputPerM: 1, OutputPerM: 2},
	})
	r := Result{Responses: []providers.Response{
		resp("deepseek/deepseek-v4", "deepseek/deepseek-v4", 1_000_000, 1_000_000),
	}}
	c := computeCosts(cat, r, true)
	if c.byIndex[0] == nil {
		t.Fatal("an OpenRouter slug the catalog lists was not priced")
	}
	if got := *c.byIndex[0]; got < 2.999 || got > 3.001 {
		t.Fatalf("cost = %v, want 3.00", got)
	}
}

// TestCachedResponseIsAKnownZeroNotAnUnknown: a cache hit must render
// $0.0000, not vanish. A missing figure reads as "we could not price this",
// which is the opposite of what happened.
func TestCachedResponseIsAKnownZeroNotAnUnknown(t *testing.T) {
	hit := resp("claude", "claude-unlisted", 1_000_000, 1_000_000)
	hit.Cached = true
	c := computeCosts(testCatalog(), Result{Responses: []providers.Response{hit}}, true)

	if c.byIndex[0] == nil {
		t.Fatal("cached response was treated as unpriceable")
	}
	if *c.byIndex[0] != 0 {
		t.Fatalf("cached response cost %v, want 0", *c.byIndex[0])
	}
	if c.partial {
		t.Fatal("a cached response must not mark the total as understated")
	}
	if got := c.formatTotal(); got != "$0.0000" {
		t.Fatalf("formatTotal = %q, want $0.0000", got)
	}
}

// TestMixedCachedAndLivePanelTotalsOnlyTheLiveWork is the money question for a
// half-cached panel: the total must be what this run actually cost.
func TestMixedCachedAndLivePanelTotalsOnlyTheLiveWork(t *testing.T) {
	cached := resp("openai", "gpt-test", 1_000_000, 0)
	cached.Cached = true
	live := resp("openai", "gpt-test", 1_000_000, 0)

	c := computeCosts(testCatalog(), Result{Responses: []providers.Response{cached, live}}, true)
	if c.total == nil || *c.total < 0.999 || *c.total > 1.001 {
		t.Fatalf("total = %v, want 1.00 (only the live call)", c.total)
	}
	if c.partial {
		t.Fatal("nothing was unpriceable; partial must be false")
	}
}

// renderStyledTo captures the styled human output.
func renderStyledTo(t *testing.T, f *Formatter, r Result) string {
	t.Helper()
	old := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = pw
	renderErr := f.Render(r)
	pw.Close()
	os.Stdout = old
	if renderErr != nil {
		t.Fatalf("Render: %v", renderErr)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(pr); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// TestHeaderPanelAndFooterBothCarryTheTotal pins the two places the total is
// meant to appear. This exists because a rebase silently dropped the header
// row: renderHeaderPanel still accepted the costs and simply stopped using
// them, which compiles cleanly and which go vet does not flag.
func TestHeaderPanelAndFooterBothCarryTheTotal(t *testing.T) {
	r := Result{
		Query:     "q",
		Providers: []string{"openai"},
		JudgeName: "claude",
		Responses: []providers.Response{resp("openai", "gpt-test", 1_000_000, 0)},
	}
	out := renderStyledTo(t, New(Options{APIMode: true, Pricing: testCatalog()}), r)

	if n := strings.Count(out, "Cost:"); n < 2 {
		t.Fatalf("output carries %d Cost: labels, want the header panel and the footer:\n%s", n, out)
	}
	if !strings.Contains(out, "$1.0000") {
		t.Fatalf("total is missing from the output:\n%s", out)
	}
}

// TestCLIModeRendersNoCostAnywhere is the same check inverted: a
// subscription-billed run must not show a dollar figure in any position.
func TestCLIModeRendersNoCostAnywhere(t *testing.T) {
	r := Result{
		Query:     "q",
		Providers: []string{"openai"},
		Responses: []providers.Response{resp("openai", "gpt-test", 1_000_000, 0)},
	}
	out := renderStyledTo(t, New(Options{APIMode: false, Pricing: testCatalog()}), r)

	if strings.Contains(out, "Cost:") || strings.Contains(out, "$") {
		t.Fatalf("CLI mode leaked a dollar figure:\n%s", out)
	}
}
