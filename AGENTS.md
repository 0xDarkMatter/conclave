# AGENTS.md

> Instructions for AI coding assistants working on Conclave.

## Project Overview

Conclave is a Go CLI that queries multiple LLM providers in parallel and synthesizes their responses. It operates in two modes:

- **CLI Mode** (default): Wraps provider CLIs (`gemini`, `claude`, `codex`, etc.). Exception: `glm` calls the Z.ai Coding Plan over HTTP directly (no CLI binary) — see ADR-007.
- **API Mode** (`-g`): Direct API calls to providers
- **Per-provider transport** (`<provider>@cli` / `<provider>@api`): pins one token to a transport regardless of `-g`; bare tokens follow the global mode — see ADR-012.

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
  tui/             # Terminal progress display (Charm bubbletea; silent under --json/-q)
  providers/       # Provider implementations
    provider.go    # Provider interface
    registry.go    # Provider registration and lookup (holds BOTH sets, picks per token)
    transport.go   # <provider>[@cli|@api] token grammar, TransportOf (ADR-012)
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

### Per-Provider Transport (`@cli` / `@api`)

`ParseProviderToken` in `transport.go` is the ONLY place the suffix is parsed. The registry holds both provider sets and picks one per token; the resolved provider reports the bare name and carries its transport (`TransportOf`, recoverable through the `Unwrap` chain). The orchestrator copies it onto `Response.Transport`, the judge onto `Verdict.JudgeTransport`, and everything that used to branch on a global "API mode" (pricing in `internal/output/cost.go`, the cache key mode in `internal/cache`, `warnSubscriptionIdle`) reads that instead. Contract and rationale: ADR-012; the trap is Gotcha 12.

### Slash Routing (OpenRouter)

In API mode any `vendor/model` token is built on the fly as an `OpenRouterAPIProvider` (name = model = token). No plain `openrouter` provider, never in `AllAPIProviders`, rejected in CLI mode. Contract and rationale: `docs/OPENROUTER.md`, ADR-010; the trap list is Gotcha 10.

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

Transport for a bare token (ADR-012): `@cli`/`@api` suffix > `transports:` map in config (or `CONCLAVE_<PROVIDER>_TRANSPORT`) > `-g`/`-c`. The registry validates the value; anything but `cli`/`api` is an error naming the key.

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
| `internal/providers/registry.go` | Provider lookup (dual sets, per-token transport), `AnyAvailable()` |
| `internal/providers/transport.go` | `ParseProviderToken`, `BareName`, `TransportOf` — the `@cli`/`@api` grammar (ADR-012) |
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
8. **A prompt never goes on a CLI's command line.** `codex` and `gemini` resolve to npm `.cmd` shims that Go runs via `cmd.exe`, which ends an argument at the first newline, expands `%VAR%` and runs `" & cmd` as a second command; native exes (`claude`, `grok`) still hit the 32,767-char Windows limit and parse a leading `-` as a flag. So codex, gemini (`-p ""`) and claude read STDIN, and grok gets `--prompt-file`. Pinned by `TestCodexReceivesMultiLinePromptIntact`, `TestGeminiCLIPromptNeverTouchesTheCommandLine`, `TestClaudeCLIPromptOnStdinInAFreshEmptyDir`, `TestGrokCLIPromptTravelsInAFile`. Every CLI starts through `newCLICommand` in `provider.go`, which kills the whole process tree on timeout: `exec.CommandContext` kills only `cmd.exe`, whose child then holds the pipes open and `-t` stops bounding the call (`TestCLITimeoutBoundsShimGrandchild`).
9. **Never prompt without a TTY**: `RunInitIfNeeded` bails when stdin is not a terminal. Subprocess callers (praxis grade) cannot answer a prompt; a prompt there is a hang.
10. **Slash tokens are API-only**: `vendor/model` provider tokens route through OpenRouter and exist only under `-g` (pay-as-you-go, no subscriptions, ~5% platform fee). CLI mode errors with "add -g". The catalog never rewrites slash tokens, so a slug OpenRouter does not list warns and is still sent through — except as the **judge**, where a catalog miss is refused before the panel spends anything (`--skip-preflight` overrides). The judge is resolved and preflighted before orchestration for the same reason. Do not add a plain `openrouter` provider or put OpenRouter in `AllAPIProviders` — ADR-010 rejected both.
11. **Response cache** (ADR-011): key = `(mode, provider, model, full prompt incl. `-f`/stdin, system)`, so a changed file is a miss by design; the judge is never cached. Two traps: a hit keeps `status: "success"` and signals only via `cached` (downstream parsers treat any other status as a failed judge; pinned by `TestCachedHitKeepsStatusSuccess`), and every provider decorator must implement `Unwrap() Provider` or `RunPreflight` silently skips that provider's auth check. The key's mode component is the provider's own transport (ADR-012), so `cache.Wrap`'s mode argument is only a fallback for undeclared providers.
12. **The transport suffix never reaches the provider name** (ADR-012): `claude@cli` resolves to a provider whose `Name()` is `claude`. `--json` keys, the progress line, the judge label, `-m` override keys and the pricing catalog all use the bare name; the transport travels separately (`Response.Transport`, `TransportOf`). Do not compare tokens to names (`p.Name() == flagJudge` breaks when the judge is `claude@cli`; use `providers.BareName`), and do not read `flagGeneral` to decide whether a response was billed: since one panel can mix transports, `output.Options.APIMode` no longer exists and any `Response` built outside the orchestrator must set `Transport` or it prices as nothing (pinned by `TestUnknownTransportIsNotPriced`). `glm@api` and `deepseek/x@cli` are errors by design, and so is the same bare name twice in one panel (`claude@cli,claude@api`): `Registry.GetProviders` refuses it because `--json` and the progress display would drop one leg. `withJudge` deduplicates by name AND transport for the same reason in reverse: a CLI panel member must not stand in for an API judge's preflight.
13. **CLIs that promise JSON still print prose on stdout.** claude printed `Client.listTools() called but server does not advertise tools capability - returning empty list` ahead of its envelope (2026-09-13), and `claude auth status` is pretty-printed multi-line JSON. Every reader of claude or gemini stdout (`Query` in both, claude's `Preflight` and `SubscriptionLoggedIn`) therefore *locates* its object with `findJSONObject` in `claude.go` instead of unmarshalling the whole buffer; do not "simplify" any of them back to `json.Unmarshal(output)`. On an API error claude exits 1 but the only readable message is in the envelope's `result`, so `Query` keeps stdout on failure (`cmdOptions.keepStdoutOnErr`) and surfaces it. Pinned by `TestClaudeCLIIgnoresLeadingNoiseBeforeJSON`, `TestGeminiCLIIgnoresStdoutNoiseAroundJSON` and siblings.
14. **claude runs isolated from the caller's cwd and settings** (ADR-013): `--strict-mcp-config --setting-sources "" --no-session-persistence`, in an empty temp directory, so a panel answer is not shaped by whichever repo conclave was invoked from (a bare "hi" once described the caller's worktree and cited its startup hook) nor by the user's own persona (user settings alone added ~52k prompt tokens per query). The empty source list keeps OAuth; never swap this for `--bare`, which disables OAuth and breaks subscription auth (Gotcha 7). Context goes in via `-f`/stdin. Pinned by `TestClaudeCLIRunsIsolatedFromCallerContext`.
