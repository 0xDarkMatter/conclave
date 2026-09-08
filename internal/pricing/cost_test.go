package pricing

import "testing"

func costCatalog() *Catalog {
	return NewCatalog([]Model{
		{ID: "openai/gpt-test", InputPerM: 2, OutputPerM: 10},
		{ID: "deepseek/deepseek-v4", InputPerM: 1, OutputPerM: 2},
	})
}

func TestCostOf(t *testing.T) {
	c := costCatalog()
	// 1M in at $2 + 0.5M out at $10 = 2.00 + 5.00
	got, ok := c.CostOf("openai", "gpt-test", 1_000_000, 500_000)
	if !ok || got < 6.999 || got > 7.001 {
		t.Fatalf("CostOf = %v, %v; want 7.00, true", got, ok)
	}
}

// TestCostOfSlashRoutedModel: an OpenRouter token is both provider and model.
func TestCostOfSlashRoutedModel(t *testing.T) {
	got, ok := costCatalog().CostOf("deepseek/deepseek-v4", "deepseek/deepseek-v4", 1_000_000, 1_000_000)
	if !ok || got < 2.999 || got > 3.001 {
		t.Fatalf("CostOf = %v, %v; want 3.00, true", got, ok)
	}
}

// TestCostOfUnknownModelIsNotZero is the whole reason this returns a bool:
// a caller must be able to tell "free" from "no idea".
func TestCostOfUnknownModelIsNotZero(t *testing.T) {
	if _, ok := costCatalog().CostOf("claude", "not-listed", 1_000_000, 1_000_000); ok {
		t.Fatal("an unlisted model reported a usable price")
	}
}

// TestNilCatalogCostsNothingAndSaysSo keeps the advisory contract: the catalog
// may be nil offline, and every caller must survive it.
func TestNilCatalogCostsNothingAndSaysSo(t *testing.T) {
	var c *Catalog
	if _, ok := c.CostOf("openai", "gpt-test", 1, 1); ok {
		t.Fatal("a nil catalog reported a usable price")
	}
	if _, ok := c.JudgeCostOf("openai", "gpt-test", 1); ok {
		t.Fatal("a nil catalog priced a judge call")
	}
}

func TestJudgeCostOfAppliesTheSplit(t *testing.T) {
	// 1M tokens at 70/30: 700k in at $2 = 1.40, 300k out at $10 = 3.00
	got, ok := costCatalog().JudgeCostOf("openai", "gpt-test", 1_000_000)
	if !ok || got < 4.399 || got > 4.401 {
		t.Fatalf("JudgeCostOf = %v, %v; want 4.40, true", got, ok)
	}
}

// TestJudgeSplitIsSharedNotCopied guards the constant that used to be
// duplicated in internal/output and internal/batch, where the two copies had
// already begun to diverge.
func TestJudgeSplitIsSharedNotCopied(t *testing.T) {
	if JudgeInputShare <= 0 || JudgeInputShare >= 1 {
		t.Fatalf("JudgeInputShare = %v, want a fraction", JudgeInputShare)
	}
	// A flat price makes the split irrelevant, which pins that both halves are
	// applied and that they sum to the whole.
	flat := NewCatalog([]Model{{ID: "openai/flat", InputPerM: 10, OutputPerM: 10}})
	got, ok := flat.JudgeCostOf("openai", "flat", 1_000_000)
	if !ok || got < 9.99 || got > 10.01 {
		t.Fatalf("JudgeCostOf = %v, want 10.00: the two halves do not sum to the whole", got)
	}
}

func TestFormatUSD(t *testing.T) {
	cases := map[float64]string{
		0:       "$0.0000",
		-1:      "$0.0000",
		0.00001: "<$0.0001", // real, but a rounded $0.0000 would read as free
		0.0003:  "$0.0003",
		12.5:    "$12.5000",
	}
	for in, want := range cases {
		if got := FormatUSD(in); got != want {
			t.Errorf("FormatUSD(%v) = %q, want %q", in, got, want)
		}
	}
}
