---
status: accepted
date: 2026-10-03
supersedes: []
superseded-by: []
extends: [ADR-016, ADR-010]
related: [ADR-008, ADR-009, ADR-011]
touches:
  - "internal/providers/decide_openrouter.go"
  - "internal/providers/registry.go:GetDecider"
  - "internal/providers/decider.go:Decision.ReportedCostUSD"
  - "cmd/decide.go"
---

# ADR-017: Slash-routed decision models via OpenRouter

## Decision (one sentence)

In `conclave decide`, any `vendor/model` token (`liquid/d1`, `inception/mercury-decide:free`) is a decision model served by OpenRouter's Decisions API (`POST https://openrouter.ai/api/alpha/decisions`, `OPENROUTER_API_KEY`), built on demand with the token as both decider name and model id, never listed in `AllDeciders()`, and priced from the `usage.cost` OpenRouter reports rather than the hand-maintained table.

## Context

ADR-016 made decision models a separate class with three named deciders (jev, clef, clef-flash), each with its own price row. Its 2026-10-03 addendum found that OpenRouter now serves Jev through a Decisions API that speaks the same System One wire format. A probe the same day found decision models from six vendors there under `output_modalities=decisions`: TypeSafe, Liquid AI, Upstage, Inception, Together and Jared Palmer, plus Respan's noul-only behaviour scorer. All of them return the bare System One response plus `usage.cost`, `id` and `provider`. Only some appear on the community Decision Index (Jev 57.9, Kev 4B 34.6, Tev1 4B 29.2); D1, Solar Decide and Mercury Decide do not. Measuring them is exactly what a decision panel is for.

Naming each one as a fixed decider would repeat the problem ADR-010 solved for chat models: a code change and a price row per vendor, for a catalog that grew from one model to seven in three weeks. List price per million tokens is also misleading here. The same request cost 86 input tokens on Kev and 1,127 on Solar Decide, so each vendor's own per-call `usage.cost` is the only honest price.

## Alternatives considered

- **A fixed decider per vendor in `AllDeciders()`.** Rejected: unbounded registration and price-table upkeep for a fast-moving catalog, and `decide` with no list would fan out to every vendor by default.
- **Price slash deciders from the OpenRouter catalog (ADR-009).** Rejected: per-million rates ignore each model's prompt-template token count. The vendor-reported `usage.cost` is exact and needs no lookup.
- **Validate the slug against the catalog's `output_modalities` before sending.** Rejected for now: OpenRouter already answers an unknown or non-decision slug with a cheap 400 ("Model ... does not exist"). A catalog check would make `decide` depend on the advisory catalog that ADR-009 keeps out of the request path.
- **Let slash deciders join chat panels too.** Rejected by ADR-016's class boundary: `GetProvider` still routes a slash token to OpenRouter chat, and `decide` never takes chat tokens.

## Consequences

### Positive
- Any OpenRouter decision model is one token away, and several can share a panel with jev and clef: `conclave decide jev,clef,liquid/d1,upstage/solar-decide`.
- Cost is exact per call. A free model's reported 0 is a real $0, distinct from unpriced (`Decision.ReportedCostUSD` is a pointer). Jev on OpenRouter also prices this way, matching the table to the cent.
- One key (`OPENROUTER_API_KEY`) through `NewKeyRotator`, so `.env` and the keyring work (ADR-008).

### Negative
- A slash token is accepted without local proof that it is a decision model; a wrong slug costs one 400 round-trip. A chat slug (`openai/gpt-6.1-sol`) fails at OpenRouter, not locally.
- `modelIDPattern` now admits `:` for variant suffixes such as `:free`. It is a legal path character, so it cannot redirect Clef's URL, but the rule is looser than before.
- Model-specific limits (Respan noul-only, Kev's 8k context, Tev1's 2-24 choices) surface as vendor 400s, not local validation.
- Behaviour scorers like Respan are reachable but are not general deciders. Putting one on a panel with choice or score questions fails that decider's whole call.

### Non-goals
- No `conclave models` listing of OpenRouter decision models yet; discovery is openrouter.ai or `GET /api/v1/models?output_modalities=decisions`.
- No catalog-driven auto-panel (for example "every decision model under $X").
- Clef is not reachable this way (`cloudflare/clef` does not exist on OpenRouter); it stays a fixed Workers AI decider.

## See also

- `internal/providers/decide_openrouter.go` - construction and the routing contract.
- [ADR-016](ADR-016-decision-models-are-a-separate-provider-class.md) - the decider class this extends (its addendum covers jev's own OpenRouter route).
- [ADR-010](ADR-010-openrouter-as-a-slash-routed-api-backend.md) - the same slash-routing pattern for chat models.
- `docs/PLAN-decision-models.md` - the vendor survey and the Decision Index price/score frontier.
