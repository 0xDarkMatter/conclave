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
  progress/        # Terminal progress display
  providers/       # Provider implementations
    provider.go    # Provider interface
    registry.go    # Provider registration and lookup
    gemini.go      # CLI provider
    api_gemini.go  # API provider
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

### Adding a New Provider

1. Create `internal/providers/{name}.go` (CLI) and/or `api_{name}.go` (API)
2. Implement the `Provider` interface
3. Register in `AllCLIProviders()` or `AllAPIProviders()` in `registry.go`
4. Add to `providerSetupInfo` in `cmd/init.go` for interactive setup
5. Update tests in `provider_test.go`
6. Document in `README.md` and `docs/MODEL_REGISTRY.md`

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

### Run Tests

```bash
go test ./...                    # All tests
go test ./internal/providers/... # Provider tests only
go test -v ./internal/config/... # Verbose config tests
```

### Build

```bash
go build -o bin/conclave .
make install  # Builds and installs to ~/.local/bin
```

### Test a Provider

```bash
# Check availability
./bin/conclave --list-providers
./bin/conclave --list-providers -g

# Quick smoke test
./bin/conclave gemini "Say hello" --no-judge
./bin/conclave -g claude "Say hello" --no-judge
```

## Important Files

| File | Purpose |
|------|---------|
| `cmd/root.go` | CLI entry point, flag definitions |
| `internal/providers/registry.go` | Provider lookup, `AnyAvailable()` |
| `internal/providers/api_base.go` | Shared API logic, retry handling, `KeyRotator` + OS-keyring fallback |
| `internal/judge/judge.go` | Verdict synthesis prompt and parsing |
| `internal/config/env.go` | .env file loading/saving |
| `cmd/keyring.go` | `conclave keyring set/list/rm` — manage keys in the OS keyring |
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
