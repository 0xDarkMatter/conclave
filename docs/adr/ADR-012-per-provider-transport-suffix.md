---
status: accepted
date: 2026-09-13
supersedes: []
superseded-by: []
extends: [ADR-002]
related: [ADR-006, ADR-009, ADR-010, ADR-011]
touches:
  - "internal/providers/transport.go"
  - "internal/providers/registry.go:GetProvider"
  - "internal/providers/provider.go:Response.Transport"
  - "internal/orchestrator/orchestrator.go"
  - "internal/cache/provider.go:Wrap"
  - "internal/output/cost.go"
  - "internal/judge/judge.go:Verdict.JudgeTransport"
  - "cmd/root.go:parseModelOverrides"
  - "cmd/root.go:warnSubscriptionIdle"
---

# ADR-012: Per-provider transport with a `@cli` / `@api` token suffix

## Decision (one sentence)

A provider token may carry a transport suffix, `<provider>@cli` or `<provider>@api`, that pins that one provider to its CLI wrapper or its direct API regardless of `-g` / `-c`; a bare token keeps following the global mode, the suffix is parsed once and never becomes part of the provider's name, and every downstream decision that used to read the global mode (pricing, the cache key, the subscription warning, availability errors) now reads the transport recorded on each response.

## Context

ADR-002 gave conclave two modes with two provider registries and one switch, `-g`, that moved the whole panel between them. That was the right shape while every provider had a free path on one side: CLI mode ran on subscriptions (Claude Max, ChatGPT Pro via codex), API mode on metered keys, and a panel was either one or the other.

By 2026-09 the sides no longer lined up per provider. Google retired the gemini CLI's free OAuth tier, so gemini needs an API key in either mode and its CLI wrapper is now a slower route to the same metered call. openai and claude, meanwhile, are cheapest and best-provisioned on their subscription CLIs. Praxis, the main caller, grades every question with `gemini,openai,claude`; because gemini forced `-g`, every grade billed `OPENAI_API_KEY` and `ANTHROPIC_API_KEY` while the ChatGPT Pro and Claude Max plans sat idle. The subscription-idle warning added the day before could say so, but its only remedy was "drop `-g`", which would have broken gemini. The only workaround was two conclave invocations and a merge step in the caller, which is exactly the plumbing Praxis is trying to delete (`docs/PLAN-reliability-judging.md`, open question 3).

The constraint that shaped the design is identity. Provider names key everything: the progress line, `--json`'s `responses.<provider>`, the judge label, the pricing catalog's vendor prefix, model overrides, and the response cache key. A transport is not part of that identity; `claude@cli` and `claude@api` are the same provider on different wires. So the suffix had to be a resolution-time instruction that leaves the name untouched, with the transport travelling alongside the response for the few consumers that need it.

## Alternatives considered

- **A per-provider environment variable** (`CONCLAVE_CLAUDE_TRANSPORT=cli`). Rejected: invisible in the command that produced a result, easy to leave set across unrelated runs, and it moves a per-invocation choice out of the invocation. It also cannot express "this call only", which is what a grader mixing panels needs.
- **Two conclave invocations, one per transport, merged by the caller.** Rejected: it doubles preflight and progress, splits `--json` into two documents the caller must stitch back together (and re-derive `meta.total_cost_usd` for), and makes a judge over the whole panel impossible without a third invocation. It is the status quo Praxis has today and the reason for this ADR.
- **Making `-g` positional** (`conclave claude -g gemini openai`). Rejected: cobra parses flags globally, so the semantics would have to be faked; the readability is poor; and it cannot pin the judge or a `-m` override, both of which are named by provider, not by position.
- **A separate `--transport claude=cli,gemini=api` flag.** Rejected: it says the same thing as the suffix in a second place, so the provider list and the flag can disagree; the suffix keeps the transport next to the name it applies to, including in `--judge` and `-m`.
- **Let the suffix become part of the provider name** (`responses["claude@cli"]`). Rejected: every consumer keyed on the bare name would silently miss the provider, the pricing catalog would fail to resolve the vendor, and the cache would treat the two spellings as different providers rather than different transports of one provider. The transport is an additive field instead (`responses.<provider>.transport`, `Response.Transport`).
- **Route slash tokens through the suffix too** (`deepseek/x@cli`). Rejected: OpenRouter has no CLI (ADR-010). `@cli` on a slug is an error naming why; `@api` on a slug is accepted and makes `-g` unnecessary for that token, because the explicit transport is the authorisation `-g` used to supply.

## Consequences

### Positive
- One invocation, one panel, one `--json`, mixed billing: `conclave gemini@api,openai@cli,claude@cli "..." --no-judge --json` prices gemini and leaves openai and claude unpriced with `transport: "cli"`. Praxis can drop its roost routing for claude.
- Purely additive. Every existing command line means what it meant: bare tokens follow `-g` / `-c`, `--all` is unchanged, `--json` gains one optional field per response and nothing moves.
- The registry now holds both provider sets and picks per token, so `-m openai:gpt-5.6-sol` and `-m openai@cli:gpt-5.6-sol` both apply to openai; overrides are keyed by bare name.
- Pricing, the cache key's mode component, the subscription-idle warning and the availability error message all follow the response's or provider's actual transport. The warning's remedy is now `<provider>@cli`, which fixes the one provider without moving the panel.
- Under `-c`, a provider pinned to `@cli` gets its normal CLI default model rather than an API-oriented cheap id the CLI wrapper cannot use.

### Negative
- One more spelling to learn, and a token grammar (`name[@cli|@api]`, split on the last `@`) that a future provider slug containing `@` would have to respect. Unknown suffixes are rejected rather than passed upstream, so a typo fails fast.
- A global "API mode" flag no longer exists as a pricing input. `output.Options.APIMode` is gone; anything that constructs `Response` values outside the orchestrator must set `Transport` or they price as nothing. Tests and batch injection were updated; the failure mode is a missing cost figure, never a fabricated one.
- `@api` on a provider with no API side (glm, ADR-006) is an error that cites the ADR and the working spelling. There is no partial fallback.
- `providers.AnyAvailable` is still per mode, so the first-run init check counts both transports whenever any token carries a suffix rather than reasoning about which suffix.

### Non-goals
- Does not change ADR-002's two registries or any provider implementation; it changes which of the two a token selects.
- Does not add a transport to `--all`, which keeps taking its list from the global mode.
- Does not make the CLI transport priceable. Subscriptions have no per-token cost (ADR-009); a `@cli` leg simply carries no dollar figure and does not mark the total as a floor.

## Addendum (2026-09-13): a config-file default

The first cut deliberately persisted nothing. The same day, a `transports:` map was added to `config.yaml` (with `CONCLAVE_<PROVIDER>_TRANSPORT` as the environment form) so a standing choice such as `gemini: api, claude: cli` does not have to be retyped on every call. This is not the per-provider environment variable rejected above: it lives in the same visible, per-user file as the model defaults, follows the same precedence rules the README already documents, and is validated by the registry so a typo fails the run naming the key. Precedence is suffix, then config, then `-g` / `-c`: what is typed on this invocation always wins, and the panel-wide flag only reaches providers nobody has said anything about.

## See also

- `internal/providers/transport.go` — `ParseProviderToken`, `BareName`, `TransportOf`, and the contract block.
- `internal/providers/registry.go` — dual sets, `GetProvider`'s per-token pick, `noTransportError`.
- `internal/output/cost.go` — per-response gating on `Response.Transport`.
- `internal/cache/provider.go` — `Wrap` reads the declared transport before the fallback mode.
- `docs/PLAN-reliability-judging.md` — open question 3, now answered by this record.
- [ADR-002](ADR-002-dual-provider-modes-cli-wrappers-and-direct-api.md) — the two modes this extends.
- [ADR-006](ADR-006-glm-api-mode-disabled-for-latency.md) — why `glm@api` is refused.
- [ADR-009](ADR-009-runtime-pricing-catalog-from-openrouter.md) — why CLI legs are never priced.
- [ADR-010](ADR-010-openrouter-as-a-slash-routed-api-backend.md) — why slash tokens are API-only.
- [ADR-011](ADR-011-opt-in-response-cache-keyed-on-the-full-prompt.md) — the cache key whose mode component now comes from the transport.
