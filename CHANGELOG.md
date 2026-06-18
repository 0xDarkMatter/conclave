# Changelog

All notable changes to Conclave will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- OS keyring fallback for API keys: when a provider's `*_API_KEY` env var is
  unset, conclave reads it from the OS keyring (Windows Credential Manager /
  macOS Keychain / Linux Secret Service) via `zalando/go-keyring`. Resolution
  order: env → `~/.config/conclave/.env` → `./.env` → keyring.
- `conclave keyring set|list|rm <ENV_VAR>` to manage keys in the OS keyring.
- `docs/adr/` — Architecture Decision Records (ADR-001…008) capturing the
  foundational design and this release's changes.

### Changed

- GLM no longer requires the `opencode` CLI: CLI-mode `glm` now calls the Z.ai
  GLM Coding Plan over direct HTTP (`api.z.ai/api/coding/paas/v4`,
  OpenAI-compatible), keyed on `GLM_API_KEY`/`ZAI_API_KEY`, model id `glm-5.2`.
- Refreshed stale provider default models to current ids:
  - openai `gpt-5.2` → `gpt-5.5`
  - gemini `gemini-3-pro-preview` (shut down) → `gemini-3.1-pro-preview`
  - claude `claude-opus-4-5-20251101` → `claude-opus-4-8`
  - glm `glm-4.7` → `glm-5.2`
  - grok CLI default `grok-code-fast-1` (retires 2026-08-15) →
    `grok-4-1-fast-reasoning`

## [1.1.0] - 2026-05-22

Production hardening release. Six bug fixes for issues hit running Conclave
heavily against gpt-5.x reasoning models, plus quality-of-life additions:
styled output, preflight auth, and a new `--raw` mode for clean piping.

### Added

- `--raw` output mode: sentinel-separated provider blocks for downstream
  parsers. Implies `--no-judge`, mutually exclusive with `--json`.
- gpt-5.x reasoning model support: automatic `max_completion_tokens`
  (default 16000) for `gpt-5*`, `o1*`, `o3*` families. Override via
  `CONCLAVE_OPENAI_MAX_COMPLETION_TOKENS`.
- Preflight auth checks: fast pre-query validation that providers have
  valid credentials. Bypass with `--skip-preflight`.
- Styled output with Lipgloss: header panel with metadata, adaptive
  colors for light/dark terminals, cool-tones palette.
- `--list-providers` (default mode): now shows both CLI and API columns
  side-by-side so divergences (glm CLI-only, different grok defaults)
  are visible at a glance.
- LICENSE file (MIT) — the README badge promised it; now it's actually there.
- This CHANGELOG.

### Fixed

- gpt-5.x reasoning models no longer fail or return empty responses.
  OpenAI rejects `max_tokens` for these models and they need an explicit
  completion budget.
- Judge `PARSE_ERROR` no longer swallows provider outputs. When synthesis
  fails, the raw per-provider responses are surfaced so the work isn't lost.
- API errors now include HTTP status code + provider's error `code`/`param`
  fields. The actionable detail on a 400 (e.g. `unsupported_parameter`,
  `param: max_tokens`) is no longer truncated.
- Transport-level failures (DNS, TLS, connection timeout) tagged distinctly
  from API-level failures so the user can tell apart "the server said no"
  from "I never reached the server".
- When ALL providers fail, the styled output still renders so each
  provider's full error is visible. Previously only the truncated spinner
  line was shown.
- Long error strings wrap instead of truncating in the styled error block.
- Client HTTP timeout raised 120s → 300s as a safety ceiling for reasoning
  models. The per-request `--timeout` (default 60s) still governs.

### Changed

- `--retries` flag documentation clarified as batch-only. Single-call
  queries already retry 429/5xx automatically via internal exponential
  backoff; the flag never applied to them and the help text now says so.

### Internal

- 16 new unit + httptest integration tests across providers, output, cmd.
- `TestSaveEnvFile` fixed for Windows (POSIX permission check skipped
  on `runtime.GOOS == "windows"`).
- Full suite: 86 tests, all green, `go vet` clean.

## [1.0.0] - 2026-01-08

Initial public release.

### Added

- Multi-provider parallel querying across Gemini, OpenAI, Anthropic, xAI
  Grok, Perplexity, and Zhipu GLM.
- CLI mode wrapping provider-specific CLIs (`gemini`, `claude`, `codex`,
  `grok`, `perplexity`, `opencode`).
- API mode (`-g` / `--general`) using raw HTTP for non-coding queries.
- Cheap mode (`-c`) for cost-effective batch processing.
- Judge synthesis with verdict, confidence, agreements, disagreements,
  and recommendations.
- Batch mode (`--batch`) with parallel workers, rate limiting, resume
  capability, and JSONL input/output.
- Charm Bubble Tea TUI with animated spinners and real-time progress.
- Blind mode for unbiased judging.
- Interactive setup (`conclave init`) for API key configuration.

[1.1.0]: https://github.com/0xDarkMatter/conclave/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/0xDarkMatter/conclave/releases/tag/v1.0.0
