// Decision-model prices (Jev, Clef, Clef-flash), kept apart from the
// OpenRouter catalog on purpose (ADR-016): the catalog does not list these
// models, so pricing them through CostOf would report "unpriced" forever.
// This table is the authority for decider costs; nothing else prices them.
//
// GUARD — this table is hand-maintained and can go stale silently:
// `conclave models --check` gates OpenRouter drift only and does NOT cover
// these rows. Every row carries AsOf + Source so a stale price is at least
// visible; re-checking a price means updating those two fields together.
//
// Advisory and nil-safe like everything in this package (ADR-009): a decider
// missing from the table is "unpriced" (ok=false), never an error. And per
// AGENTS.md Gotcha 6, do NOT extend batch's fallbackCosts instead of adding a
// row here — that table exists only for catalog-outage fallbacks.
package pricing

// DeciderPrice is one row of the hand-maintained decision-model price table.
// AsOf is the date the price was verified and Source the page it came from;
// because nothing re-checks the row, the citation IS its provenance.
type DeciderPrice struct {
	Decider string  `json:"decider"`
	Model   string  `json:"model"`
	InPerM  float64 `json:"in_per_m"`
	OutPerM float64 `json:"out_per_m"`
	AsOf    string  `json:"as_of"`
	Source  string  `json:"source"`
}

// deciderPrices is the table. Prices are USD per million tokens, input-only
// for every listed row: decision models bill the state+questions tokens and
// emit a few dozen answer tokens.
//
// clef's OutPerM is 0 because Cloudflare had published NO output price as of
// 2026-10-02 (model page verified) — 0 here means "no known output charge",
// not "verified free"; revisit with AsOf if Workers AI pricing gains one.
// jev's 0 is verified: Typesafe states output is FREE.
//
// clef-flash has NO row on purpose: its price was unpublished at 2026-10-02.
// Phase 0 probe 5 (docs/PLAN-decision-models.md) fills it; until then
// DeciderCost reports it unpriced, which ADR-009 defines as the honest state.
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
		Source: "https://developers.cloudflare.com/workers-ai/models/clef",
	},
}

// DeciderCost prices one decision call from the table. Matching, in order:
//
//  1. the exact (decider, model) row;
//  2. otherwise the decider's DEFAULT row — the row whose Model is
//     "<decider>-latest" or the decider's own name. Vendors report
//     version-specific model ids in responses ("jev-1.13.0"), and Cloudflare
//     spells clef "@cf/cloudflare/clef"; none of those have their own row,
//     and they must still price, because a decider has one price per model
//     family in this table.
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

// deciderRow finds the pricing row for (decider, model) under the matching
// rule documented on DeciderCost. Exact match first so a future second model
// row for one decider (a cheaper tier, say) is honoured when asked for by id.
func deciderRow(decider, model string) *DeciderPrice {
	for i := range deciderPrices {
		if r := &deciderPrices[i]; r.Decider == decider && r.Model == model {
			return r
		}
	}
	for i := range deciderPrices {
		r := &deciderPrices[i]
		if r.Decider == decider && (r.Model == decider+"-latest" || r.Model == decider) {
			return r
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
