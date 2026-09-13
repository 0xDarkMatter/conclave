# Changelog

All notable changes to Conclave will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Per-provider transport: a token may carry `@cli` or `@api`
  (`gemini@api,openai@cli,claude@cli`) to pin that provider to its CLI or its
  direct API regardless of `-g` / `-c`. Bare tokens are unchanged. Works in the
  provider list, `--judge` and `-m` (`-m openai@cli:gpt-5.6-sol` and
  `-m openai:gpt-5.6-sol` both apply). The provider name stays bare in the
  progress line, the judge label and `--json` keys; `--json` gains
  `responses.<provider>.transport: "cli" | "api"`. Cost fields, the cache
  key's mode, the subscription-idle warning and availability errors now
  follow each provider's actual transport, so a mixed panel prices only its
  API legs. `deepseek/x@cli` and `glm@api` are errors that say why;
  `deepseek/x@api` no longer needs `-g`. Motivated by Praxis billing
  OPENAI_API_KEY and ANTHROPIC_API_KEY on every grade because gemini alone
  needed the API. ADR-012. The same provider may appear once per panel
  (`claude@cli,claude@api` is refused, since outputs key on the bare name),
  and a judge is deduplicated against panel members by name AND transport,
  so `-g gemini,claude@cli --judge claude` still preflights the API judge.
- API-mode warning when a provider is about to be billed by key while its
  CLI holds a subscription login: `note: openai is running in API mode
  (metered key) while codex is logged in on a subscription; drop -g for
  openai to run on the plan.` Same for claude via `claude auth status`.
  Advisory, stderr, silenced by `-q` and `--raw` but not by `--json`, since
  the person running a JSON pipeline is the one spending the key. Motivated
  by Praxis billing OPENAI_API_KEY on every grade while the Pro plan sat idle.
  With per-provider transport the hint is now `write openai@cli to run it on
  the plan in this panel, or drop -g`, and the warning is decided per
  provider, so a `claude@cli` beside `-g` panel members is never warned about.

### Changed

- `output.Options.APIMode` is gone (internal). Pricing is decided per
  response from `Response.Transport`; a response that does not say how it ran
  is not priced. `cache.Wrap`'s mode argument is now a fallback that a
  provider's declared transport overrides.

### Fixed

- `openai` CLI mode dropped every line of the prompt after the first on
  Windows. `codex` on PATH is npm's `codex.cmd` shim, Go launches it through
  `cmd.exe`, and `cmd.exe` stops reading a positional argument at the first
  newline, so any `-f` file, piped stdin or multi-line rubric reached codex as
  its first line only and codex replied "Context loaded. What would you like
  me to work on?". The prompt now goes on stdin (`codex exec` reads it there
  when no positional prompt is given), which also lifts the 32K command-line
  limit. Regression test: `TestCodexReceivesMultiLinePromptIntact`. The
  `gemini` CLI is the same kind of shim and still passes `-p <prompt>`; it is
  unaffected on this machine only because its CLI auth falls back to the API.

## [1.3.0] - 2026-09-08

### Added

- Per-query cost in API mode (`-g` / `-c`): each provider block, the header
  panel and the `Completed in` footer show the dollar figure derived from the
  OpenRouter catalog and the response's token metrics, and `--json` gains
  `responses.<provider>.cost_usd` plus `meta.total_cost_usd` (providers +
  judge). An unpriceable model is omitted rather than shown as `$0.00`, and a
  total ending in `+` means at least one response could not be priced. CLI mode
  shows nothing about dollars because it is subscription-billed. `--raw` and
  `--brief` are unchanged.
- `--budget <usd>` for batch mode (also `CONCLAVE_BATCH_BUDGET`): stops
  dispatching new items once cumulative estimated spend reaches the cap,
  lets in-flight items finish, and exits non-zero with a summary naming the
  cap, the completed count and the skipped count. Undispatched items stay out
  of the checkpoint so `--resume` continues the run. Because cost is measured
  post-hoc, overshoot by up to `--workers` items is expected.
- Opt-in response cache: `--cache[=TTL]` (24h by default) or
  `CONCLAVE_CACHE_TTL=<hours>` reuses an identical provider response instead of
  paying for it twice; `--no-cache` overrides an env-enabled cache. The key is
  a sha256 of the mode, provider, model and the full prompt including file and
  stdin context, so any context change is a miss. A hit is tagged `(cached)` in
  the progress line and provider block, carries `cached: true` in `--json`, and
  costs nothing. Judge synthesis is never cached. Works in CLI and API mode;
  batch mode honours it per item. `conclave cache stats` and
  `conclave cache clear` manage the store. ADR-011.
- `make check`: one gate running `go vet`, `gofmt -l`, `go test` and
  `conclave models --check`, plus a `.github/workflows/check.yml` running the
  same on ubuntu-latest and windows-latest.
- Tests for `internal/batch`, which had none: item parsing (malformed line
  skipped, missing id assigned, duplicate id dropped), worker fan-out,
  rate-limit retry and retry exhaustion, checkpoint resume, cost estimation
  precedence, and the budget stop, plus `checkpoint_test.go` for
  load/append/corrupt-line handling.
- Runtime pricing catalog (`internal/pricing`): conclave caches OpenRouter's
  public models feed under the user cache directory, refreshes it in the
  background once per `CONCLAVE_PRICING_TTL` hours (default 24), and never
  blocks a query on the network once a cache exists. `CONCLAVE_NO_PRICING=1`
  disables it. ADR-009.
- Model drift warning: when a configured model id is not in the catalog,
  conclave prints one stderr line naming the newest listed alternative and
  proceeds. Suppressed under `--json`, `--raw`, `-q`.
- `conclave models [provider] [--check|--refresh|--json|--all]` to inspect
  current ids, context sizes and API prices, and to gate releases
  (`--check` exits 2 when a compiled default is missing, 3 when the catalog is unreachable).
- OpenRouter as an API-mode backend: in `-g` mode any provider token written
  as an OpenRouter slug (`deepseek/deepseek-v4-pro`, `anthropic/claude-opus-5`)
  routes through `openrouter.ai/api/v1/chat/completions`, with the slug as
  both provider name and model id. Works in the provider list and `--judge`;
  `--all` never auto-includes OpenRouter models. Key `OPENROUTER_API_KEY`
  (env, `.env`, or OS keyring); `conclave init` and `keyring list` know it.
  Preflight checks `/auth/key` and reports an exhausted spend limit as "no
  credit". Drift warnings, display names and batch cost estimates resolve
  the slug directly in the pricing catalog. CLI mode rejects slash tokens
  (API-only, pay-as-you-go). ADR-010.
- The judge is now resolved (and preflighted) before the panel runs, so a
  judge that cannot be built fails before any provider is paid for. A
  slash-routed judge that is not in the OpenRouter catalog is refused
  outright (`--skip-preflight` sends it anyway); panel members only warn.
- Provider lists are trimmed (`"a, b"` works); malformed slugs (`/model`,
  `model/`) and a bare `openrouter` token get specific errors; an
  OpenRouter error envelope inside an HTTP 200 surfaces as an error.

### Fixed

- Batch mode no longer discards a result that has already been paid for. A
  worker whose send raced a cancelled context threw the result away, so an item
  that had been queried and billed left no output line, no checkpoint entry and
  no trace it had run. Results are now always handed to the writer.
- An interrupted batch exits non-zero and says how many items were not
  dispatched. It previously exited 0, letting a pipeline read a partial JSONL
  as the complete answer.
- Provider decorators no longer hide a provider's auth check. Embedding the
  `Provider` interface does not promote the optional `Preflighter`, so wrapping
  a provider silently skipped its preflight; every decorator now implements
  `Unwrap` and `RunPreflight` follows the chain.
- The response cache no longer deletes an entry before replacing it, which
  opened a window where a concurrent reader saw nothing, and it no longer leaks
  a temp file when the replacement is refused.
- `--budget` warns when it cannot bind: on a non-batch query, where it does
  nothing, and when the pricing catalog is unavailable, where estimates cover
  only the built-in providers.
- Batch mode no longer checkpoints items that a cancellation stopped from ever
  running, which made `--resume` skip them permanently.
- A batch run now keeps a checkpoint even without `--resume`, so the resume
  hint printed by the budget-stop and interrupt summaries is actually true.
- Error paths in batch mode now carry the spend they incurred, so a failing
  judge model no longer makes `--budget` unenforceable.
- `--json` gains `meta.total_cost_partial`, marking a total that understates
  the real spend because something could not be priced. The styled output
  already showed this as a trailing `+`.
- Runtime failures no longer print the whole usage block after the error,
  which buried the batch summaries. Argument misuse still shows usage.
- `conclave models --check` exits 2 on real drift and 3 when the catalog is
  unreachable, so `make check` and CI branch on the code instead of matching
  message text.
- `--json` now reports the timeout actually in force as
  `execution.timeout_seconds`. The field existed but was never populated, so it
  always read `0` regardless of `-t`.

- `gemini` CLI mode: pass `-p` (gemini-cli 0.58 treats a positional prompt as
  interactive mode and never returns headless) and `--skip-trust` plus
  `GEMINI_CLI_TRUST_WORKSPACE=true` (exit 55 in any un-trusted directory).
  When gemini-cli still fails on auth (Google retired the free Code Assist
  OAuth tier it defaults to) and a `GEMINI_API_KEY` is present, the query
  falls back to the direct Gemini API with the same model. Set
  `security.auth.selectedType` to `gemini-api-key` in `~/.gemini/settings.json`
  to keep the CLI route.
- Preflight budget raised 2s → 5s; claude/codex cold starts on Windows were
  tripping it. `codex login status` prints to stderr, which the old check
  never read.
- Auto-`init` no longer runs when stdin is not a terminal. A subprocess with
  no provider keys visible used to block forever on an invisible prompt and
  look like a 110s+ hang.
- `openai` CLI-mode preflight asks `codex login status` before demanding
  `OPENAI_API_KEY`, so ChatGPT-subscription users are no longer rejected.
  Remediation text for gemini/openai now says which mode needs what.

### Changed

- Default models bumped (all verified live 2026-09-08): openai
  `gpt-5.5` → `gpt-5.6-sol`, claude `claude-opus-4-8` → `claude-opus-5`,
  grok `grok-4-1-fast-reasoning` → `grok-4.6` (the only id the grok CLI
  offers), glm `glm-5.2` → `glm-5.3`. Cheap models: grok → `grok-build-0.1`,
  glm → `glm-5.3-flash`. `conclave models --check` now passes clean.
- Batch-mode cost estimates now use live per-model prices from the catalog;
  the hardcoded table in `internal/batch/processor.go` is demoted to an
  offline fallback (and its gpt-5-nano input price corrected 0.10 → 0.05).
- `docs/MODEL_REGISTRY.md` refreshed against the 2026-09-08 feed: GPT-5.6
  Sol/Terra/Luna, Claude Fable 5 / 5.1, Opus 5, Sonnet 5, Gemini 3.5–3.8
  Flash, Grok 4.20–4.6 and Build 0.1, GLM 5.3 / 5.3 Flash. Notes that all
  prices are API-mode only. Adds a Drift Watch section: `grok-4-1-fast-*`
  and `glm-4.6v-flashx` are no longer listed on OpenRouter.
- Line endings pinned to LF via `.gitattributes` for `.go`, `.md`, `.yml` and
  the Makefile, so gofmt agrees on Windows and Linux. A fresh worktree on an
  `autocrlf` machine can still show CRLF until re-checked out; see
  `docs/CHECK_GATE.md` landmines.
- `make check` enumerates Go files via `go list` instead of `gofmt -l .`, which
  recursed into nested `.claude/worktrees/*` checkouts and failed on other
  sessions' files.
- Makefile `VERSION` now tracks the release (it reported 1.1.0 for 1.2.0 code).

## [1.2.0] - 2026-06-18

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

[1.3.0]: https://github.com/0xDarkMatter/conclave/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/0xDarkMatter/conclave/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/0xDarkMatter/conclave/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/0xDarkMatter/conclave/releases/tag/v1.0.0
