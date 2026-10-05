---
status: accepted
date: 2026-10-05
supersedes: []
superseded-by: []
extends: [ADR-016, ADR-018]
related: [ADR-004, ADR-009, ADR-015]
touches:
  - "internal/pricing/deciders.go"
  - "internal/frontier/**"
  - "cmd/models.go:printDeciderPrices"
  - "internal/providers/api_base.go:billingCode"
---

# ADR-019: Free daily allocations are recorded, never subtracted

## Decision (one sentence)

A vendor's free daily allocation (Workers AI: 10,000 Neurons per account per UTC day) is recorded on the decider price row as a fact (its size and the model's Neuron rate) and shown beside the price, but never subtracted from it: `cost_usd`, `meta.total_cost_usd` and the frontier always use the metered list price, and a spent allocation is a `BillingError`.

## Context

Workers AI bills Neurons, at $0.011 per 1,000, and shows each model's rate as a per-token equivalent ([pricing page](https://developers.cloudflare.com/workers-ai/platform/pricing/#other-model-pricing), updated 2026-10-01, read 2026-10-05). Clef costs 21,818 Neurons per million input tokens ($0.240) and Clef-flash 8,182 ($0.090). Neither has an output price, and Clef reports 0 output tokens. The dollar figures in ADR-016's table were right; the Neuron rate behind them was not recorded.

Every account, on Workers Free or Workers Paid, gets 10,000 Neurons a day at no charge, reset at 00:00 UTC. Past that, Workers Free fails every call until the reset, and Workers Paid bills the excess at the metered rate. If nothing else on the account uses Workers AI, the allocation is 458,337 Clef input tokens or 1,222,195 Clef-flash input tokens a day. A typical decide call (Phase 0 measured 142 to 392 input tokens) uses about 9 Neurons on Clef and 3 on Clef-flash, so roughly 1,100 Clef calls or 3,000 Clef-flash calls a day cost nothing.

So for a light user, a Clef call priced at $0.000094 was really free, and nothing in Conclave said so. What Conclave cannot know is how much of the allocation is left. The allocation is account-wide and shared with every other Workers AI workload on that account (Workers, other tools, other people). Conclave has no reliable way to read the account's plan with the Workers AI token it holds. A call can also straddle the boundary.

The spent-allocation error was also unrecorded. The Phase 0 probes could not trigger it cheaply, and the code it was suspected to carry (3036) was from memory. Cloudflare's [errors page](https://developers.cloudflare.com/workers-ai/platform/errors/) (updated 2026-09-17) does list 3036 ("Account limited", HTTP 429). But every capture on the REST API found in public projects shows code 4006 with the same message, for example `{"errors":[{"message":"AiError: AiError: you have used up your daily free allocation of 10,000 neurons, please upgrade to Cloudflare's Workers Paid plan if you would like to continue usage. (<uuid>)","code":4006}],"success":false,"result":{},"messages":[]}`. Captures came from run logs and test fixtures dated 2026-07 to 2026-10, for example github.com/oliver-trako/korcula-events/issues/4. No capture shows 3036. Code 3040 ("Out of capacity") is the same 429 in the same envelope and is transient.

## Alternatives considered

- **Price a Workers AI call at $0, or subtract the allocation.** Rejected. It is wrong on Workers Paid once the allocation is spent, and wrong whenever another workload shares the account. It would also be an estimate of the remaining allocation, which ADR-018's "external data only, never estimated" stance rules out. On the frontier it would put Clef at $0, below every paid model for any workload past a few hundred calls a day, and a log axis cannot plot $0 anyway.
- **Query the account's Neuron usage (GraphQL Analytics) and price each call at its true marginal cost.** Rejected for now. It needs an extra token permission (Account Analytics read) and an extra request per run. The analytics lag and concurrent use make the answer approximate, and the plan is still unknown. It could come back later as an opt-in `conclave usage` command, but it would not change `cost_usd`.
- **Add a second per-call number ("estimated billed") beside `cost_usd`.** Rejected: it has the same unknowns, and two cost numbers invite reading the wrong one.
- **Put it in the docs only.** Rejected. The price is read in `conclave models` and on the frontier, so the fact belongs next to the number. The table needed the Neuron rate as provenance anyway.
- **Use a free-text note field instead of typed fields.** Rejected. Typed `neurons_per_m_in` and `free_neurons_per_day` let the listing show the allocation in the model's own tokens. They also let a test pin the dollar price to the Neuron rate, so a hand edit to one cannot drift from the other.
- **Classify only the documented 3036.** Rejected: it would never have matched the wire. 3036 is kept beside 4006 in case Cloudflare brings the wire into line with its docs.

## Consequences

### Positive
- `cost_usd` has one meaning for every decider: the metered price, and so an upper bound on what the call adds to the bill. It is exact on Workers Paid once the allocation is spent, $0 in fact inside it, and never billed on Workers Free. Costs stay comparable across deciders and on the frontier.
- The allocation is visible where prices are read. `conclave models` shows a FREE/DAY column (458K input tokens for Clef, 1.2M for Clef-flash) with a note, and `--json` carries both new fields. Frontier points priced from the table carry `free_daily`, shown as a `+` mark and a legend line in the terminal and as a note and tooltip in the HTML report.
- A spent allocation fails on the first attempt instead of after ~7 s of backoff per decider per call. It is permanent for the run (ADR-015's rule), so it is not retried, and `--resume` after 00:00 UTC picks it up.

### Negative
- For a light user on Workers Free, every Clef `cost_usd` overstates the bill, which is $0. The listing and the frontier legend say so; `conclave decide`'s own output does not, to keep its envelope unchanged.
- `free_neurons_per_day` is Workers-AI-shaped. Another vendor's free tier, in another unit, would need its own field or a generalisation; decide that when one exists.
- The row is still hand-maintained (ADR-016). The Neuron rate test catches a half-edit, not a vendor price change.

### Non-goals
- No tracking of allocation use, no plan detection, no "free calls left today" estimate.
- No change for OpenRouter `:free` models. They have a listed price of $0, which is a genuine price, not an allocation.

## See also

- [ADR-016](ADR-016-decision-models-are-a-separate-provider-class.md) - the hand-maintained decider price table this extends.
- [ADR-018](ADR-018-frontier-from-external-edition-pinned-sources.md) - list price on the decision frontier; external data only.
- [ADR-004](ADR-004-shared-openai-compatible-http-client-with-retry-backoff.md), [ADR-015](ADR-015-batch-skips-permanent-failures-and-never-aborts-on-billing.md) - billing errors fail fast and are permanent for a run.
- `docs/PLAN-decision-models.md` - Phase 0 probe 2 findings.
