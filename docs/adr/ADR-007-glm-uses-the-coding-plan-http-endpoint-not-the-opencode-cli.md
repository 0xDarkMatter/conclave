---
status: accepted
date: 2026-06-18
supersedes: []
superseded-by: []
extends: [ADR-004]
related: [ADR-002, ADR-006, ADR-008]
touches:
  - "internal/providers/glm.go"
  - "internal/providers/api_glm.go"
---

# ADR-007: GLM uses the Coding Plan HTTP endpoint, not the opencode CLI

## Decision (one sentence)

CLI-mode GLM queries the Z.ai GLM Coding Plan over direct OpenAI-compatible HTTP (`https://api.z.ai/api/coding/paas/v4`, overridable via `GLM_BASE_URL`), so GLM requires only an API key and no external CLI binary.

## Context

Every other CLI-mode provider shells out to a vendor CLI (`gemini`, `claude`, `codex`, `grok` — see ADR-002). GLM originally did the same via `opencode run --model zai-coding-plan/glm-5.2`, which made GLM unusable unless `opencode` was installed and separately authenticated — a heavy, Node-based runtime dependency for a single provider, and the only one needing an agent CLI just to send one prompt.

Z.ai exposes the GLM Coding Plan through two first-party endpoints: an OpenAI Chat Completions endpoint (`/api/coding/paas/v4`) and an Anthropic Messages endpoint (`/api/anthropic`). The Coding Plan is a flat subscription — the same entitlement `opencode` was already consuming — and is distinct from the pay-as-you-go `/api/paas/v4` endpoint, whose latency/balance cost is exactly why API-mode GLM is disabled (ADR-006). conclave already had an OpenAI-compatible HTTP path shared across providers (ADR-004), so talking to the Coding Plan directly was wiring, not new transport code.

## Alternatives considered

- **Keep shelling out to `opencode`.** Rejected: forces a Node/agent-CLI install + separate auth for one provider, gives conclave no control over the request, and leaked the `zai-coding-plan/` opencode model-id namespace into conclave config.
- **Use the pay-as-you-go `/api/paas/v4` endpoint** (the existing disabled `api_glm.go`). Rejected for the default: bills per-token against a prepaid balance instead of the flat Coding Plan subscription, so it would silently cost money and 401 for Coding-Plan-only keys (cf. ADR-006).
- **Use the Anthropic Messages endpoint** (`/api/anthropic`). Rejected: conclave's shared plumbing is OpenAI-shaped (ADR-004); the OpenAI-compatible endpoint reuses existing code with zero new parsing.

## Consequences

### Positive
- GLM works with only an API key — no `opencode`, no Node, no second auth step.
- Uses the flat Coding Plan subscription (no pay-as-you-go balance surprise).
- Reuses the ADR-004 HTTP path, retry/backoff, and metrics — GLM behaves like every other API provider.
- Default model id is now the bare API id (`glm-5.2`) instead of opencode-namespaced `zai-coding-plan/glm-5.2`.

### Negative
- Couples GLM to the Coding Plan endpoint's OpenAI-compatibility contract; if Z.ai changes it, GLM breaks where `opencode` would have absorbed it.
- CLI-mode GLM is now key-gated, not binary-gated, so `--list-providers` shows `[not installed]` (the CLI-list label) when no key is set — imprecise wording for a missing key.

### Non-goals
- Does not re-enable API-mode GLM (`-g glm` stays disabled — ADR-006).
- Does not change the other CLI providers — they still wrap their vendor CLIs (ADR-002).

## See also

- `internal/providers/glm.go` — the HTTP implementation (replaces the `opencode` exec).
- [ADR-004](ADR-004-shared-openai-compatible-http-client-with-retry-backoff.md) — the shared client this reuses.
- [ADR-006](ADR-006-glm-api-mode-disabled-for-latency.md) — why the pay-as-you-go API path stays off.
- [ADR-008](ADR-008-api-keys-resolve-from-environment-then-os-keyring.md) — lets the GLM key live in the OS keyring.
