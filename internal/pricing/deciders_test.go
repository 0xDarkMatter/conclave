// Tests for the ADR-016 hand-maintained decider pricing boundary.
// They pin cited rows, the intentionally narrow vendor-id aliases, JSON listing
// schema, and the invariant that OpenRouter Catalog.CostOf never prices this
// separate provider class.
package pricing

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDeciderCostStaysSeparateFromProviderCatalog pins ADR-016's boundary:
// decision models use their hand-maintained table and never Catalog.CostOf.
func TestDeciderCostStaysSeparateFromProviderCatalog(t *testing.T) {
	// A nil catalog is the honest stand-in: it is what an offline run, and a
	// catalog that never listed deciders, both look like to CostOf.
	var c *Catalog
	if _, ok := c.CostOf("jev", "jev-latest", 1_000_000, 0); ok {
		t.Fatal("the OpenRouter catalog must not claim to price a decider")
	}
	// Jev: $0.042 per million input tokens, output free (verified 2026-10-02
	// against the Typesafe announcement: "Output tokens: FREE (too cheap to
	// meter)").
	got, ok := DeciderCost("jev", "jev-latest", 1_000_000, 0)
	if !ok || got < 0.0419 || got > 0.0421 {
		t.Fatalf("DeciderCost(jev, 1M in) = %v, %v; want 0.042, true", got, ok)
	}
	// Clef: $0.24 per million input tokens; the output price is unpublished, so
	// the row carries 0 — output tokens must add nothing.
	got, ok = DeciderCost("clef", "clef", 1_000_000, 0)
	if !ok || got < 0.2399 || got > 0.2401 {
		t.Fatalf("DeciderCost(clef, 1M in) = %v, %v; want 0.24, true", got, ok)
	}
	if got, ok := DeciderCost("clef", "clef", 0, 1_000_000); !ok || got != 0 {
		t.Fatalf("DeciderCost(clef, output-only) = %v, %v; want 0, true — output must be free, not unpriced", got, ok)
	}
	for _, row := range DeciderPrices() {
		if row.Decider == "clef" && row.Source != "https://developers.cloudflare.com/workers-ai/platform/pricing/" {
			t.Fatalf("clef source = %q; want the Workers AI pricing page", row.Source)
		}
	}
}

// TestClefFlashHasPublishedPrice pins the independently dated Cloudflare row.
func TestClefFlashHasPublishedPrice(t *testing.T) {
	got, ok := DeciderCost("clef-flash", "clef-flash", 1_000_000, 0)
	if !ok || got < 0.0899 || got > 0.0901 {
		t.Fatalf("DeciderCost(clef-flash, 1M in) = %v, %v; want 0.090, true", got, ok)
	}
	for _, row := range DeciderPrices() {
		if row.Decider == "clef-flash" {
			if row.Model != "clef-flash" || row.InPerM != 0.090 || row.OutPerM != 0 || row.AsOf != "2026-10-02" || row.Source != "https://developers.cloudflare.com/workers-ai/platform/pricing/" {
				t.Fatalf("clef-flash row = %+v; want the verified Cloudflare pricing row", row)
			}
			return
		}
	}
	t.Fatal("DeciderPrices omitted clef-flash")
}

// TestVersionedJevModelStillPriced pins the approved aliases reported by Jev
// and Cloudflare; each alias must resolve without admitting arbitrary ids.
func TestVersionedJevModelStillPriced(t *testing.T) {
	got, ok := DeciderCost("jev", "jev-1.13.0", 1_000_000, 0)
	if !ok || got < 0.0419 || got > 0.0421 {
		t.Fatalf("DeciderCost(jev, versioned model) = %v, %v; want the jev-latest row, true", got, ok)
	}
	if _, ok := DeciderCost("clef", "@cf/cloudflare/clef", 1_000, 0); !ok {
		t.Fatal("the @cf-prefixed model id fell off the price table")
	}
}

// TestUnknownDeciderModelsAreUnpriced prevents the family fallback from
// fabricating costs for arbitrary or cross-decider model ids.
func TestUnknownDeciderModelsAreUnpriced(t *testing.T) {
	cases := []struct {
		decider string
		model   string
	}{
		{decider: "jev", model: "jev-evil"},
		{decider: "jev", model: "totally-unrelated"},
		{decider: "jev", model: "jev-1"},
		{decider: "jev", model: "jev-1.13.0.1"},
		{decider: "jev", model: "jev-1.13beta"},
		{decider: "clef", model: "clef-flash"},
		{decider: "clef-flash", model: "clef"},
		{decider: "clef", model: "@cf/cloudflare/clef-flash"},
	}
	for _, tc := range cases {
		if got, ok := DeciderCost(tc.decider, tc.model, 1_000_000, 0); ok {
			t.Errorf("DeciderCost(%q, %q) = %v, true; want unpriced", tc.decider, tc.model, got)
		}
	}
	for _, model := range []string{"jev-latest", "jev-1.13", "jev-1.13.0"} {
		if _, ok := DeciderCost("jev", model, 1_000_000, 0); !ok {
			t.Errorf("validated Jev model %q was not priced", model)
		}
	}
	for _, tc := range []struct{ decider, model string }{
		{"clef", "@cf/cloudflare/clef"},
		{"clef-flash", "@cf/cloudflare/clef-flash"},
	} {
		if _, ok := DeciderCost(tc.decider, tc.model, 1_000_000, 0); !ok {
			t.Errorf("vendor alias %q for %q was not priced", tc.model, tc.decider)
		}
	}
}

// TestUnknownDeciderIsUnpriced: a decider with no row is "unpriced" (ok=false),
// never an error — the advisory, nil-safe contract every pricing caller is
// built against (ADR-009).
func TestUnknownDeciderIsUnpriced(t *testing.T) {
	if _, ok := DeciderCost("gpt-10", "gpt-10", 1_000_000, 0); ok {
		t.Fatal("a chat model must not find a decider price")
	}
}

// TestDeciderPriceJSONUsesSourceURL pins the public listing schema; consumers
// must not have to translate the internal field name into the documented key.
func TestDeciderPriceJSONUsesSourceURL(t *testing.T) {
	b, err := json.Marshal(DeciderPrices())
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, `"source_url":`) {
		t.Fatalf("marshaled rows omit source_url: %s", got)
	}
	if strings.Contains(got, `"source":`) {
		t.Fatalf("marshaled rows expose obsolete source key: %s", got)
	}
}

// TestDeciderPriceRowsCarryProvenance: nothing re-verifies this table
// (`models --check` covers OpenRouter drift only), so a row without an as_of
// date and a source URL is an unverifiable number. Also pins that DeciderPrices
// hands out a copy — a caller mutating the listing must not corrupt pricing.
func TestDeciderPriceRowsCarryProvenance(t *testing.T) {
	rows := DeciderPrices()
	if len(rows) < 2 {
		t.Fatalf("DeciderPrices returned %d rows, want at least jev and clef", len(rows))
	}
	for _, r := range rows {
		if r.Decider == "" || r.Model == "" || r.AsOf == "" || r.Source == "" {
			t.Errorf("row %+v lacks provenance (as_of + source are mandatory)", r)
		}
		if r.InPerM <= 0 {
			t.Errorf("row %+v has no input price; an unknown price belongs in a comment, not a zero row", r)
		}
	}
	rows[0].InPerM = 9999
	if again := DeciderPrices(); again[0].InPerM == 9999 {
		t.Fatal("DeciderPrices leaked the package-level table to callers")
	}
}
