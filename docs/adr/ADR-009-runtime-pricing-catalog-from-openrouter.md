---
status: accepted
date: 2026-09-08
supersedes: []
superseded-by: []
extends: []
related: [ADR-002, ADR-004]
touches:
  - "internal/pricing"
  - "internal/batch/processor.go"
  - "cmd/models.go"
  - "cmd/root.go"
  - "docs/MODEL_REGISTRY.md"
---

# ADR-009: Runtime pricing catalog from OpenRouter, cached daily

## Decision (one sentence)

Conclave keeps a locally cached copy of OpenRouter's unauthenticated models feed (`https://openrouter.ai/api/v1/models`), refreshed at most once per `CONCLAVE_PRICING_TTL` hours (default 24) and never on the query's critical path once a cache exists, and uses it for three advisory purposes: warning when a configured model id is no longer listed, pricing batch-mode cost estimates, and the `conclave models` inspection command.

## Context

Model ids and prices live in three places that drift independently: the compiled defaults in `internal/config/config.go`, a hardcoded price table in `internal/batch/processor.go`, and the hand-maintained `docs/MODEL_REGISTRY.md`. By 2026-09 the registry named a shut-down Gemini model as the default, two compiled grok ids and one GLM id no longer existed on any aggregator, and the batch table priced gpt-5-nano at double its list price. Nothing in the tool could notice.

The user asked for OpenRouter to be checked "every time conclave runs" so the reference cannot go stale. OpenRouter is the right source: no auth, one feed covers all six vendors conclave talks to, and its per-token prices are pass-through vendor list prices. It is a proxy for the vendor, though. A model missing from OpenRouter is a strong hint the vendor retired it, not proof.

Two constraints shaped the design. First, Conclave's pitch is speed; a blocking HTTP call before every query would tax every invocation for information that changes weekly. Second, most runs here are CLI mode on subscriptions (Claude Max, Codex, GLM Coding Plan), where the per-token price is $0 and only the existence check matters.

## Alternatives considered

- **Fetch synchronously on every run.** Rejected: adds network latency and an offline failure mode to every query for data that changes on a weekly cadence.
- **Vendor APIs directly** (`/v1/models` per vendor). Rejected: six auth schemes, several without prices, and it only works for providers whose keys are configured.
- **Manual-only command that regenerates the registry doc.** Rejected as insufficient: it keeps the doc honest but the running binary still cannot warn about a retired default.
- **Make a catalog miss a hard error.** Rejected: OpenRouter's coverage lags and differs from vendor endpoints (the GLM Coding Plan serves ids OpenRouter never lists). Advisory only.

## Consequences

### Positive
- A retired default now produces a one-line stderr warning naming the newest listed replacement, instead of a silent 404 minutes later.
- Batch cost estimates use the model actually queried, at current prices, with the compiled table demoted to an offline fallback.
- `conclave models --check` is a pre-release drift gate with an exit code, and `conclave models [provider]` replaces reading the registry doc for "what exists and what it costs".
- One synchronous fetch in the lifetime of a cache (first run or `--refresh`), bounded at 6 seconds; every later run reads a file. Stale caches are served immediately and refreshed by a goroutine the CLI gives a 2-second grace period at exit.

### Negative
- One more network dependency and one more file on disk (`$XDG_CACHE_HOME/conclave/openrouter-models.json`, about 1 MB). `CONCLAVE_NO_PRICING=1` removes both.
- Vendor id to OpenRouter slug mapping is heuristic (`claude-opus-4-8` to `claude-opus-4.8`, dated Anthropic suffixes dropped, xAI reasoning variants collapsed). A new naming convention from a vendor could produce a false "missing" warning until the rewriter learns it. Tests pin the known cases.
- `docs/MODEL_REGISTRY.md` remains hand-written prose and can still lag; it now carries a maintenance rule and points readers at `conclave models` for live data.

### Non-goals
- Does not change any default model. The drift check reports; humans bump `config.go`.
- Does not price CLI-mode runs. Subscriptions have no per-token cost and the tool says so wherever it prints prices.
- Does not route requests through OpenRouter. It is a reference feed only; ADR-002's provider modes are unchanged.

## See also

- `internal/pricing/catalog.go` — cache rules, TTL, slug rewriter, and the advisory contract in the package doc.
- `cmd/models.go` — the `models` command and the `--check` gate.
- `cmd/root.go` — `loadCatalog` / `warnModelDrift`.
- `internal/batch/processor.go` — `priceFor` and the demoted `fallbackCosts` table.
- `docs/MODEL_REGISTRY.md` — the human-readable registry this keeps honest.
- [ADR-002](ADR-002-dual-provider-modes-cli-wrappers-and-direct-api.md) — why CLI mode is subscription-billed and API mode is metered.
