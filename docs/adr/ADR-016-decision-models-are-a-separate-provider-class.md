---
status: accepted
date: 2026-10-02
supersedes: []
superseded-by: []
extends: [ADR-002, ADR-004]
related: [ADR-001, ADR-008, ADR-009, ADR-010, ADR-011, ADR-012]
touches:
  - "internal/providers/decider.go"
  - "internal/providers/decide_*.go"
  - "internal/providers/registry.go:AllDeciders"
  - "internal/decide/**"
  - "internal/pricing/deciders.go"
  - "internal/providers/decide_jev.go"
  - "cmd/decide.go"
---

# ADR-016: Decision models are a separate provider class

## Decision (one sentence)

System One decision models (Typesafe's Jev, Cloudflare's Clef and Clef-flash) are a separate class: they implement a `Decider` interface rather than `Provider`, take typed questions and return typed answers with probabilities. They resolve through their own `AllDeciders()` list, never `AllAPIProviders()`, and run only from `conclave decide` and the entry points built on it. Their panel combines answers by equal-weight probability averaging rather than an LLM judge, and they are priced from a dated table in `internal/pricing/deciders.go` because the OpenRouter catalog does not list them.

## Context

Conclave's `Provider` contract is `Query(prompt) -> string`: free text in, free text out, with agreement found afterwards by an LLM judge (ADR-001). Decision models invert that. A request is a `state` plus up to 64 named `questions`, each of type `noul` (a 0-1 value), `choice` (one of a criteria map) or `score` (an ordinal over a criteria list). The response is an `answers` map with the chosen value, a `confidence` and a full `probabilities` distribution per question. Each question is evaluated in parallel and on its own. Medians are 39-209 ms, and prices are input-only: Jev $0.042/M, Clef $0.24/M (2026-10).

Both vendors speak one wire format. Clef is published as "fully Jev-API compatible": Jev at `POST https://api.typesafe.ai/v1/systemone`, Clef at Workers AI `POST /client/v4/accounts/{account}/ai/run/@cf/cloudflare/clef`. Clef's weights are Apache-2.0 on Hugging Face, so a self-hosted endpoint speaking the same format is plausible.

Treated as a `Provider`, a decision model fails in four ways:

1. **`--all` sends it a free-text prompt it cannot answer.** This is the same reason ADR-010 kept a plain `openrouter` provider out of `AllAPIProviders`.
2. **Its typed answer gets flattened into `Response.Response`.** Every consumer would then re-parse the string.
3. **The cache key misses the questions.** ADR-011's key is built from the prompt, so two different question sets over the same state would collide.
4. **Pricing silently drops it.** The ADR-009 catalog has no entry, so it prices as nothing.

Meanwhile the thing Conclave exists to do, comparing several models and measuring their agreement, is better defined for decision models than for chat models, because their outputs are directly comparable distributions.

## Alternatives considered

- **Implement `Provider` and JSON-encode the answers into the response string.** Rejected: hits the four failures above, and every downstream reader (judge, output, batch) would need a "this string is really a decision" branch.
- **Wrap the question set into an ordinary LLM prompt and let chat models answer too, all through one interface.** Rejected: chat models return no calibrated probabilities, so the mixed panel's aggregate would average real distributions with fabricated ones. Chat models stay on the existing path; the rubric work (plan Feature 5) is where the two classes meet, behind an explicit scorer choice.
- **Aggregate a decision panel with an LLM judge.** Rejected: it spends a slow, expensive, uncalibrated call to summarise fast, cheap, calibrated ones. Probability averaging is arithmetic and is reproducible.
- **Confidence-weighted aggregation.** Rejected for now: each vendor's calibration is its own claim and is not comparable across vendors. Equal weight is explainable, and weighting can be added later as a flag if a calibration eval justifies it.
- **Price through the OpenRouter catalog, or extend batch's `fallbackCosts`.** Rejected: OpenRouter does not list these models, and AGENTS.md Gotcha 6 forbids growing `fallbackCosts`. A small table with each price cited and dated is honest about being manual.

## Consequences

### Positive
- Answers keep their types end to end: `--json` carries the vendor's answer object per decider, plus a consensus block per question.
- `--all`, the judge, the progress line and the pricing catalog are untouched. No chat-model code path learns about decisions.
- One shared System One client covers every backend. Adding a vendor with the same format is a registration, not a client.
- The shared client sits on ADR-004's retry and backoff, so 429/5xx handling and the `BillingError` short-circuit come for free. Keys come from ADR-008's `NewKeyRotator`, so `.env` and the OS keyring work.

### Negative
- Two registries to keep straight. A decider name used as a `Provider` token (`conclave clef "..."`) must fail with a pointer to `conclave decide`, not "unknown provider".
- The price table is hand-maintained and can go stale silently. It carries an `as_of` date per row, and `conclave models --check` does not cover it.
- The cache key for a decision is `(api, decider, model, state, canonical-JSON(questions + image hashes))`, reusing `cache.Key`'s `system` slot for the question set. This trick must be documented at the construction site.
- Context limits differ per backend (Jev 32k, Clef 64k). Oversized state is sent and the vendor's 4xx is surfaced, never truncated locally.

### Non-goals
- Not a replacement for the LLM judge: decision models cannot write reasoning, agreements or recommendations. A decision-model verdict over an LLM panel is a later, additive option.
- No CLI transport: deciders are API-only, so `clef@cli` is an error. They take no transport suffix at all.
- No fine-tuning, RL or model-management features from either vendor.
- No image support in the first cut. Clef accepts up to 4 images, and the request type leaves room for them.

## See also

- `docs/PLAN-decision-models.md` — phased build plan, wire contract, `--json` shape, tests.
- [ADR-001](ADR-001-llm-as-judge-verdict-synthesis.md) — the LLM judge this deliberately does not use.
- [ADR-004](ADR-004-shared-openai-compatible-http-client-with-retry-backoff.md) — the retry/backoff client the System One client rides on.
- [ADR-009](ADR-009-runtime-pricing-catalog-from-openrouter.md) — the catalog these models are absent from.
- [ADR-010](ADR-010-openrouter-as-a-slash-routed-api-backend.md) — precedent for keeping a model class out of `AllAPIProviders`.
- [ADR-011](ADR-011-opt-in-response-cache-keyed-on-the-full-prompt.md) — the cache key this extends.
- Typesafe quickstart: https://docs.typesafe.ai/introduction/quickstart
- Clef announcement: https://blog.cloudflare.com/clef-decision-models/ and model page https://developers.cloudflare.com/workers-ai/models/clef

## Addendum — 2026-10-03: Jev has a second route, through OpenRouter

Jev is now also served by OpenRouter's Decisions API (`POST https://openrouter.ai/api/alpha/decisions`,
`OPENROUTER_API_KEY`), at the same $0.042/M and with the same bare System One response shape plus
`usage.cost`, `id` and `provider`. The `jev` decider keeps one name and one price row but picks its
route by credential: Typesafe direct when `TYPESAFE_API_KEY` resolves, otherwise OpenRouter. This
refines, not reverses, the decision above: jev is still a `Decider`, still outside `AllAPIProviders`,
still priced from the table (whose jev alias rule now accepts OpenRouter's `typesafe/jev-X.Y-YYYYMMDD`
id). The same probe found five more vendors' decision models on OpenRouter; whether `conclave decide`
should route arbitrary `vendor/model` decider tokens there is a separate decision, left to ADR-017.
Evidence: `docs/PLAN-decision-models.md`, Phase 0 findings.

## Addendum - 2026-10-05: Workers AI rows carry their Neuron rate and free allocation

The Clef prices above were re-verified on 2026-10-05 and stand: $0.240 and $0.090 per million input
tokens, still with no output price. Workers AI bills Neurons ($0.011 per 1,000) and shows the
per-token figure as an equivalent, so the table rows gained two optional columns, `neurons_per_m_in`
(21818 and 8182) and `free_neurons_per_day` (10,000, account-wide, reset 00:00 UTC). The dollar price
must stay the Neuron rate at $0.011 per 1,000, and a test pins that. The free allocation is recorded
and shown but never subtracted from a cost; that decision, and why, is
[ADR-019](ADR-019-free-daily-allocations-are-recorded-never-subtracted.md). A spent allocation
(HTTP 429, Cloudflare code 4006) is now a `BillingError`.
