---
status: accepted
date: 2025-12-24
supersedes: []
superseded-by: []
related: [ADR-001]
touches:
  - "internal/orchestrator"
  - "cmd/root.go:--timeout"
---

# ADR-003: Parallel queries with per-provider timeouts

## Decision (one sentence)

conclave queries all selected providers concurrently and applies the `--timeout` (default 60s) as a per-provider deadline, not a global one, so a slow or hanging provider cannot block the others and partial results still reach the judge.

## Context

> Reconstructed retroactively on 2026-06-18 from git history (commit `a7b94b1`, conclave v1.0.0); rationale inferred from the orchestrator code and the `AGENTS.md` gotcha "Timeouts: Per-provider timeout, not total".

conclave's latency floor is the slowest model queried. Running providers sequentially would make total time the sum of all providers; a single hung provider under a global deadline could also starve the rest. The orchestrator therefore fans out concurrently and bounds each provider with its own context timeout. A provider that errors or times out is reported as a failed result, and the judge synthesizes whatever succeeded rather than the whole run failing.

## Alternatives considered

- **Sequential queries.** Rejected: total latency = sum of providers; defeats the point of a multi-model tool.
- **Single global deadline across all providers.** Rejected: one slow provider consumes the shared budget and can cause otherwise-fine providers to be cut off; per-provider isolation is fairer and more predictable.
- **Fail the whole run if any provider fails.** Rejected: partial answers are still useful; the judge handles a subset.

## Consequences

### Positive
- Wall-clock ≈ slowest single provider, not the sum.
- One hanging provider can't sink the run; others and the judge still complete.
- Failures degrade gracefully into partial synthesis.

### Negative
- `--timeout` is per-provider, so worst-case total time and cost scale with provider count — a recurring point of user confusion.
- Concurrency adds rate-limit pressure (mitigated by ADR-004's backoff and batch-mode worker caps).

### Non-goals
- Does not define the synthesis behavior over partial results (see ADR-001).
- Does not set batch-mode concurrency limits (a separate `--workers` concern).

## See also

- `internal/orchestrator` — concurrent execution and per-provider context timeouts.
- `cmd/root.go` — `--timeout` flag.
- [ADR-001](ADR-001-llm-as-judge-verdict-synthesis.md) — the judge that consumes the (possibly partial) results.
