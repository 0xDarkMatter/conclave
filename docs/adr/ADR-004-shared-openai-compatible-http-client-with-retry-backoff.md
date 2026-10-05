---
status: accepted
date: 2025-12-25
supersedes: []
superseded-by: []
related: [ADR-002]
touches:
  - "internal/providers/api_base.go"
---

# ADR-004: Shared OpenAI-compatible HTTP client with retry backoff

## Decision (one sentence)

API-mode providers that speak the OpenAI Chat Completions shape (OpenAI, Grok, Perplexity, GLM) share one request/response path and one retry layer in `api_base.go` (`chatCompletionRequest` / `extractChatResponse`, exponential backoff on 429/5xx honoring `Retry-After`), rather than each reimplementing HTTP.

## Context

> Reconstructed retroactively on 2026-06-18 from git history (commits `376d00a` added API providers; `cda24c2` "Add exponential backoff retry for API requests"); rationale inferred from `api_base.go` and the provider files.

Most providers expose an OpenAI-compatible `/chat/completions` endpoint, so their request bodies and response parsing are identical up to base URL, model id, and auth header. Duplicating that per provider would also duplicate the fiddly parts — retry/backoff, `Retry-After` handling, error-body extraction. Centralizing it in `apiBaseProvider` means a provider file only specifies what differs (endpoint, key env, default model), and every provider inherits consistent resilience. Anthropic and Gemini, whose wire formats differ, keep their own request/response structs but still embed `apiBaseProvider` for the shared transport.

## Alternatives considered

- **Per-provider HTTP implementations.** Rejected: duplicates transport and retry logic, inviting drift and inconsistent error handling.
- **A third-party multi-provider SDK.** Rejected: a heavy dependency for what is a thin, well-understood request; conclave keeps the surface small and dependency-light.
- **No retries (surface errors immediately).** Rejected: 429/5xx are common and transient under parallel fan-out; backoff materially improves success rate.

## Consequences

### Positive
- New OpenAI-compatible providers are a few lines (endpoint + key + default model).
- Uniform retry/backoff, `Retry-After`, and error extraction across providers.
- ADR-007 reused this path to add GLM over HTTP with no new transport code.

### Negative
- The shared `chatCompletionRequest` is a lowest-common-denominator shape; provider-specific fields require special-casing (e.g. `max_completion_tokens` for gpt-5.x).
- A bug in the shared client affects all OpenAI-compatible providers at once.
- A status code is not enough to decide a retry. OpenAI sends "no credits remaining" (`credit_balance_exhausted`, `insufficient_quota`) as a 429, and retrying it added ~7 s per call before failing anyway (seen 2026-10-01). Since then the retry loop classifies billing by the vendor's error code before looking at the status: those codes, Anthropic `billing_error` and any 402 return on the first attempt as a typed `BillingError`, which batch's adaptive rate limiter also ignores. The code list (`billingErrorCodes`) is per-vendor knowledge and needs updating when a vendor adds a billing code. Gemini's 429 `RESOURCE_EXHAUSTED` is deliberately still retried, because Gemini uses it mostly for per-minute limits.
- 2026-10-01: "5xx" had been implemented as a list (500, 502, 503, 504), so Anthropic's 529 `overloaded_error` got no backoff and a load spike failed a `-g claude` call on its first attempt. `isRetryable` is now the status class (429 and every 5xx), which is what this ADR always said and what Anthropic's own SDKs do; it also covers Cloudflare's 520-524. The accepted cost: a permanent 5xx (501, 505) spends ~7 s of backoff before failing.
- 2026-10-05: Anthropic answers an organization past its usage tier's monthly spend cap with a 429 `rate_limit_error`, the same status and type as a per-minute limit, so each call spent three backoffs (~7 s) and batch's limiter paced for it. Anthropic documents `error.details.error_code` = `enforced_spend_limit_reached` as the way to tell the two apart (docs/api/rate-limits, "Reaching your spend cap"), so `billingCode` now also reads that field and the code returns a `BillingError` at once. The cap's other documented tell, a missing `retry-after`, is deliberately not used: a proxy can strip the header, and a rate limit misread as billing fails a panel member that one backoff would have saved. Anthropic's self-set spend limits are 400s and already fail at once; a Claude Code workspace limit 429 carries `retry-after` and stays retryable.

### Non-goals
- Does not cover non-OpenAI wire formats (Anthropic/Gemini keep bespoke structs).
- Does not define credential sourcing (see ADR-005).

## See also

- `internal/providers/api_base.go` — shared client, `KeyRotator`, retry/backoff, `chatCompletionRequest`/`extractChatResponse`.
- [ADR-002](ADR-002-dual-provider-modes-cli-wrappers-and-direct-api.md) — the API mode this serves.
- [ADR-007](ADR-007-glm-uses-the-coding-plan-http-endpoint-not-the-opencode-cli.md) — GLM reuses this client.
