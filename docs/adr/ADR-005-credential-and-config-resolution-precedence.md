---
status: accepted
date: 2025-12-25
supersedes: []
superseded-by: []
related: [ADR-008]
touches:
  - "internal/config/config.go"
  - "internal/config/env.go"
  - "internal/providers/api_base.go"
---

# ADR-005: Credential and config resolution precedence

## Decision (one sentence)

Configuration resolves highest-to-lowest as CLI flags → `CONCLAVE_*` environment variables → `~/.config/conclave/config.yaml` → provider defaults, and API keys resolve as process environment variable → `~/.config/conclave/.env` → `./.env`.

## Context

> Reconstructed retroactively on 2026-06-18 from git history (config precedence present from early commits; `8774088` "Add interactive API key setup" established the `~/.config/conclave/.env` store); rationale inferred from `config.go`, `env.go`, and `AGENTS.md`.

conclave needs predictable overriding for both behavior (models, timeout) and secrets, across interactive use, scripts, and CI. The chosen order puts the most explicit, most local signal first (a `-m`/`-t` flag, or an exported env var) and the most general last (built-in defaults). Secrets follow the same philosophy: an explicitly exported key wins, then a user-level `.env`, then a project-local `.env` for per-repo overrides. This lets a user set keys once in `~/.config/conclave/.env` (what `conclave init` writes) while still allowing a one-off `KEY=… conclave …` or a project `.env` to override.

## Alternatives considered

- **Config file beats env vars.** Rejected: a checked-in or stale file silently overriding an explicit `export` is surprising and breaks CI overrides.
- **Single source only (env, or file).** Rejected: env-only is painful for many keys; file-only breaks ephemeral/CI overrides. A layered order serves both.
- **Project `.env` beats user `.env`.** Kept (project is more local), but project sits *below* the process environment so an explicit export always wins.

## Consequences

### Positive
- One predictable rule for both config and secrets.
- Set-once (`~/.config/conclave/.env`) with per-invocation and per-project override paths.
- CI/containers work via plain env vars with no file.

### Negative
- Multiple key sources mean "why is it using *that* key?" requires knowing the order.
- `.env` files hold secrets in plaintext on disk (mitigated, not replaced, by ADR-008's keyring fallback).

### Non-goals
- Does not introduce a secret store — that is ADR-008 (keyring), which inserts strictly *below* the env var.
- Does not define per-provider key env-var names (those live with each provider).

## See also

- `internal/config/config.go` — flag/env/yaml/default precedence and `CONCLAVE_*` overrides.
- `internal/config/env.go` — `.env` loading/saving.
- `internal/providers/api_base.go` — `NewKeyRotator` env resolution.
- [ADR-008](ADR-008-api-keys-resolve-from-environment-then-os-keyring.md) — extends this with an OS-keyring fallback.
