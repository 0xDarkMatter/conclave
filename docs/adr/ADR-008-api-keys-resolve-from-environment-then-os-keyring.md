---
status: accepted
date: 2026-06-18
supersedes: []
superseded-by: []
extends: [ADR-005]
related: [ADR-007]
touches:
  - "internal/providers/api_base.go"
  - "cmd/keyring.go"
  - "cmd/init.go"
---

# ADR-008: API keys resolve from environment then OS keyring

## Decision (one sentence)

When a provider's API-key environment variable is unset, conclave falls back to the OS keyring (service `conclave`, account = the env-var name) via the pure-Go `zalando/go-keyring` library, and ships a `conclave keyring set|list|rm` command that writes through the same library.

## Context

Per ADR-005, conclave resolved API keys from the process environment, optionally seeded from `.env`. That leaves users exporting secrets into every shell or keeping them in a plaintext `.env`. A user asked to store a key in the OS keyring — the standard secure local secret store — and not re-provide it.

A keyring fallback also needs a *write* path, and the OS keyring is not one format: Python's `keyring` and Go keyring libraries use different Windows Credential Manager target-name conventions, so a key written by `python -m keyring` is not found by a Go reader (this exact mismatch surfaced during implementation). Whatever conclave reads with, it must also write with. `zalando/go-keyring` is pure Go (Windows Credential Manager via `danieljoos/wincred`, macOS Keychain, Linux Secret Service) — no external binary, consistent with ADR-007's goal of removing runtime CLI dependencies.

## Alternatives considered

- **Shell-profile hook** (`export KEY=$(keyring get …)`). Rejected as primary: only helps shells sourcing that profile, runs a subprocess per shell start, and re-introduces an external `keyring` binary dependency.
- **Persistent Windows user env var / registry (`setx`).** Rejected: stores the secret in the registry in plaintext and pollutes the global environment — explicitly rejected by the user.
- **Shell out to the Python `keyring` CLI.** Rejected: reintroduces an external-binary dependency and is the source of the target-name mismatch.
- **Keyring only, no env var.** Rejected: env vars must stay first so CI, containers, and one-off `KEY=… conclave …` keep working.

## Consequences

### Positive
- A key stored once in the OS keyring loads automatically every run — no export, no plaintext file.
- The fallback lives in `NewKeyRotator`, so it covers **every** provider uniformly, not just GLM.
- `conclave keyring set/list/rm` reads and writes with the same library, eliminating the Python-vs-Go target-name mismatch.

### Negative
- Adds a third-party dependency (`zalando/go-keyring` + `danieljoos/wincred` + `godbus/dbus`) — small and pure-Go, but a new supply-chain surface.
- A keyring lookup runs at provider construction when the env var is empty (a few Credential Manager reads at startup / `--list-providers`); negligible but non-zero.
- On headless Linux without a Secret Service/D-Bus, `keyring.Get` errors; treated as "no key" (graceful), so env-only setups never break.

### Non-goals
- Does not change the env → `.env` precedence (ADR-005); the keyring is strictly a fallback *below* the env var.
- Does not migrate existing `.env` keys into the keyring, nor deprecate `conclave init` / the `.env` path.
- Does not manage or encrypt the keyring backend itself — that is the OS's responsibility.

## See also

- `internal/providers/api_base.go` — `KeyringService` const and the keyring fallback in `NewKeyRotator`.
- `cmd/keyring.go` — the `set|list|rm` management command.
- [ADR-005](ADR-005-credential-and-config-resolution-precedence.md) — the precedence this extends.
- [ADR-007](ADR-007-glm-uses-the-coding-plan-http-endpoint-not-the-opencode-cli.md) — the GLM transport decision this complements.
