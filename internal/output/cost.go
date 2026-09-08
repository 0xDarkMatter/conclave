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
	"fmt"

	"github.com/0xDarkMatter/conclave-cli/internal/pricing"
	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

// judgeInputShare splits the judge's single token count into input/output for
// pricing. The judge Provider interface reports one total (judge.Verdict has
// JudgeTokens, not a split), and a synthesis prompt is dominated by the pasted
// provider answers, so the bias is heavily toward input. The same 70/30
// heuristic is used by internal/batch/processor.go estimateCost; keep them
// together if either changes.
const judgeInputShare = 0.7

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
		if in, out, ok := cat.Price(r.Verdict.JudgeProvider, r.Verdict.JudgeModel); ok {
			tok := float64(r.Verdict.JudgeTokens)
			v := tok*judgeInputShare*in/1_000_000 + tok*(1-judgeInputShare)*out/1_000_000
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
	in, out, ok := cat.Price(resp.Provider, resp.Model)
	if !ok {
		return 0, false
	}
	return float64(resp.Metrics.InputTokens)*in/1_000_000 + float64(resp.Metrics.OutputTokens)*out/1_000_000, true
}

// formatUSD renders a known cost. Never called for an unknown one.
func formatUSD(v float64) string {
	switch {
	case v <= 0:
		return "$0.0000"
	case v < 0.0001:
		return "<$0.0001" // real but sub-tenth-of-a-cent; a rounded $0.0000 would read as free
	default:
		return fmt.Sprintf("$%.4f", v)
	}
}

// formatTotal renders the header/footer total, marking an understated sum.
func (c costs) formatTotal() string {
	if c.total == nil {
		return ""
	}
	s := formatUSD(*c.total)
	if c.partial {
		s += "+" // at least this much; some models were not in the catalog
	}
	return s
}
