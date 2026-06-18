---
status: accepted
date: 2025-12-24
supersedes: []
superseded-by: []
related: [ADR-004]
touches:
  - "internal/providers/registry.go"
  - "internal/providers/provider.go"
  - "cmd/root.go:--general"
---

# ADR-002: Dual provider modes — CLI wrappers and direct API

## Decision (one sentence)

conclave runs each provider in one of two modes — the default CLI mode wraps the provider's installed coding CLI (`gemini`, `claude`, `codex`, …), and `-g`/`--general` mode calls the provider's HTTP API directly — with a separate provider list per mode (`AllCLIProviders` / `AllAPIProviders`).

## Context

> Reconstructed retroactively on 2026-06-18 from git history (commits `a7b94b1` v1.0.0 shipped CLI providers; `376d00a` "Add API-based providers for general-purpose queries" added the API mode the same day); rationale inferred from those commits, the code, and `README.md`.

The two modes answer two different needs. **CLI mode** reuses the coding CLIs a developer already has installed and authenticated — no API keys to manage, and those CLIs are tuned/guard-railed for coding tasks, which suits conclave's primary use (reviewing code with multiple models). **API mode** exists for general-purpose questions that the coding CLIs either restrict or shape toward code; `376d00a` introduced it explicitly "for general-purpose queries (no coding restrictions)." Keeping them as separate registries (rather than one provider that picks a transport) keeps availability, defaults, and model ids per-mode, which is why `--list-providers` shows both columns.

## Alternatives considered

- **API-only.** Rejected: would force every user to obtain and manage API keys for every provider, and discard the coding-tuned behavior of the installed CLIs.
- **CLI-only.** Rejected: CLIs impose coding-oriented restrictions and require the binaries installed; useless for general-purpose queries or headless/API environments.
- **One provider, transport chosen by flag.** Rejected: defaults, model ids, and availability genuinely differ between a CLI and its API (e.g. different default models, different auth), so a clean split is simpler than a branching hybrid.

## Consequences

### Positive
- CLI mode works with zero API keys by leaning on already-authed coding CLIs.
- API mode unlocks general-purpose use without coding-tool guardrails.
- Per-mode registries keep defaults/availability explicit and independently evolvable.

### Negative
- Two code paths per provider (`{name}.go` + `api_{name}.go`) to maintain.
- A provider can behave differently across modes (different default models/ids), which can surprise users — hence the side-by-side `--list-providers`.

### Non-goals
- Does not dictate each provider's transport details (CLI invocation vs HTTP) — see per-provider ADRs (e.g. ADR-007 for GLM).
- Does not require every provider to support both modes (e.g. GLM API mode is disabled — ADR-006).

## See also

- `internal/providers/registry.go` — `AllCLIProviders()` / `AllAPIProviders()`.
- `internal/providers/provider.go` — the `Provider` interface both modes implement.
- [ADR-004](ADR-004-shared-openai-compatible-http-client-with-retry-backoff.md) — the HTTP client API-mode providers share.
