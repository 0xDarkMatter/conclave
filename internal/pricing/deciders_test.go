package pricing

import "testing"

// TestDeciderPricedFromTableNotCatalog: decision models are priced from the
// hand-maintained table because the OpenRouter catalog does not list them
// (ADR-016). If this ever passes through CostOf, deciders silently price as
// nothing — the exact failure the table exists to prevent.
func TestDeciderPricedFromTableNotCatalog(t *testing.T) {
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
	// Clef: $0.24 per million input tokens; the output price is unpublished on
	// the model page, so the row carries 0 — output tokens must add nothing.
	got, ok = DeciderCost("clef", "clef", 1_000_000, 0)
	if !ok || got < 0.2399 || got > 0.2401 {
		t.Fatalf("DeciderCost(clef, 1M in) = %v, %v; want 0.24, true", got, ok)
	}
	if got, ok := DeciderCost("clef", "clef", 0, 1_000_000); !ok || got != 0 {
		t.Fatalf("DeciderCost(clef, output-only) = %v, %v; want 0, true — output must be free, not unpriced", got, ok)
	}
}

// TestVersionedJevModelStillPriced: vendors report version-specific model ids
// in their responses ("jev-1.13.0"; Cloudflare spells clef
// "@cf/cloudflare/clef"). Those ids have no table row of their own and must
// fall through to the decider's default row, or every real response would
// price as nothing.
func TestVersionedJevModelStillPriced(t *testing.T) {
	got, ok := DeciderCost("jev", "jev-1.13.0", 1_000_000, 0)
	if !ok || got < 0.0419 || got > 0.0421 {
		t.Fatalf("DeciderCost(jev, versioned model) = %v, %v; want the jev-latest row, true", got, ok)
	}
	if _, ok := DeciderCost("clef", "@cf/cloudflare/clef", 1_000, 0); !ok {
		t.Fatal("the @cf-prefixed model id fell off the price table")
	}
}

// TestUnknownDeciderIsUnpriced: a decider with no row is "unpriced" (ok=false),
// never an error — the advisory, nil-safe contract every pricing caller is
// built against (ADR-009). clef-flash is deliberately absent until Phase 0
// probe 5 prices it; a zero row would have lied that it was free.
func TestUnknownDeciderIsUnpriced(t *testing.T) {
	if _, ok := DeciderCost("clef-flash", "clef-flash", 1_000_000, 0); ok {
		t.Fatal("clef-flash must stay unpriced until Phase 0 probe 5 fills its row")
	}
	if _, ok := DeciderCost("gpt-10", "gpt-10", 1_000_000, 0); ok {
		t.Fatal("a chat model must not find a decider price")
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
