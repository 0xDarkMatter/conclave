---
status: accepted
date: 2025-12-25
supersedes: []
superseded-by: []
related: [ADR-002, ADR-007]
touches:
  - "internal/providers/registry.go"
---

# ADR-006: GLM API mode disabled for latency

## Decision (one sentence)

GLM is excluded from API mode — `NewGLMAPIProvider()` is commented out of `AllAPIProviders()` — so `-g glm` is unavailable, while GLM remains usable in CLI mode.

## Context

> Reconstructed retroactively on 2026-06-18 from git history (commit `f74caae`, "Disable GLM API provider (too slow)"); rationale inferred from that commit and `registry.go`.

The pay-as-you-go GLM API (`open.bigmodel.cn` / `api.z.ai/api/paas/v4`) was slow enough at the time to drag down API-mode runs (where conclave waits on the slowest provider — ADR-003), so it was removed from the API-mode roster rather than degrade every `-g` run. A later code comment also notes the pay-as-you-go endpoint requires a prepaid account balance, which is a second reason to keep it off by default. The provider code (`api_glm.go`) was kept intact so the decision is a one-line re-enable if those conditions change.

## Alternatives considered

- **Keep GLM in API mode despite latency.** Rejected: under per-provider-but-parallel execution a chronically slow provider still inflates wall-clock and failure rate for `-g` runs and `--all`.
- **Delete the API provider entirely.** Rejected: the code is correct and cheap to keep; commenting it out of the registry preserves an easy re-enable.

## Consequences

### Positive
- `-g`/`--all` API runs aren't dragged down by a slow GLM endpoint.
- Avoids surprise pay-as-you-go billing for users without a balance.

### Negative
- `-g glm` silently unavailable; a user expecting API-mode GLM finds it missing from the list.
- The disable lives as a code comment, not config — re-enabling needs a rebuild.

### Non-goals
- Does not affect **CLI-mode** GLM, which remains available and was later moved onto the Coding Plan HTTP endpoint — see ADR-007.
- Does not remove the API provider code; it stays for an easy future re-enable.

## See also

- `internal/providers/registry.go` — the commented-out `NewGLMAPIProvider()` in `AllAPIProviders()`.
- [ADR-002](ADR-002-dual-provider-modes-cli-wrappers-and-direct-api.md) — the dual-mode design this is an exception within.
- [ADR-007](ADR-007-glm-uses-the-coding-plan-http-endpoint-not-the-opencode-cli.md) — CLI-mode GLM transport (separate from this API-mode exclusion).
