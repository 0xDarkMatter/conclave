package output

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// renderJSONTo runs the JSON renderer and returns the decoded document.
// renderJSON writes to os.Stdout directly, so the pipe swap is unavoidable.
func renderJSONTo(t *testing.T, f *Formatter, r Result) JSONOutput {
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
	var out JSONOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, buf.String())
	}
	return out
}

// TestJSONReportsTheTimeoutInForce defends against a machine-readable field
// that silently reads 0 whatever -t was given. A consumer reading
// execution.timeout_seconds to decide whether a slow provider was cut off
// would draw the wrong conclusion from a hardcoded zero.
func TestJSONReportsTheTimeoutInForce(t *testing.T) {
	result := Result{
		Query:     "q",
		Providers: []string{"openai"},
		Timeout:   45,
		Responses: []providers.Response{
			{Provider: "openai", Model: "m", Status: "success", Response: "a"},
		},
	}

	out := renderJSONTo(t, New(Options{JSON: true, Timeout: 45}), result)
	if out.Execution.TimeoutSeconds != 45 {
		t.Fatalf("timeout_seconds = %d, want 45", out.Execution.TimeoutSeconds)
	}
}

// TestJSONTimeoutFallsBackToTheResult: the value lives on both the formatter
// options and the Result, and a caller that fills only one must not get a 0.
func TestJSONTimeoutFallsBackToTheResult(t *testing.T) {
	result := Result{
		Query:     "q",
		Providers: []string{"openai"},
		Timeout:   30,
		Responses: []providers.Response{
			{Provider: "openai", Model: "m", Status: "success", Response: "a"},
		},
	}

	out := renderJSONTo(t, New(Options{JSON: true}), result)
	if out.Execution.TimeoutSeconds != 30 {
		t.Fatalf("timeout_seconds = %d, want 30 from the Result", out.Execution.TimeoutSeconds)
	}
}

// TestJSONMarksAnUnderstatedTotal is the trap a machine consumer falls into:
// a cache hit is a known zero, so a panel of one cached response and one
// unpriceable live response yields a total of 0 that looks complete. The
// styled output says "+"; JSON has no room for that, so it needs a flag.
func TestJSONMarksAnUnderstatedTotal(t *testing.T) {
	cat := pricing.NewCatalog([]pricing.Model{
		{ID: "openai/gpt-test", InputPerM: 1, OutputPerM: 0},
	})
	cached := providers.Response{
		Provider: "openai", Model: "gpt-test", Status: "success", Cached: true,
		Metrics: &providers.Metrics{InputTokens: 1_000_000},
	}
	unpriceable := providers.Response{
		Provider: "claude", Model: "not-in-catalog", Status: "success",
		Metrics: &providers.Metrics{InputTokens: 1_000_000},
	}

	out := renderJSONTo(t, New(Options{JSON: true, APIMode: true, Pricing: cat}),
		Result{Query: "q", Providers: []string{"openai", "claude"},
			Responses: []providers.Response{cached, unpriceable}})

	if out.Meta.TotalCostUSD == nil {
		t.Fatal("total is absent; the cached zero should still be reported")
	}
	if *out.Meta.TotalCostUSD != 0 {
		t.Fatalf("total = %v, want 0 (only the cached response was priceable)", *out.Meta.TotalCostUSD)
	}
	if !out.Meta.TotalCostPartial {
		t.Fatal("total_cost_partial is false, so a consumer reads an understated 0 as the whole bill")
	}
	if out.Responses["claude"].CostUSD != nil {
		t.Fatal("an unpriceable response must have no cost_usd at all")
	}
}

// TestJSONCompleteTotalIsNotMarkedPartial is the inverse: flagging every total
// would make the flag meaningless.
func TestJSONCompleteTotalIsNotMarkedPartial(t *testing.T) {
	cat := pricing.NewCatalog([]pricing.Model{{ID: "openai/gpt-test", InputPerM: 1, OutputPerM: 0}})
	out := renderJSONTo(t, New(Options{JSON: true, APIMode: true, Pricing: cat}),
		Result{Query: "q", Providers: []string{"openai"},
			Responses: []providers.Response{{
				Provider: "openai", Model: "gpt-test", Status: "success",
				Metrics: &providers.Metrics{InputTokens: 1_000_000},
			}}})

	if out.Meta.TotalCostPartial {
		t.Fatal("a fully priced panel was marked partial")
	}
	if out.Meta.TotalCostUSD == nil || *out.Meta.TotalCostUSD != 1 {
		t.Fatalf("total = %v, want 1.00", out.Meta.TotalCostUSD)
	}
}

// TestCachedHitKeepsStatusSuccessInJSON is the same contract as the
// orchestrator test, asserted at the boundary consumers actually parse.
// A downstream reader treats any status other than "success" as a panel
// failure, so a cache hit that changed status would turn a healthy run into a
// phantom degradation.
func TestCachedHitKeepsStatusSuccessInJSON(t *testing.T) {
	cached := providers.Response{
		Provider: "openai", Model: "gpt-test", Status: "success", Cached: true,
		Response: "an answer",
		Metrics:  &providers.Metrics{InputTokens: 5, OutputTokens: 7},
	}
	out := renderJSONTo(t, New(Options{JSON: true, APIMode: true, Pricing: nil}),
		Result{Query: "q", Providers: []string{"openai"},
			Responses: []providers.Response{cached}})

	got := out.Responses["openai"]
	if got.Status != "success" {
		t.Fatalf("status = %q, want \"success\": a consumer would read this as a panel failure", got.Status)
	}
	if !got.Cached {
		t.Fatal("cached: true is missing, so the hit is invisible to a consumer")
	}
	if got.Error != "" {
		t.Fatalf("a cache hit carries an error: %q", got.Error)
	}
	if got.Response != "an answer" {
		t.Fatalf("response body = %q", got.Response)
	}
}
