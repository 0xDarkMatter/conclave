// Decision-model prices stay outside the OpenRouter catalog under ADR-016.
// This dated table alone prices deciders; unknown model ids remain unpriced.
// GUARD: models --check cannot detect drift here, so update AsOf and Source
// together (and a Workers AI row's InPerM with its NeuronsPerMIn), and never
// route these rows through batch fallbackCosts.
// Costs are advisory and nil-safe under ADR-009: absence means ok=false. A
// free daily allocation is recorded, never subtracted (ADR-019).
package pricing

import (
	"regexp"
	"strconv"
)

// DeciderPrice is one row of the hand-maintained decision-model price table.
// AsOf is the date the price was verified and Source the page it came from;
// because nothing re-checks the row, the citation IS its provenance.
//
// The two Neuron fields are Workers AI's own billing facts (zero elsewhere).
// Workers AI bills Neurons and shows a per-token price beside each model as
// an equivalent, so InPerM must stay NeuronsPerMIn at $0.011 per 1,000
// Neurons (pinned by a test). FreeNeuronsPerDay is a FACT about the vendor,
// never a discount: the allocation is account-wide, shared with every other
// Workers AI caller on the account, and Conclave cannot see how much of it is
// left, so DeciderCost always prices at InPerM (ADR-019).
type DeciderPrice struct {
	Decider           string  `json:"decider"`
	Model             string  `json:"model"`
	InPerM            float64 `json:"in_per_m"`
	OutPerM           float64 `json:"out_per_m"`
	NeuronsPerMIn     float64 `json:"neurons_per_m_in,omitempty"`
	FreeNeuronsPerDay int     `json:"free_neurons_per_day,omitempty"`
	AsOf              string  `json:"as_of"`
	Source            string  `json:"source_url"`
}

// FreeInputTokensPerDay is the daily free allocation in this model's input
// tokens, if nothing else on the account spends any of it: an upper bound,
// derived from two published numbers, not an estimate of what is left. 0 when
// the row has no free allocation.
func (p DeciderPrice) FreeInputTokensPerDay() int {
	if p.FreeNeuronsPerDay <= 0 || p.NeuronsPerMIn <= 0 {
		return 0
	}
	return int(float64(p.FreeNeuronsPerDay) * 1_000_000 / p.NeuronsPerMIn)
}

// FreeDaily names the free allocation in the vendor's own unit, for listings
// and the frontier legend; "" when the row has none.
func (p DeciderPrice) FreeDaily() string {
	if p.FreeNeuronsPerDay <= 0 {
		return ""
	}
	return groupThousands(p.FreeNeuronsPerDay) + " Neurons/day per Cloudflare account"
}

// groupThousands writes 10000 as "10,000", the way the pricing page does.
func groupThousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// deciderPrices is the table. Prices are USD per million tokens, input-only
// for every listed row: decision models bill the state+questions tokens and
// emit a few dozen answer tokens.
//
// Clef rows' OutPerM is 0 because Cloudflare publishes NO output price (still
// none on 2026-10-05; Clef reports 0 output tokens anyway) — 0 here means "no
// known output charge", not "verified free"; revisit with AsOf if Workers AI
// pricing gains one. jev's 0 is verified: Typesafe states output is FREE.
//
// Clef rows, read 2026-10-05 from the "Other model pricing" table (page
// updated 2026-10-01): Workers AI bills $0.011 per 1,000 Neurons, clef at
// 21818 Neurons per M input tokens ($0.240) and clef-flash at 8182 ($0.090).
// Every account gets 10,000 Neurons/day free on both Workers plans, reset
// 00:00 UTC; past it, Workers Free fails (429 code 4006, a BillingError) and
// Workers Paid bills the excess at the rates above.
var deciderPrices = []DeciderPrice{
	{
		Decider: "jev", Model: "jev-latest",
		InPerM: 0.042, OutPerM: 0,
		AsOf:   "2026-10-02",
		Source: "https://typesafe.ai/blog/introducing-system-one-models-and-jev",
	},
	{
		Decider: "clef", Model: "clef",
		InPerM: 0.240, OutPerM: 0,
		NeuronsPerMIn: 21818, FreeNeuronsPerDay: 10_000,
		AsOf:   "2026-10-05",
		Source: "https://developers.cloudflare.com/workers-ai/platform/pricing/#other-model-pricing",
	},
	{
		Decider: "clef-flash", Model: "clef-flash",
		InPerM: 0.090, OutPerM: 0,
		NeuronsPerMIn: 8182, FreeNeuronsPerDay: 10_000,
		AsOf:   "2026-10-05",
		Source: "https://developers.cloudflare.com/workers-ai/platform/pricing/#other-model-pricing",
	},
}

// jevVersionID matches the ids Jev reports or accepts for a numbered release:
// Typesafe's "jev-1.13" and OpenRouter's "typesafe/jev-1.13-20260917" (dated
// canonical slug, probed 2026-10-03). Same model, same price on both routes.
var jevVersionID = regexp.MustCompile(`^(typesafe/)?jev-\d+\.\d+(\.\d+)?(-\d{8})?$`)

// DeciderCost prices one decision call from the table. Matching, in order:
//
//  1. the exact (decider, model) row;
//  2. an explicit vendor alias: @cf/cloudflare/<decider> for either Clef row,
//     or jev-latest / ~typesafe/jev-latest / a numeric jev-X.Y[.Z] version
//     (optionally typesafe/-prefixed and -YYYYMMDD-dated) for Jev.
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
		alias = model == "jev-latest" || model == "~typesafe/jev-latest" || jevVersionID.MatchString(model)
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
