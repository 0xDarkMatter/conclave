# AGENTS.md

> Instructions for AI coding assistants working on Conclave.

## Project Overview

Conclave is a Go CLI that queries multiple LLM providers in parallel and synthesizes their responses. It operates in two modes:

- **CLI Mode** (default): Wraps provider CLIs (`gemini`, `claude`, `codex`, etc.). Exception: `glm` calls the Z.ai Coding Plan over HTTP directly (no CLI binary) — see ADR-007.
- **API Mode** (`-g`): Direct API calls to providers

> Architectural decisions are recorded in `docs/adr/` (the directory is the index). Run `python ~/.claude/skills/adr-ops/scripts/adr-touching.py <path>` to find which ADR governs a file before changing it.

## Architecture

```
cmd/
  root.go          # Main CLI entry, flag parsing, orchestration
  init.go          # Interactive API key setup

internal/
  config/          # Configuration loading (.env, config.yaml)
  context/         # File/stdin context building
  judge/           # Verdict synthesis logic
  orchestrator/    # Parallel provider execution
  output/          # Result formatting (JSON, human, brief)
  pricing/         # Cached OpenRouter model catalog: drift warnings, batch prices (ADR-009)
  cache/           # Opt-in response cache, $XDG_CACHE_HOME/conclave/responses/ (ADR-011)
  progress/        # Terminal progress display
  providers/       # Provider implementations
    provider.go    # Provider interface
    registry.go    # Provider registration and lookup
    gemini.go      # CLI provider
    api_gemini.go  # API provider
    api_openrouter.go  # Slash-routed OpenRouter backend: any vendor/model token in -g mode (ADR-010)
    ...
```

## Key Conventions

### Provider Pattern

Each provider implements the `Provider` interface:

```go
type Provider interface {
    Name() string
    DefaultModel() string
    IsAvailable() bool
    Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error)
}
```

- CLI providers wrap external commands (`gemini.go`, `claude.go`) and embed `baseProvider`
- API providers make HTTP calls (`api_gemini.go`, `api_anthropic.go`) and embed `apiBaseProvider`
- Exception: `glm.go` is a CLI-mode provider that embeds `apiBaseProvider` (HTTP to the Coding Plan endpoint, no binary) — ADR-007

### Slash Routing (OpenRouter)

In API mode any `vendor/model` token is built on the fly as an `OpenRouterAPIProvider` (name = model = token). No plain `openrouter` provider, never in `AllAPIProviders`, rejected in CLI mode. Contract and rationale: `docs/OPENROUTER.md`, ADR-010; the trap list is Gotcha 9.

### Adding a New Provider

1. Create `internal/providers/{name}.go` (CLI) and/or `api_{name}.go` (API)
2. Implement the `Provider` interface
3. Register in `AllCLIProviders()` or `AllAPIProviders()` in `registry.go`
4. Add to `providerSetupInfo` in `cmd/init.go` for interactive setup
5. Add its OpenRouter vendor prefix to `vendorPrefix` in `internal/pricing/catalog.go` (or drift checks silently skip it)
6. Update tests in `provider_test.go`
7. Document in `README.md` and `docs/MODEL_REGISTRY.md`

### Changing a Default Model

Defaults live in `internal/config/config.go` (`Models`, `CheapModels`) and each provider's `defaultModel`. After changing one, run `conclave models --check` (exit 2 on drift, 3 if the catalog is unreachable) and update the Defaults / Cheap Mode tables in `docs/MODEL_REGISTRY.md` in the same commit.

### Error Handling

- API providers use exponential backoff for 429/5xx errors (see `api_base.go`)
- Up to 3 retries with 1s base delay, respects `Retry-After` headers
- Context cancellation is respected throughout

### Configuration

Priority (highest to lowest):
1. CLI flags (`-m`, `-t`, etc.)
2. Environment variables (`CONCLAVE_*`)
3. Config file (`~/.config/conclave/config.yaml`)
4. Provider defaults

API keys loaded from (highest to lowest):
1. Environment variables (`GEMINI_API_KEY`, etc.)
2. `~/.config/conclave/.env`
3. `./.env` (project overrides)
4. OS keyring fallback (service `conclave`, account = env-var name) when the var is unset — `conclave keyring set <ENV_VAR>`; see ADR-005/ADR-008

## Common Tasks

### The Gate

```bash
make check
```

One command gates everything: vet, gofmt, tests, race, and catalog drift. Run it
before every commit. CI runs the same command. Details and the exit-code
contract: [docs/CHECK_GATE.md](docs/CHECK_GATE.md).

### Build, install, smoke-test

`make install` builds and copies to `~/.local/bin`. `./bin/conclave --list-providers` (add `-g` for API mode) shows what is configured; `./bin/conclave <provider> "Say hello" --no-judge` is the smoke test. Per-package tests: `go test ./internal/providers/...`.

## Important Files

| File | Purpose |
|------|---------|
| `cmd/root.go` | CLI entry point, flag definitions |
| `internal/providers/registry.go` | Provider lookup, `AnyAvailable()` |
| `internal/providers/api_base.go` | Shared API logic, retry handling, `KeyRotator` + OS-keyring fallback |
| `internal/judge/judge.go` | Verdict synthesis prompt and parsing |
| `internal/config/env.go` | .env file loading/saving |
| `cmd/keyring.go` | `conclave keyring set/list/rm` — manage keys in the OS keyring |
| `cmd/cache.go` | `conclave cache stats/clear` — inspect or empty the response cache |
| `internal/cache/` | Opt-in response cache and its Provider decorator; nil = disabled (ADR-011) |
| `cmd/models.go` | `conclave models [provider] [--check\|--refresh\|--json]` — inspect the pricing catalog, gate drift |
| `internal/providers/api_openrouter.go` | OpenRouter transport + `/auth/key` preflight; `IsOpenRouterModel` is the routing rule |
| `docs/OPENROUTER.md` | User guide for slash-routed OpenRouter models: setup, slugs, cost, judge rule, error decoder |
| `internal/pricing/catalog.go` | OpenRouter catalog cache, TTL, vendor-id → slug rewriter; advisory, nil-safe |
| `docs/adr/` | Architecture Decision Records (the directory is the index) |

## Code Style

- Standard Go formatting (`gofmt`)
- Error wrapping with context: `fmt.Errorf("action: %w", err)`
- Table-driven tests preferred
- Keep providers self-contained (one file per provider per mode)

## Testing Notes

- Provider tests check registration and default models
- Config tests use temp directories and mock `EnvFilePath`
- Some tests skip if CLIs not installed (e.g., `TestRegistryGetProvider`)
- Judge parsing tests cover JSON extraction edge cases

## Gotchas

1. **Model names**: CLI and API modes may use different model identifiers
2. **Timeouts**: Per-provider timeout, not total - parallel execution
3. **GLM API mode disabled**: `-g glm` is excluded (pay-as-you-go endpoint latency/balance — ADR-006). CLI-mode `glm` works via the Coding Plan HTTP endpoint (ADR-007).
4. **Blind mode**: Anonymizes provider names for unbiased judging
5. **Pricing catalog is advisory**: `internal/pricing` may return a nil catalog (offline, `CONCLAVE_NO_PRICING=1`); every caller must tolerate nil. A model missing from OpenRouter is a warning, never an error — the GLM Coding Plan serves ids OpenRouter does not list. Cache: `$XDG_CACHE_HOME/conclave/openrouter-models.json`, TTL `CONCLAVE_PRICING_TTL` hours (default 24). Prices are API-mode only; CLI mode is subscription-billed.
6. **Batch cost fallback table**: `fallbackCosts` in `internal/batch/processor.go` is only used when the catalog is unavailable. Do not extend it; fix the catalog lookup instead.
7. **gemini CLI needs a key even in CLI mode** (Google retired its free OAuth tier, 2026-09). `gemini.go` passes `-p` and `--skip-trust`; removing either reintroduces an interactive hang or exit 55. On a CLI auth failure it falls back to the direct API with the same key (`isGeminiCLIAuthError`). codex and claude CLIs run on subscriptions and must NOT be gated on API keys. User-side remedy is in README "Requirements".
8. **Never prompt without a TTY**: `RunInitIfNeeded` bails when stdin is not a terminal. Subprocess callers (praxis grade) cannot answer a prompt; a prompt there is a hang.
9. **Slash tokens are API-only**: `vendor/model` provider tokens route through OpenRouter and exist only under `-g` (pay-as-you-go, no subscriptions, ~5% platform fee). CLI mode errors with "add -g". The catalog never rewrites slash tokens, so a slug OpenRouter does not list warns and is still sent through — except as the **judge**, where a catalog miss is refused before the panel spends anything (`--skip-preflight` overrides). The judge is resolved and preflighted before orchestration for the same reason. Do not add a plain `openrouter` provider or put OpenRouter in `AllAPIProviders` — ADR-010 rejected both.
10. **Response cache** (ADR-011): key = `(mode, provider, model, full prompt incl. `-f`/stdin, system)`, so a changed file is a miss by design; the judge is never cached. Two traps: a hit keeps `status: "success"` and signals only via `cached` (downstream parsers treat any other status as a failed judge; pinned by `TestCachedHitKeepsStatusSuccess`), and every provider decorator must implement `Unwrap() Provider` or `RunPreflight` silently skips that provider's auth check.
