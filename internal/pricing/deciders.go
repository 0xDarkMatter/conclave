// Decision-model prices stay outside the OpenRouter catalog under ADR-016.
// This dated table alone prices deciders; unknown model ids remain unpriced.
// GUARD: models --check cannot detect drift here, so update AsOf and Source
// together, and never route these rows through batch fallbackCosts.
// Costs are advisory and nil-safe under ADR-009: absence means ok=false.
package pricing

import "regexp"

// DeciderPrice is one row of the hand-maintained decision-model price table.
// AsOf is the date the price was verified and Source the page it came from;
// because nothing re-checks the row, the citation IS its provenance.
type DeciderPrice struct {
	Decider string  `json:"decider"`
	Model   string  `json:"model"`
	InPerM  float64 `json:"in_per_m"`
	OutPerM float64 `json:"out_per_m"`
	AsOf    string  `json:"as_of"`
	Source  string  `json:"source_url"`
}

// deciderPrices is the table. Prices are USD per million tokens, input-only
// for every listed row: decision models bill the state+questions tokens and
// emit a few dozen answer tokens.
//
// Clef rows' OutPerM is 0 because Cloudflare had published NO output price as
// of 2026-10-02 — 0 here means "no known output charge",
// not "verified free"; revisit with AsOf if Workers AI pricing gains one.
// jev's 0 is verified: Typesafe states output is FREE.
var deciderPrices = []DeciderPrice{
	{
		Decider: "jev", Model: "jev-latest",
		InPerM: 0.042, OutPerM: 0,
		AsOf:   "2026-10-02",
		Source: "https://typesafe.ai/blog/introducing-system-one-models-and-jev",
	},
	{
		Decider: "clef", Model: "clef",
		InPerM: 0.24, OutPerM: 0,
		AsOf:   "2026-10-02",
		Source: "https://developers.cloudflare.com/workers-ai/platform/pricing/",
	},
	{
		Decider: "clef-flash", Model: "clef-flash",
		InPerM: 0.090, OutPerM: 0,
		AsOf:   "2026-10-02",
		Source: "https://developers.cloudflare.com/workers-ai/platform/pricing/",
	},
}

var jevVersionID = regexp.MustCompile(`^jev-\d+\.\d+(\.\d+)?$`)

// DeciderCost prices one decision call from the table. Matching, in order:
//
//  1. the exact (decider, model) row;
//  2. an explicit vendor alias: @cf/cloudflare/<decider> for either Clef row,
//     or jev-latest / a numeric jev-X.Y[.Z] version for Jev.
//
// Anything else is unpriced: ok=false, never an error (ADR-009). A cost of 0
// with ok=true means the table knows the price and it is genuinely zero
// (output tokens); callers must keep distinguishing that from ok=false.
func DeciderCost(decider, model string, inTokens, outTokens int) (cost float64, ok bool) {
	row := deciderRow(decider, model)
	if row == nil {
		return 0, false
	}
	return float64(inTokens)*row.InPerM/1_000_000 + float64(outTokens)*row.OutPerM/1_000_000, true
}

// deciderRow keeps alias acceptance deliberately enumerated. A prefix or
// decider-only fallback would fabricate a cost for future, differently priced
// models and can even cross-price clef-flash as clef.
func deciderRow(decider, model string) *DeciderPrice {
	for i := range deciderPrices {
		if r := &deciderPrices[i]; r.Decider == decider && r.Model == model {
			return r
		}
	}
	alias := false
	switch decider {
	case "jev":
		alias = model == "jev-latest" || jevVersionID.MatchString(model)
	case "clef", "clef-flash":
		alias = model == "@cf/cloudflare/"+decider
	}
	if alias {
		for i := range deciderPrices {
			if r := &deciderPrices[i]; r.Decider == decider {
				return r
			}
		}
	}
	return nil
}

// DeciderPrices returns the table rows for `conclave models` to list with
// their as_of dates (ADR-016). A copy: callers must not be able to mutate the
// package-level table through the listing.
func DeciderPrices() []DeciderPrice {
	out := make([]DeciderPrice, len(deciderPrices))
	copy(out, deciderPrices)
	return out
}
