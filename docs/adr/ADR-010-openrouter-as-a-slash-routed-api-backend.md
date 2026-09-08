---
status: accepted
date: 2026-09-08
supersedes: []
superseded-by: []
extends: [ADR-002]
related: [ADR-004, ADR-008, ADR-009]
touches:
  - "internal/providers/api_openrouter.go"
  - "internal/providers/registry.go:GetProvider"
  - "internal/pricing/catalog.go:Lookup"
  - "cmd/root.go:--list-providers"
  - "cmd/init.go"
---

# ADR-010: OpenRouter as a slash-routed API backend

## Decision (one sentence)

In API mode (`-g`) any provider token containing a slash (`deepseek/deepseek-v4-pro`, `anthropic/claude-opus-5`) is routed through OpenRouter's OpenAI-compatible endpoint with the token serving as both the provider name and the model id, constructed on demand by the registry rather than registered as a provider; there is no plain `openrouter` provider, CLI mode rejects slash tokens, and `--all` never auto-includes OpenRouter models.

## Context

Conclave has six direct providers. Anything else a user wants on a panel (DeepSeek, Mistral, Qwen, Kimi, a second Anthropic model alongside the first) needs either a new `api_{name}.go` per vendor, or an aggregator. OpenRouter already sits in the codebase as the reference feed for pricing and drift (ADR-009), speaks the same Chat Completions shape as four of the direct providers (ADR-004), and one key covers every vendor it lists.

The awkward part is the provider identity. Conclave's registry, progress display, judge label, `--json` output and pricing lookups all key on a provider *name*, and a panel may legitimately want two OpenRouter models at once (`deepseek/deepseek-v4-pro` and `qwen/qwen3.5-max`). A single `openrouter` provider with `-m openrouter:<model>` can only carry one model per panel, and its name would say nothing about what answered. Using the OpenRouter slug itself as the provider name resolves both: every token is unique, self-describing in every output surface, and already the exact id the pricing catalog indexes, so drift warnings and batch cost estimates work with a one-line special case in `Lookup`.

OpenRouter is pay-as-you-go only. The subscriptions that make CLI mode free (Claude Max, Codex, GLM Coding Plan) cannot be reached through it, and it adds a platform fee of roughly 5% over the vendor's list price. Direct providers therefore stay the better route for gemini, openai and claude; OpenRouter is for models conclave would otherwise not have at all.

## Alternatives considered

- **A plain `openrouter` provider selected with `-m openrouter:<model>`.** Rejected: one OpenRouter model per panel (the registry keys on the name), an opaque `openrouter` label in progress, judge and JSON output, and a second naming scheme for the pricing catalog to reconcile.
- **Route every provider through OpenRouter.** Rejected: loses the subscription-billed CLI mode entirely, pays the platform fee on models conclave already reaches directly, and drops provider-specific request fields (`max_completion_tokens` for gpt-5.x, Anthropic's native wire format).
- **One `api_{vendor}.go` per additional vendor.** Rejected: unbounded maintenance for a long tail of vendors that all speak the same OpenAI-compatible shape OpenRouter already normalises.
- **Slash tokens in CLI mode too.** Rejected: there is no CLI or subscription behind an OpenRouter slug; a clear "API-only, add `-g`" error is more honest than silently switching modes.

## Consequences

### Positive
- Any model in OpenRouter's catalog is one token away, with no code change per vendor; two or more OpenRouter models can sit on the same panel or act as judge.
- The token is the id everywhere: progress line, judge label, `--json`, the pricing catalog. Drift warnings, "newest listed" hints and batch cost estimates come for free (`Lookup` matches a slash token verbatim; `VendorPrefix` derives the vendor from it).
- One key (`OPENROUTER_API_KEY`) through `NewKeyRotator`, so `.env` and the OS keyring (ADR-008) both work; `conclave init` and `conclave keyring list` know the variable.
- Preflight hits `GET /auth/key` (no tokens spent) and turns an exhausted spend limit into a "no credit" message before the panel runs.

### Negative
- Discovery is indirect: `--list-providers` shows a placeholder `openrouter` row and a note, and users find real slugs via `conclave models` or openrouter.ai. There is deliberately no hardcoded list.
- The pricing catalog never rewrites slash tokens, so a vendor-style id in a slug (`anthropic/claude-opus-4-8`) is a miss and a drift warning, by design: the user typed an OpenRouter slug, and the catalog is the authority on those.
- `IsOpenRouterModel` is "contains a slash with non-empty halves". Provider names containing a slash for any other reason would be misrouted; none exist and none are planned. `/model` and `model/` are rejected as malformed rather than sent upstream.
- Display names for slash tokens depend on the catalog label ("DeepSeek: DeepSeek V4 Pro"); offline or with `CONCLAVE_NO_PRICING=1` the raw slug is shown.

### Non-goals
- Does not add OpenRouter to `AllAPIProviders`, so `--all -g` is unchanged and never fans out across the catalog.
- Does not change which provider is the default judge, nor any compiled default model.
- Does not attempt OpenRouter-specific features (provider routing preferences, fallbacks, `:free`/`:nitro` variants beyond passing the slug through verbatim).
- Does not make the catalog authoritative for routing panel members: a slug the catalog does not list is still sent to OpenRouter, with a warning, because the feed lags. The one exception is the judge: a slash-routed judge missing from the catalog is refused before the panel runs (the alternative is paying for the whole panel and then failing at synthesis); `--skip-preflight` sends it anyway. The judge is resolved and preflighted before orchestration for the same reason.

## See also

- `internal/providers/api_openrouter.go` — transport, attribution headers, preflight, and the routing contract in the file header.
- `internal/providers/registry.go` — `GetProvider` slash branch, `OpenRouterListing`, `SetOpenRouterNamer` / `DisplayName`.
- `internal/pricing/catalog.go` — the `Lookup` / `VendorPrefix` slash special case and `NameOf`.
- [ADR-002](ADR-002-dual-provider-modes-cli-wrappers-and-direct-api.md) — the API mode this extends; CLI mode is untouched.
- [ADR-004](ADR-004-shared-openai-compatible-http-client-with-retry-backoff.md) — the shared client this rides.
- [ADR-008](ADR-008-api-keys-resolve-from-environment-then-os-keyring.md) — why the key resolves through `NewKeyRotator`.
- [ADR-009](ADR-009-runtime-pricing-catalog-from-openrouter.md) — the catalog whose slugs double as provider names here.
