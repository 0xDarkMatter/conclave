package output

// Dollar figures for a single render pass.
//
// Contract:
//   - Costs are ONLY ever shown in API mode (-g / -c). CLI-mode providers ride
//     subscriptions, so a per-token price there would be a lie. See the preamble
//     of docs/MODEL_REGISTRY.md and ADR-009.
//   - The pricing catalog is advisory and may be nil (offline, CONCLAVE_NO_PRICING=1).
//     A price we cannot look up is OMITTED, never rendered as $0.00 — a printed
//     zero would read as "this was free", which is the opposite of "unknown".
//   - A cache hit costs nothing and is a KNOWN zero, so it renders as $0.0000.

import (
	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// costs holds the priced result of one render. A nil pointer means "unknown"
// and must render as nothing at all.
type costs struct {
	// byIndex is parallel to Result.Responses. nil entry = unpriceable.
	byIndex []*float64
	// judge is the synthesis cost, nil when there is no verdict or no price.
	judge *float64
	// total sums every KNOWN figure; nil when nothing at all could be priced.
	total *float64
	// partial is true when at least one response could not be priced, so the
	// total understates the real spend and must be shown as an "at least".
	// It is tracked independently of cache hits: a hit is a KNOWN zero and
	// must not make a total look complete when something else was unpriceable.
	partial bool
}

// enabled reports whether anything cost-related should be rendered.
func (c costs) enabled() bool { return c.total != nil }

// computeCosts prices a result. Returns the zero value (renders nothing) in
// CLI mode or when the catalog knows none of the models involved.
func computeCosts(cat *pricing.Catalog, r Result, apiMode bool) costs {
	var c costs
	if !apiMode {
		return c
	}

	var sum float64
	var known bool

	c.byIndex = make([]*float64, len(r.Responses))
	for i, resp := range r.Responses {
		if resp.Status != "success" {
			continue // a failed call is not billed for output we never got
		}
		v, ok := responseCost(cat, resp)
		if !ok {
			c.partial = true
			continue
		}
		c.byIndex[i] = &v
		sum += v
		known = true
	}

	if r.Verdict != nil && r.Verdict.JudgeTokens > 0 {
		if v, ok := cat.JudgeCostOf(r.Verdict.JudgeProvider, r.Verdict.JudgeModel, r.Verdict.JudgeTokens); ok {
			c.judge = &v
			sum += v
			known = true
		} else {
			c.partial = true
		}
	}

	if known {
		c.total = &sum
	}
	return c
}

// responseCost prices one provider response. ok=false means "no idea".
func responseCost(cat *pricing.Catalog, resp providers.Response) (float64, bool) {
	if resp.Cached {
		return 0, true // served from conclave's response store; nobody was billed
	}
	if resp.Metrics == nil {
		return 0, false // provider reported no token usage (most CLI wrappers)
	}
	return cat.CostOf(resp.Provider, resp.Model, resp.Metrics.InputTokens, resp.Metrics.OutputTokens)
}

// formatTotal renders the header/footer total, marking an understated sum.
func (c costs) formatTotal() string {
	if c.total == nil {
		return ""
	}
	s := pricing.FormatUSD(*c.total)
	if c.partial {
		s += "+" // at least this much; some models were not in the catalog
	}
	return s
}
