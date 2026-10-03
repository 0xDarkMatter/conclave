---
status: accepted
date: 2026-10-03
supersedes: []
superseded-by: []
extends: [ADR-009, ADR-016]
related: [ADR-010, ADR-017]
touches:
  - "internal/frontier/**"
  - "internal/benchmarks/**"
  - "cmd/models.go:--frontier"
---

# ADR-018: Price-performance frontiers from external, edition-pinned quality sources

## Decision (one sentence)

`conclave models --frontier` draws price-performance Pareto frontiers only from external quality sources, never from evaluations Conclave runs itself. The sources are: OpenRouter's benchmarks feed (Artificial Analysis indices and OpenRouter's own evals) for chat models; and for decision models, the community Decision Index recomputed from its upstream Space with its published methodology, pinned to one edition by file name and cross-checked against Cloudflare's mirror. A model with no external score is shown as unscored, never estimated.

## Context

Comparing models on price and quality needs a quality axis. Running our own evals was considered and rejected by the maintainer (2026-10-03): it is a separate product with its own upkeep. External data covers most of the need. OpenRouter's `/api/v1/benchmarks` (keyed) serves Artificial Analysis intelligence, coding and agentic indices plus OpenRouter's own evals, which include `avg_cost_per_task`. That feed lists 277 chat models and no decision models.

For decision models the only quality source is the community Decision Index (Hugging Face Space `multimodalart/jev-decision-index`). Its upstream publishes per-benchmark scores, calibration and latency as static JSON per edition, plus a machine-readable methodology: per-benchmark chance levels, gold weights, area weights, formulas and a worked example. It does not publish the headline index. Cloudflare's mirror (`clef-evals.workers-ai-mle.workers.dev/data/leaderboard.json`) publishes a precomputed index and adds self-reported Clef rows, but it is a vendor's demo built from one snapshot.

The methodology moves fast: v0.2 on 2026-09-27, v0.2.1 on 2026-09-28, and a "core25" panel already proposed. The Space carries no licence.

## Alternatives considered

- **Run our own evals inside Conclave.** Rejected by the maintainer: a different product.
- **Read the mirror's precomputed index only.** Rejected as the primary source: a vendor ranking its own models, a one-off build with no update cadence, and fewer fields. It is kept as a cross-check and as the source of the Clef rows, marked self-reported.
- **Recompute from upstream only.** Rejected as insufficient on its own: upstream publishes no headline index, so a silent formula drift would have nothing to be checked against.
- **Follow the moving `index.json`.** Rejected: an upstream edition change would silently change Conclave's numbers. Edition moves are deliberate.
- **Copy the Decision Index data into the repo.** Rejected: the Space has no licence. Conclave fetches at run time, caches like the pricing catalog, and cites.

## Consequences

### Positive
- Every plotted score has a named external source and a snapshot date, and the no-eval boundary is structural: no frontier code runs a model.
- Decision scores come from the original data, and a disagreement with the mirror becomes a visible warning, not a silent drift.
- The published worked example is a fixed test of the formula.

### Negative
- Conclave owns about 300 lines of formula code per edition, and a methodology change needs a deliberate edition bump (file name, worked example, re-check).
- Decision Index names ("Kev 4B") need a hand-maintained map to OpenRouter slugs and Conclave decider names. An unmapped model is shown under its board name but cannot be priced.
- Decision-model cost is list price per million input tokens. Per-call token counts differ by up to 13x between vendors, and no external source publishes per-call cost for them. Chat models use OpenRouter's `avg_cost_per_task` where it exists.
- Every source is advisory: unreachable or unparseable data leaves models unscored with a warning, never an error (ADR-009's rule).

### Non-goals
- No evaluation harness, gold datasets or `--eval` flag, now or later, without a new ADR that supersedes this one.
- No auto-picking of panel members from the frontier.

## See also

- `docs/PLAN-decision-models.md` - "Frontier view" section: build plan and source details.
- [ADR-009](ADR-009-runtime-pricing-catalog-from-openrouter.md) - advisory, cached, nil-safe external data.
- [ADR-016](ADR-016-decision-models-are-a-separate-provider-class.md), [ADR-017](ADR-017-slash-routed-decision-models-via-openrouter.md) - the decision models being plotted.
