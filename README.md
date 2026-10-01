# Conclave

[![Release](https://img.shields.io/github/v/release/0xDarkMatter/conclave?style=flat&labelColor=2d3142&color=eb6c36)](https://github.com/0xDarkMatter/conclave/releases)
[![License](https://img.shields.io/badge/license-MIT-4f5d75?style=flat&labelColor=2d3142)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat&labelColor=2d3142&logo=go)](go.mod)
[![Built with Charm](https://img.shields.io/badge/built%20with-Charm-ff69b4?style=flat&labelColor=2d3142)](https://charm.sh)

> Stop juggling six AI CLIs. Query any model with one syntax, or convene the whole council and let a judge synthesize the verdict.

Tired of memorizing whether it's `--file` or `-f` or piping to stdin? Sick of context-switching between `gemini`, `claude`, `codex`, and whatever CLI Grok ships this week? **Conclave is your universal remote for LLMs** - one command, one syntax, any provider. Six direct providers, plus any model on OpenRouter by its `vendor/model` slug. Learn it once, query everything.

But here's where it gets interesting: why trust a single AI's opinion when you can convene an entire council? Conclave queries multiple models in parallel, then hands their responses to a judge who synthesizes a verdict with confidence levels, agreements, disagreements, and actionable recommendations. It's like having a room full of very expensive consultants who actually have to reach consensus before billing you.

Built with [Charm](https://charm.sh)'s Bubble Tea for a terminal UI that doesn't look like it crawled out of 1985. Animated spinners, real-time progress, token counts - because if you're going to burn API credits, you should at least enjoy watching the meter spin.

## Why Conclave?

- **One interface** - Same syntax for Gemini, Claude, GPT, Grok, Perplexity, GLM, and any `vendor/model` on OpenRouter
- **Reduce bias** - No single model's quirks dominate the response
- **Catch blind spots** - Disagreements are surfaced, not averaged away; different models notice different issues
- **Faster iteration** - Parallel queries, one synthesized answer, and an opt-in cache so re-runs are free
- **Use the plans you already pay for** - Pin each provider to its subscription CLI or its metered API, even within one panel (`gemini@api,openai@cli,claude@cli`)
- **Know what it cost** - API legs print the real dollar figure per response from a daily-refreshed price catalog, which also warns when a configured model id has vanished
- **Beautiful TUI** - Animated progress with [Charm](https://charm.sh) (Bubble Tea)

## How It Works

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/diagrams/how-it-works-dark.svg">
  <img alt="Architecture: a prompt with context goes to the orchestrator, which fans out in parallel to Gemini, OpenAI and Claude; their responses go to a judge that synthesizes one verdict" src="docs/diagrams/how-it-works.svg" width="100%">
</picture>

1. **Query Phase** - Prompt sent to all providers in parallel, each under its own timeout
2. **Judge Phase** - Designated LLM synthesizes the responses (skipped for a single provider)
3. **Output Phase** - Formatted result with confidence and reasoning, priced in API mode

### Multi-model judging

The judge does not average the panel. It sorts what the models said into what they agree
on and what they contest, turns the consensus into a reasoned verdict, and surfaces the
contested material as disagreements and blind spots rather than discarding it. `--blind`
hides which model said what so the sorting cannot favour a brand.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/diagrams/judging-flow-dark.svg">
  <img alt="Sankey: token shares from Gemini, OpenAI and Claude flow into consensus and contested pools; consensus becomes the verdict, contested content is surfaced as disagreements and blind spots, and a small remainder is dropped" src="docs/diagrams/judging-flow.svg" width="100%">
</picture>

Diagram sources live in [`docs/diagrams/src/`](docs/diagrams/src/); `python docs/diagrams/export.py` regenerates the light and dark SVGs.

## Recent Updates

### v1.5.0 — 2026-10-03

**🎯 Decision models: `conclave decide`**

Typed questions in, typed answers with probabilities out. `conclave decide clef,jev -f ticket.txt --questions triage.yaml` runs Cloudflare Clef and Typesafe Jev in parallel and averages their probabilities per question, with no LLM judge in the loop: each question gets a consensus answer, an agreement score and a `contested` flag when the models disagree. See [Decision models](#decision-models-conclave-decide).

**🧭 Any decision model on OpenRouter**

A `vendor/model` token joins the panel through OpenRouter's Decisions API, priced from the cost OpenRouter reports: `conclave decide jev,liquid/d1,upstage/solar-decide,inception/mercury-decide:free ...`. Jev itself runs on an OpenRouter key alone when no Typesafe key is set.

**📈 Price-performance frontiers: `conclave models --frontier`**

Which models are worth their price? `conclave models --frontier` draws the Pareto frontier of quality against cost from external sources only: Artificial Analysis indices via OpenRouter for chat models, and the community Decision Index (recomputed from upstream, edition-pinned) for decision models. Add `--html report.html` for a self-contained offline report. Conclave runs no evaluations of its own; a model nobody has scored is listed as unscored, never estimated. See [Price-performance frontiers](#price-performance-frontiers).

**🛠️ Readable OpenRouter and Perplexity errors**

Error responses whose `code` is a number (OpenRouter, Perplexity) used to surface as raw JSON; they now read `HTTP 401: User not found. [code: 401]`.

**⚠️ Known issue (unchanged):** the Perplexity Agent API migration is still held pending a live key. See [CHANGELOG](CHANGELOG.md#140---2026-10-01).

---

### v1.4.0 — 2026-10-01

**🔌 Subscriptions and API keys in one panel**

Any provider token can pin its own transport: `conclave gemini@api,openai@cli,claude@cli "..."` runs gemini on a metered key beside openai and claude on their ChatGPT and Claude Max subscriptions. A standing choice goes in `config.yaml` under `transports:`. Only the API legs are priced, and `--json` says which leg ran where.

**🚀 GPT-6.1 Sol and Claude Opus 5.5 by default**

Defaults move to `gpt-6.1-sol` and `claude-opus-5-5` (20% cheaper on the API), with `gpt-6-luna` and `claude-sonnet-5-5` in cheap mode, since Haiku 4.5 is retiring. CLI mode now needs codex 0.159.1 or newer, which bundles the GPT-6 models.

**🛡️ A hardening pass**

Thirty-seven fix commits, led by Windows: prompts no longer travel on the command line, where `cmd.exe` cut multi-line prompts to their first line, and `-t` now bounds npm-shim CLIs. A failed or unparseable judge exits 1, Ctrl-C cancels cleanly with exit 130, batch `--resume` retries failures, and an out-of-credit key fails at once instead of retrying.

**🎯 Panel answers no longer shaped by your repo**

claude panel queries run in an empty directory with no MCP servers or settings, so an answer depends on the prompt and its `-f` context, not on whichever project you ran conclave from.

**⚠️ Known issue:** Perplexity ended Sonar chat-completions support on 2026-09-27. The provider most likely still works (Perplexity is reformulating the old calls onto its Agent API), but this is unverified; a migration is held for a later release. See [CHANGELOG](CHANGELOG.md#140---2026-10-01).

---

### v1.3.0 — 2026-09-08

**🚀 Claude Opus 5 and GPT-5.6 Sol by default**

Defaults moved to the current flagships: claude `claude-opus-5`, openai `gpt-5.6-sol`, grok `grok-4.6`, glm `glm-5.3`, with `grok-build-0.1` and `glm-5.3-flash` in cheap mode. Every id was verified live on both the CLI and API routes before it shipped. Override any of them with `-m provider:model`.

**🌐 Any model via OpenRouter**

In API mode, write a provider as its OpenRouter slug and it just works: `conclave -g deepseek/deepseek-v4-pro,anthropic/claude-opus-5 "..." --judge openai/gpt-5.6-sol`. Slugs can sit beside direct providers on one panel or act as judge. Needs `OPENROUTER_API_KEY`; details in [docs/OPENROUTER.md](docs/OPENROUTER.md).

**💸 Costs you can see**

API-mode runs now print the dollar figure per response and in total, from a cached [OpenRouter](https://openrouter.ai/models) price catalog that refreshes once a day in the background. `--json` gains `cost_usd` and `meta.total_cost_usd`. `conclave models` prints current ids and prices; `conclave models --check` tells you if a compiled default has been retired.

**🗂 Opt-in response cache and a batch budget**

`--cache` reuses an identical provider response instead of paying for it twice; `--budget 5.00` stops a batch run once estimated spend hits the cap and leaves it resumable. Judge synthesis is never cached.

**🛠 gemini, codex and the hang that wasn't**

Google retired gemini-cli's free OAuth tier, so CLI-mode gemini now needs `GEMINI_API_KEY` and falls back to the direct API when the CLI's auth fails. codex is checked with `codex login status` instead of demanding an API key. And Conclave no longer opens an interactive setup prompt when stdin is not a terminal, which used to look like a 110-second hang from a script.

---

Older releases (v1.2.0 keyring and GLM-5.2, v1.1.0 `--raw` and preflight, v1.0.0 initial
release): see the [CHANGELOG](CHANGELOG.md).

## Terminal UI

Conclave features a rich terminal interface powered by [Bubble Tea](https://github.com/charmbracelet/bubbletea):

```
▸ Querying 3 providers...
  ├── ⠹ Google Gemini 3.1 Pro [02.34s]
  ├── ✓ OpenAI GPT-5.6 Sol [01.91s / 000168 tokens]
  └── ⠼ Anthropic Claude Opus 5 [03.12s]

▸ Crystallizing... ⠋ [02.45s]
```

- **Animated spinners** - Braille animation for active providers
- **Real-time progress** - Token counts and timing as providers complete
- **Synthesis verbs** - 25 rotating verbs during verdict synthesis
- **Non-TTY fallback** - Clean output for CI/CD and piped commands
- **Cost on the line** - In API mode each provider block and the footer carry the real spend

## One CLI, Every LLM

Beyond consensus, Conclave serves as a **unified interface for any LLM**. Instead of learning six different CLI tools with different syntaxes, flags, and quirks - use one:

```bash
# Same syntax, any provider
conclave gemini "Explain this error" -f error.log
conclave claude "Review this PR" -f diff.txt
conclave openai "Generate test cases" -f api.go
conclave grok "What does this regex do?" -f patterns.txt
```

**Why use Conclave for single-provider queries?**

| Benefit | Without Conclave | With Conclave |
|---------|-----------------|---------------|
| Syntax | Learn each CLI's flags | One consistent syntax |
| Files | Different `-f`/`--file`/stdin handling | Always `-f` |
| Setup | Configure each tool separately | `conclave init` once |
| Switching | Remember which tool for which task | Just change the provider name |
| Models | Different `--model` formats | Always `-m provider:model` |

```bash
# Quick single-provider queries (no judge needed)
conclave gemini "What's the time complexity of this?" -f algo.py
conclave perplexity "Latest news on Rust 2.0"
conclave -g claude "Summarize this paper" -f paper.pdf

# Switch models on the fly
conclave gemini "Explain" -m gemini:gemini-3.8-flash      # Fast
conclave gemini "Explain" -m gemini:gemini-3.1-pro-preview # Thorough
```

When you query a single provider, Conclave skips the judge phase and returns the response directly - it's just a cleaner interface to the underlying LLM.

## Installation

```bash
git clone https://github.com/0xDarkMatter/conclave
cd conclave
make install  # installs to ~/.local/bin
```

## Requirements

Conclave operates in two modes with different requirements:

### API Mode (`-g`) - Recommended for Most Users

**Only requires API keys** - no additional CLI tools needed.

```bash
conclave init                    # Set up API keys
conclave -g gemini,claude "..."  # Works immediately
```

### CLI Mode (Default)

Uses provider-specific CLI tools optimized for coding tasks. Each provider requires its CLI installed:

| Provider | CLI Tool | Installation |
|----------|----------|--------------|
| **gemini** | `gemini` | `npm install -g @google/gemini-cli`, plus `GEMINI_API_KEY` (Google retired the CLI's free OAuth tier; Conclave falls back to the API if the CLI's auth fails) |
| **claude** | `claude` | `npm install -g @anthropic-ai/claude-code`, then `claude auth login` (Max subscription, no API key) |
| **openai** | `codex` | `npm install -g @openai/codex`, then `codex login` (ChatGPT subscription, no API key). Needs codex 0.159.1 or newer: codex bundles its model list, and older builds reject the default `gpt-6.1-sol` |
| **grok** | `grok` | See [xAI Grok CLI](https://github.com/xai-org/grok-cli) |
| **perplexity** | `perplexity` | See [Perplexity CLI](https://github.com/perplexity-ai/perplexity-cli) |
| **glm** | _(none — direct API)_ | Set `GLM_API_KEY` (GLM Coding Plan key from [z.ai](https://z.ai/manage-apikey/apikey-list)) |

**Check what's available:**

```bash
conclave --list-providers      # CLI mode - shows installed CLIs
conclave --list-providers -g   # API mode - shows configured API keys
```

**Tip:** Start with API mode (`-g`) to get running quickly. Add CLI tools later if you want their coding-specific optimizations, or to run on subscriptions instead of metered keys. You can mix the two per provider in one call: `gemini@api,openai@cli,claude@cli` (see [Modes](#modes)).

## Quick Start

```bash
# First run - interactive setup for API keys
conclave init

# Query multiple providers
conclave gemini,openai,claude "Is this code secure?" -f auth.go --judge claude

# Use all available providers
conclave --all "Review this architecture" -f design.md --judge claude
```

## Modes

Each provider token is routed by three questions: does it carry a `vendor/model` slug, does it
carry a `@cli` / `@api` suffix, and (only if it does not) is `-g` set?

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/diagrams/provider-routing-dark.svg">
  <img alt="Flowchart: a slash token goes to OpenRouter in API mode or is rejected in CLI mode; a plain name goes to the direct API in -g mode or to the wrapped CLI, with the gemini CLI falling back to the API on an auth failure" src="docs/diagrams/provider-routing.svg" width="100%">
</picture>

### CLI Mode (Default)

Uses coding-focused CLI tools (`gemini`, `claude`, `codex`, etc.). Best for code review and technical queries.

```bash
conclave gemini,claude "Explain this function" -f utils.go
```

### General Mode (`-g`)

Uses raw APIs without coding restrictions. Best for general-purpose queries, research, and non-technical topics.

```bash
conclave -g gemini,openai,claude "What are the implications of quantum computing for cryptography?" --judge claude
```

### Cheap Mode (`-c`)

Uses smaller, faster models for cost-effective batch processing and pipelines. Implies `-g` (API mode).

```bash
# ~10x cheaper per query
conclave -c gemini,claude "Classify as spam/ham" -f message.txt --json

# Batch processing with all providers
conclave -c --all "Summarize" -f doc.md --brief
```

Each provider's cheap model is in the [Providers](#providers) table.


### Mixed transports (`<provider>@cli` / `<provider>@api`)

A token can pin its own transport. The suffix overrides `-g` / `-c` for that provider only;
bare tokens keep following the global mode, so every existing command line means what it
meant. This is how one panel runs gemini on a metered key (its CLI lost its free tier) beside
openai and claude on their ChatGPT Pro and Claude Max subscriptions:

```bash
# gemini on the API, openai and claude on their subscription CLIs, one panel, one JSON
conclave gemini@api,openai@cli,claude@cli "Grade this answer" --no-judge --json

# The judge and -m take the same grammar
conclave -g gemini,openai --judge claude@cli -m openai@cli:gpt-5.6-sol "Is this secure?"
```

Rules:

- **The provider's name stays bare everywhere**: progress line, judge label, and `--json`
  keys (`responses.openai`, never `responses["openai@cli"]`). `--json` gains
  `responses.<provider>.transport: "cli" | "api"` so a consumer can tell which leg ran where.
- **Dollars follow the transport, per response.** A `@cli` leg is subscription-billed and shows
  no cost field; it does not turn the total into a floor. Only API legs are priced.
- **`-m` applies by provider, not by spelling**: `-m openai:x` and `-m openai@cli:x` both set
  openai's model whichever transport it runs on.
- **`-c` implies API for bare tokens**, as before. A `@cli` provider under `-c` uses its
  normal CLI default model, because the cheap models are API ids the CLI wrappers cannot use.
- **`vendor/model` slugs are API-only** ([OpenRouter](docs/OPENROUTER.md)): `deepseek/x@cli`
  is an error that says why; `deepseek/x@api` works without `-g`.
- **`glm@api` is refused** (API mode is disabled for glm, ADR-006); use `glm` or `glm@cli`.
- **`--all` is unchanged** and takes its list from the global mode.
- **A standing choice goes in the config file.** `transports: {gemini: api, claude: cli}` in
  `config.yaml` (or `CONCLAVE_CLAUDE_TRANSPORT=cli`) pins bare tokens without retyping the
  suffix. Precedence is suffix, then config, then `-g` / `-c`. A value other than `cli` or `api`
  fails the run naming the key. Because a config pin is the one input that can move billing
  without appearing in the command line, the run prints a `note:` whenever a pin actually
  redirected a bare token, and how to override it for that run. Scripts that must be portable
  across machines should spell the suffix rather than rely on a pin.
- **Mixed panels say so in the styled view.** When a panel runs on more than one transport,
  each provider block is tagged `via cli` or `via api`. Single-transport runs are unchanged.
- **One provider, one seat.** `claude@cli,claude@api` is refused: outputs are keyed by the
  bare name, so the two legs would overwrite each other. Run them as two panels if you need both.
- **The judge is checked on its own transport.** With `-g gemini,claude@cli --judge claude`
  the judge is the API claude, a different credential from the panel's CLI claude, and is
  preflighted separately.

The API-mode warning about an idle subscription now suggests the suffix: `write openai@cli to
run it on the plan in this panel`. Decision record: [ADR-012](docs/adr/ADR-012-per-provider-transport-suffix.md).

### Batch Mode (`--batch`)

Process thousands of items with parallel workers, rate limiting, and resume capability. Built in Go for performant concurrent execution - scales to 200 parallel workers with minimal overhead. Uses cheap mode by default.

```bash
# Process a JSONL file with a single provider
conclave -c grok "Classify this account" --batch items.jsonl -o results.jsonl

# Parallel workers for faster throughput
conclave -c gemini "Analyze" --batch items.jsonl --workers 50 -o results.jsonl

# Cap the spend; the run exits non-zero and --resume continues it
conclave --all --batch items.jsonl -o out.jsonl --budget 5.00 --resume

# Resume an interrupted job
conclave -c claude "Analyze" --batch items.jsonl -o results.jsonl --resume
```

`--resume` skips items that succeeded and retries everything else, including items
that failed. It appends to the output, so a retried item has its old error line and
a new line: take the last line per `id`.

**Input format (JSONL):**
```jsonl
{"id": "1", "context": "Username: @acme_corp\nBio: Enterprise solutions...\nFollowers: 50K\n\nRecent posts:\n..."}
{"id": "2", "context": "Username: @jane_dev\nBio: Software engineer, coffee lover\nFollowers: 2K\n\nRecent posts:\n..."}
```

**Performance (99 items, 50 workers, measured December 2025 on the cheap models of the time; costs have moved since, see the caveat in [docs/BATCH_BENCHMARKS.md](docs/BATCH_BENCHMARKS.md)):**

| Provider | Time | Cost | Best For |
|----------|------|------|----------|
| **Grok** | 23s | $0.05 | Speed & cost efficiency |
| Gemini | 33s | $0.28 | Budget with decent quality |
| Claude | 39s | $0.65 | Accuracy, depth, nuanced analysis |
| OpenAI | 88s | $0.18 | Reliable fallback |

**Note:** Complex prompts slow throughput by 1.4-2.3x. Claude produces the most comprehensive analysis but at higher cost.

See [docs/BATCH_MODE.md](docs/BATCH_MODE.md) for full documentation and [docs/BATCH_BENCHMARKS.md](docs/BATCH_BENCHMARKS.md) for detailed performance benchmarks.

## Providers

**Chat models**, queried as a panel and judged (or one at a time with no judge):

| Provider | CLI mode | API mode (`-g`) | Default model | Cheap model (`-c`) | Key |
|---|---|---|---|---|---|
| gemini | `gemini` CLI | Gemini API | `gemini-3.1-pro-preview` | `gemini-3-flash-preview` | `GEMINI_API_KEY` (needed in both modes) |
| openai | `codex` CLI (ChatGPT plan) | OpenAI API | `gpt-6.1-sol` | `gpt-6-luna` | `OPENAI_API_KEY` (API only) |
| claude | `claude` CLI (Claude Max) | Anthropic API | `claude-opus-5-5` | `claude-sonnet-5-5` | `ANTHROPIC_API_KEY` (API only) |
| grok | `grok` CLI | xAI API | `grok-4.7` | `grok-build-0.1` | `XAI_API_KEY` |
| perplexity | `perplexity` CLI | Perplexity API | `sonar-pro` | `sonar` | `PERPLEXITY_API_KEY` |
| glm | Coding Plan API (direct HTTP) | disabled ([ADR-006](docs/adr/ADR-006-glm-api-mode-disabled-for-latency.md)) | `glm-5.3` | `glm-5.3-flash` | `GLM_API_KEY` / `ZAI_API_KEY` |
| `vendor/model` | - | [OpenRouter](#openrouter-any-model) | the slug itself | - | `OPENROUTER_API_KEY` |

Default models are the same on both transports. Override one with `-m provider:model`
(`-m openai:gpt-6-luna`), or permanently in [`config.yaml`](#config-file) or with
`CONCLAVE_<PROVIDER>_MODEL`. `conclave models --check` tells you when a default has been
retired.

**Decision models**, asked typed questions with `conclave decide` (see [below](#decision-models-conclave-decide)):

| Decider | Runs on | Default model | Price | Key |
|---|---|---|---|---|
| jev | TypeSafe API, or OpenRouter's Decisions API | `jev-latest` | $0.042 / M input | `TYPESAFE_API_KEY`, or `OPENROUTER_API_KEY` |
| clef | Cloudflare Workers AI | `clef` | $0.24 / M input | `CLOUDFLARE_API_TOKEN` + `CLOUDFLARE_ACCOUNT_ID` |
| clef-flash | Cloudflare Workers AI | `clef-flash` | $0.09 / M input | `CLOUDFLARE_API_TOKEN` + `CLOUDFLARE_ACCOUNT_ID` |
| `vendor/model` | OpenRouter's Decisions API | the slug itself | as OpenRouter reports | `OPENROUTER_API_KEY` |

Output tokens are free for decision models. [docs/MODEL_REGISTRY.md](docs/MODEL_REGISTRY.md)
is the full reference for every id, price and verification date.

### OpenRouter (any model)

In API mode, any provider token written as an OpenRouter slug (`vendor/model`) is sent
through [OpenRouter](https://openrouter.ai/models). The slug is both the provider name and
the model id, so several OpenRouter models can sit on one panel or act as judge:

```bash
conclave -g deepseek/deepseek-v4-pro,anthropic/claude-opus-5 "Compare these" --judge openai/gpt-5.6-sol
conclave -g google/gemini-3.8-flash,claude "Summarise" --judge claude   # mix with direct providers
```

Set `OPENROUTER_API_KEY` (env, `.env`, or `conclave keyring set OPENROUTER_API_KEY`).
`conclave models` prints current slugs and prices; `--list-providers -g` shows whether the key
is configured. `--all` never auto-includes OpenRouter models. A slug the catalog does not list
still runs (with a warning) as a panel member, but is refused as the judge so a typo cannot cost
you the whole panel; `--skip-preflight` overrides that.

- **API mode only, pay-as-you-go.** OpenRouter cannot use subscriptions (Claude Max, Codex,
  GLM Coding Plan), and it adds a platform fee of about 5% over the vendor's list price. In CLI
  mode a slash token is rejected with a hint to add `-g`.
- **Direct providers remain better for gemini, openai and claude**: no fee, provider-specific
  request fields, and subscription billing in CLI mode. Use OpenRouter for models conclave has
  no direct provider for. Full guide (setup, finding slugs, costs, the judge rule, error decoder):
  [docs/OPENROUTER.md](docs/OPENROUTER.md); rationale in [ADR-010](docs/adr/ADR-010-openrouter-as-a-slash-routed-api-backend.md).

## Decision models (`conclave decide`)

### What are System One models?

Most of Conclave talks to LLMs: you send a prompt, they write text, and something has to
read that text to find the answer. That is slow (seconds), costs output tokens, and the
"confidence" an LLM states is just more text it chose to write.

**System One models** are a different class, introduced by TypeSafe with **Jev**. The name
echoes Kahneman's System 1 (fast, intuitive judgment) as opposed to System 2 (slow,
deliberate reasoning, which is the LLM's job). Instead of a prompt, you give one a *state*
(a ticket, a log line, a JSON record) and a set of *typed questions*. Instead of text, it
returns a typed value per question with a probability distribution attached, which your
code can branch on directly:

| | LLM (chat model) | System One model |
|---|---|---|
| Input | a prompt | a state plus typed questions |
| Output | free text you must parse | a typed answer per question: yes/no, one of N labels, or a point on a scale |
| Confidence | whatever the model says it is | a probability distribution over the possible answers |
| Speed | seconds | tens to hundreds of milliseconds |
| Cost | input and output tokens | input tokens only (output is free) |
| Good for | explaining, writing, open-ended reasoning | routing, triage, classification, gating an agent's next action |

Each question is answered independently and in parallel, so one call can ask 64 things about
the same state. The answers cannot drift out of format, because there is no free text to
drift. They will not explain *why*, though: when you need reasoning, ask an LLM.

The models Conclave can use:

- **Jev** (TypeSafe): the first System One model. 32k context, $0.042 per million input
  tokens. Reached directly with a TypeSafe key, or through OpenRouter.
- **Clef** and **Clef-flash** (Cloudflare): open-weight (Apache-2.0) models that speak Jev's
  API, hosted on Workers AI. 64k context. Clef-flash trades a little quality for speed
  (around 40 ms median).
- **Any OpenRouter decision model**: Liquid AI, Upstage, Inception, Together and others publish
  System One models there; use them as `vendor/model`.

Because they return comparable probabilities, a panel of decision models needs no judge:
Conclave averages the distributions and tells you where the models disagree. To compare them
on quality and price, see [Price-performance frontiers](#price-performance-frontiers).

**Read more:**
[Introducing System One models and Jev](https://typesafe.ai/blog/introducing-system-one-models-and-jev) (TypeSafe) ·
[System One concepts](https://docs.typesafe.ai/concepts/system-one),
[question types](https://docs.typesafe.ai/primitives) and
[reading confidence](https://docs.typesafe.ai/confidence) (TypeSafe docs) ·
[Clef and Clef-flash](https://blog.cloudflare.com/clef-decision-models/) (Cloudflare) ·
[Using Jev on OpenRouter](https://openrouter.ai/docs/guides/community/jev) ·
[Decision Index leaderboard](https://huggingface.co/spaces/multimodalart/jev-decision-index) (community) ·
[ADR-016](docs/adr/ADR-016-decision-models-are-a-separate-provider-class.md) (how Conclave models them)

### Using `conclave decide`

Every question has one of three types:

| Type | Asks | Answer |
|---|---|---|
| `noul` | a 0-1 value ("is this urgent?") | `noul: 0.82` |
| `choice` | one of named options | `choice`, plus a probability per option |
| `score` | a point on an ordered scale (index = score) | `score`, plus a probability per index |

`conclave decide` runs a panel of decision models in parallel and combines the answers per question
by **averaging the probabilities with equal weight**, not by an LLM judge. Each question
reports the consensus answer, `agreement` (1 minus the Jensen-Shannon divergence between
the deciders, 1.0 = identical) and `contested` when the deciders' own picks differ.

```bash
# Every configured decider, a question file, the state from a file
conclave decide -f ticket.txt --questions triage.yaml

# Named deciders, JSON envelope (deciders / consensus / meta)
conclave decide clef,jev -f ticket.txt --questions triage.yaml --json

# One-question shorthand: a single noul question with id "q"
conclave decide "Server down since 9am, customers locked out" --ask "Is this urgent?"

# Scripting: one id=value line per question
cat ticket.txt | conclave decide --questions triage.yaml -q

# One named decider: give the state as a second argument
conclave decide jev "Server down since 9am" --ask "Is this urgent?"
```

**Positional arguments.** Two arguments are the decider list then the state. A single
argument is the decider list only when it is a comma list of two or more names
(`clef,jev`, state then from stdin or `-f`); anything else, including one word such as
`jev`, is the state and every configured decider answers it. Every entry of a decider list
must be `jev`, `clef`, `clef-flash` or an OpenRouter `vendor/model` decision model; a typo or a
chat provider is refused before anything is sent.

A questions file is YAML or JSON in the vendors' wire shape (`criteria` is a mapping for
`choice` (2-255 options), an ordered list for `score` (2-10 levels), and for `noul` an optional
mapping of `true`/`false` to what yes and no mean); 1-64 questions, ids of 1-100 letters, digits,
`_`, `.` or `-`. It is validated locally before anything is sent, so a malformed file
costs nothing.

```yaml
department:
  type: choice
  instructions: Which team should handle this ticket?
  criteria: {billing: Invoice or refund, technical: Bug or outage, sales: Plans and pricing}
frustration:
  type: score
  instructions: How frustrated is the customer?
  criteria: [Calm, Frustrated, Angry]
is_urgent:
  type: noul
  instructions: Must this be answered within an hour?
```

**Any OpenRouter decision model.** A `vendor/model` token is routed to OpenRouter's Decisions API
with `OPENROUTER_API_KEY`, so models from other vendors can join the panel and be priced from the
cost OpenRouter reports:

```bash
conclave decide jev,liquid/d1,upstage/solar-decide,inception/mercury-decide:free -f ticket.txt --questions triage.yaml
```

Find them at openrouter.ai (output modality "decisions"). Design: [ADR-017](docs/adr/ADR-017-slash-routed-decision-models-via-openrouter.md).

**Setup and behaviour:**

- **Keys:** see the [Providers](#providers) table. Set them in the environment,
  `~/.config/conclave/.env`, `conclave init`, or `conclave keyring set <VAR>`.
- **Not chat providers:** decision models are API-only (no `@cli`/`@api` suffix), never part of
  `--all`, and never accepted as a chat panel member or judge.
- **Flags:** `--cache`, `-t` (default 30 s per decider) and `--json` behave as on a query.
- **Exit status:** 0 when at least one decider answered, 1 when all failed, 130 on Ctrl-C.
- **Cost:** the vendor's own reported figure where one exists (OpenRouter's `usage.cost`, which
  covers every `vendor/model` decider and jev on that route); otherwise a hand-maintained table
  (`conclave models jev`, which works offline and with `CONCLAVE_NO_PRICING=1`). Input tokens only.
- **Secrets:** any API key or account id a vendor echoes back in an error is shown as `<redacted>`.

Design: [ADR-016](docs/adr/ADR-016-decision-models-are-a-separate-provider-class.md),
build plan: [docs/PLAN-decision-models.md](docs/PLAN-decision-models.md).

## Setup

### Interactive Setup

```bash
conclave init
```

Walks you through configuring API keys, validates each one, and saves to `~/.config/conclave/.env`. Keys load automatically on subsequent runs.

### Manual Setup

Set environment variables directly:
```bash
export GEMINI_API_KEY=your-key
export OPENAI_API_KEY=your-key
export ANTHROPIC_API_KEY=your-key
```

Or create `~/.config/conclave/.env`:
```bash
GEMINI_API_KEY=your-key
OPENAI_API_KEY=your-key
ANTHROPIC_API_KEY=your-key
```

### OS Keyring (no plaintext)

Store keys in the OS keyring (Windows Credential Manager / macOS Keychain / Linux
Secret Service) instead of a file. When the matching env var is unset, conclave
reads the key from the keyring automatically:

```bash
conclave keyring set GLM_API_KEY      # hidden prompt, or: echo "$KEY" | conclave keyring set GLM_API_KEY
conclave keyring list                 # show which provider keys are stored
conclave keyring rm  GLM_API_KEY
```

Resolution order is environment variable → `~/.config/conclave/.env` → `./.env` →
OS keyring. See [ADR-008](docs/adr/ADR-008-api-keys-resolve-from-environment-then-os-keyring.md).

### Model Catalog and Prices

Conclave keeps a cached copy of the [OpenRouter](https://openrouter.ai/models) model feed
(refreshed in the background once a day) and uses it to warn you when a configured model
id has disappeared, to price batch-mode cost estimates, and to answer "what exists and
what does it cost" without leaving the terminal:

```bash
conclave models                 # every provider, newest models first
conclave models claude          # one provider
conclave models --check         # do the compiled defaults still exist? exit 2 on drift
conclave models --refresh       # fetch now instead of waiting for the daily refresh
```

Prices shown are pay-as-you-go API prices and apply to API mode (`-g`, `-c`, `--batch`).
CLI mode runs on each provider's subscription and costs nothing per token. Set
`CONCLAVE_NO_PRICING=1` to disable the catalog entirely, or `CONCLAVE_PRICING_TTL=<hours>`
to change how often it refreshes. `--check` exits 2 on drift and 3 when the catalog is
unreachable, so a release script can tell the two apart. See
[docs/MODEL_REGISTRY.md](docs/MODEL_REGISTRY.md) for the annotated reference.

The catalog never blocks a query: once a cache exists it is served immediately, stale or
not, and refreshed in the background.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/diagrams/pricing-catalog-dark.svg">
  <img alt="State machine: no cache leads to one synchronous fetch; a fresh cache is served with no network until its TTL expires; a stale cache is served immediately while a background refresh makes it fresh; a failed first fetch leaves the catalog absent and queries proceed without prices" src="docs/diagrams/pricing-catalog.svg" width="100%">
</picture>

### Price-performance frontiers

`conclave models --frontier` lists the models no other model beats on both quality and
price, from **external** scores only. Conclave runs no evaluations of its own, every score
names its source and snapshot date, and a model nobody has scored is listed as unscored,
never estimated (ADR-018).

```bash
conclave models --frontier                          # chat: AA Intelligence Index vs blended list price
conclave models --frontier --axis coding            # or agentic
conclave models --frontier --cost task              # OpenRouter's measured cost per eval task (or: input, blend)
conclave models --frontier --deciders               # decision models: Decision Index vs input list price
conclave models --frontier --deciders --html d.html # plus a self-contained HTML report
conclave models --frontier --json                   # the full result, machine-readable
```

- **Chat models** need `OPENROUTER_API_KEY` (env, `.env` or `conclave keyring set
  OPENROUTER_API_KEY`), because OpenRouter's benchmarks feed is keyed. Without it the
  command says so and exits 3.
- **Decision models** need no key. Scores are the Decision Index (v0.2.1), recomputed from
  its upstream Hugging Face Space and cross-checked against Cloudflare's mirror; Clef rows
  come from the mirror and are marked self-reported. The only cost basis is list price per
  input token, which can mislead: per-call token counts differ by up to 13x between vendors.
- Output has four parts: the **frontier**, the top 15 **dominated** models, scored models
  with **no price** on the chosen basis, and the **unscored** names, followed by the sources.
- Sources are advisory. One that fails leaves its models unscored with a warning on
  stderr; only when nothing at all loads does the command exit 3. `--refresh` bypasses the
  caches (24 h by default; `CONCLAVE_BENCHMARKS_TTL`, `CONCLAVE_DECISION_INDEX_TTL`).
- The HTML report is one file with inline SVG charts (score vs cost on a log axis with the
  frontier step line, plus score vs latency for decision models), a sortable table, and
  the sources. It makes no network requests, so it works offline and from `file://`.

## Usage Examples

### Code Review

```bash
# Review a file
conclave gemini,claude,openai "Review for bugs and security issues" -f api.go --judge claude

# Compare implementations
conclave gemini,claude "Which approach is better?" -f impl_a.go -f impl_b.go --judge claude

# Pipe from stdin
git diff HEAD~1 | conclave gemini,claude "Review these changes" --judge claude
```

### Research & Analysis

```bash
# General knowledge (API mode)
conclave -g --all "Explain the trolley problem and its variations" --judge claude

# Fact-checking
conclave -g gemini,perplexity,claude "Is it true that..." --judge claude
```

### Architecture Decisions

```bash
conclave --all "Should we use microservices or monolith for this use case?" \
  -f requirements.md --judge claude --verbose
```

## Output Formats

By default the result is a styled display: verdict, confidence, reasoning, agreements,
disagreements and recommendations. The other formats:

- `--json`: structured output for scripts and CI (below).
- `--brief`: one line with the verdict, confidence and key recommendation.
- `-q` / `--quiet`: the verdict only, for scripts that just need the answer.
- `--raw`: sentinel-separated provider blocks with no judge or styling (below).

### JSON (`--json`)

```bash
conclave gemini,claude "Analyze" --judge claude --json | jq '.verdict'
```

Structured output for scripting and CI/CD integration.

### Cost Fields (API transport only)

Conclave prices each response that ran on the API (`-g` / `-c`, or a
`<provider>@api` token) from the cached OpenRouter catalog and shows the dollar
figure on the provider block, in the header panel, and in the `Completed in`
footer. `--json` carries the same numbers as `responses.<provider>.cost_usd`
and `meta.total_cost_usd` (providers plus judge), and says which leg ran where
in `responses.<provider>.transport`.

Three rules govern the numbers:

- **A CLI leg shows nothing about dollars.** Those providers ride subscriptions
  (Claude Max, Codex, the GLM Coding Plan), so a per-token price would be fiction.
  In a mixed panel (`gemini@api,claude@cli`) only the API leg carries a figure,
  and the CLI leg does not mark the total as understated.
- **An unknown price is omitted, never printed as `$0.00`.** If the catalog is
  offline, disabled with `CONCLAVE_NO_PRICING=1`, or simply does not list the
  model, the field is absent. A displayed zero always means a real zero.
- **A total ending in `+` is a floor.** At least one response could not be
  priced, so the true spend is higher than shown.

`--raw` and `--brief` are unchanged: both are fixed-shape contracts.

### Response cache (`--cache`)

Off by default. `--cache` reuses an identical provider response instead of
paying for it twice, which makes iterating on a prompt, a judge choice, or an
output format free.

```bash
conclave -g gemini,openai "Review this" -f auth.go --cache        # 24h TTL
conclave -g gemini,openai "Review this" -f auth.go --cache=6h     # explicit TTL
CONCLAVE_CACHE_TTL=6 conclave -g gemini,openai "Review this"      # via env, in hours
conclave -g gemini,openai "Review this" --no-cache                # override the env
```

An explicit TTL must use `--cache=6h`, not `--cache 6h`.

An entry is addressed by the mode, provider, model and the **full prompt
including any file or stdin context**, so changing one byte of an attached file
is a miss. A hit is marked `(cached)` on the progress line and the provider
block, carries `cached: true` in `--json`, and costs nothing. Failures are never
cached, and **judge synthesis is never cached** because a verdict depends on the
whole set of responses it saw. See ADR-011.

Works in both CLI and API mode, and batch mode honours it per item.

```bash
conclave cache stats   # directory, entry count, size, age range
conclave cache clear   # delete every cached response
```

Entries are plain JSON under `$XDG_CACHE_HOME/conclave/responses/`. Leave the
cache off for anything sensitive.

### Raw (`--raw`)

Sentinel-separated provider blocks only - no header art, no judge, no styling. For piping into downstream parsers.

```bash
conclave -g gemini,openai "Classify" --raw -f items.txt | my-extractor
```

Format:
```
===PROVIDER:openai MODEL:gpt-6.1-sol STATUS:success===
<response body>
===PROVIDER:claude MODEL:claude-opus-5-5 STATUS:error===
<error message>
===END===
```

Implies `--no-judge`.

## Flags Reference

```
Query Flags:
  -f, --file <path>      Include file content (repeatable)
  -j, --judge <provider> LLM that synthesizes verdict (default: claude)
      --no-judge         Skip synthesis, return raw responses
  -t, --timeout <secs>   Per-provider timeout (default: 60)
  -m, --model <p:model>  Override model for provider (p@cli:model / p@api:model also accepted)
      --max-context <n>  Max total -f/stdin context in bytes (default: 500000)
      --no-stdin         Ignore piped stdin
      --skip-preflight   Skip the credential check before querying

Mode Flags:
  -g, --general          Use API mode (no coding restrictions)
  -c, --cheap            Cheap mode: smaller/faster models, implies -g
  <provider>@cli|@api    Per-token transport, overrides -g/-c for that provider only
                         (works in the provider list, --judge and -m; see Modes)
  -a, --all              Query all available providers
      --blind            Anonymize providers for unbiased judging

Batch Mode:
      --batch <file>     JSONL input file for batch processing
      --workers <n>      Number of parallel workers (default: 5)
  -o, --output <file>    Output file (default: stdout)
      --resume           Resume from checkpoint: skip succeeded items, retry failed ones
      --retries <n>      Retry failed batch items N times with exponential backoff (batch mode only)
      --no-rate-limit    Disable rate limiting (high-tier API accounts)
      --budget <usd>     Stop dispatching once estimated spend hits this cap (also CONCLAVE_BATCH_BUDGET)

Cache Flags:
      --cache[=TTL]      Reuse identical provider responses (default 24h; also CONCLAVE_CACHE_TTL=<hours>)
      --no-cache         Never read or write the response cache

Output Flags:
      --json             Structured JSON output
      --verbose          Include full provider responses
      --brief            Short verdict only
  -q, --quiet            Minimal output (verdict only)
      --raw              Sentinel-separated provider blocks only (implies --no-judge)

Subcommands:
      conclave init      Set up API keys interactively
      conclave models    Inspect the model/price catalog (--check, --json, --all, --refresh)
                         --frontier [--deciders] [--axis intelligence|coding|agentic]
                         [--cost blend|input|task] [--html <file>]: price-performance frontier
      conclave decide    Decision-model panel: [deciders] [state] --questions <file> | --ask <text>
                         (-f, -t, --json, -q, --cache work as on a query)
      conclave keyring   Manage API keys in the OS keyring
      conclave cache     Inspect or empty the response cache

Other:
      --list-providers   List available providers and exit
      --version          Show version
```

Quote the prompt: it must be a single argument. A prompt that starts with `-` goes
after `--`, with every flag before it (`conclave --no-judge claude -- "-v means verbose?"`). A run whose judge fails,
or answers without a parseable verdict (`PARSE_ERROR`), still prints the panel and
exits 1; `--json` reports a failure in `execution.judge_error`.

## Features

### Parallel Execution

All providers are queried simultaneously. Total time ≈ slowest provider, not sum of all.

### Automatic Retry

Transient failures (429 rate limits, 5xx errors) automatically retry with exponential backoff:
- Up to 3 retries
- 1s → 2s → 4s delays with jitter
- Respects `Retry-After` headers

A 429 that means the account is out of credit (OpenAI's "You have no credits remaining" / `insufficient_quota`) is not retried, since waiting won't add credit: it fails on the first call with the vendor's message. The same goes for any 402 and Anthropic's `billing_error`.

This is built-in for **all** single-call queries via API mode. The `--retries` flag is separate and applies only to **batch mode** (`--batch`) — it retries failed items in the JSONL pipeline. Neither path retries a failure that resending can't fix: a billing failure, or a 4xx other than 429 (bad key, rejected parameter, unknown model, prompt too long). In batch an item skips its retries only when every provider in the panel failed that way; one transient cause (a 429, a 5xx, a timeout, a CLI error) still earns the retry. Either way the item stays out of the checkpoint, so `--resume` retries it once you've fixed the key or added credit. A batch that runs out of credit is not aborted: see [Batch Mode](docs/BATCH_MODE.md#retries-and-permanent-failures).

### Blind Mode

Anonymize provider names so the judge evaluates responses without brand bias:

```bash
conclave --all "Which solution is best?" -f options.md --judge claude --blind
```

The judge sees "Provider A", "Provider B", etc. instead of "OpenAI", "Claude".

### Context Handling

- Automatic stdin detection for piped content
- Multiple `-f` flags for comparing files
- Configurable context size limits

## Configuration

### Config File

`~/.config/conclave/config.yaml`:

```yaml
default_judge: claude        # used when --judge is not given
timeout_seconds: 60          # used when -t is not given (CONCLAVE_TIMEOUT overrides it)
max_context_size: 500000     # bytes; used when --max-context is not given

models:
  gemini: gemini-3.1-pro-preview
  openai: gpt-6.1-sol
  claude: claude-opus-5-5

# Pin a provider's transport for bare tokens (optional; ADR-012).
# A token's own @cli/@api suffix still wins; -g/-c applies to the rest.
transports:
  gemini: api
  claude: cli

# Override cheap mode models (optional)
cheap_models:
  gemini: gemini-3.1-flash-lite # Cheaper than the default cheap model
  openai: gpt-5-nano            # Older but cheaper than the default gpt-6-luna
```

### Environment Variables

```bash
CONCLAVE_TIMEOUT=30               # Per-provider timeout in seconds (a -t flag still wins)
CONCLAVE_GEMINI_MODEL=...         # Override default model
CONCLAVE_CHEAP_CLAUDE_MODEL=...   # Override cheap mode model
CONCLAVE_EXCLUDE=glm,grok         # Exclude providers from --all
CONCLAVE_PRICING_TTL=24           # Hours between OpenRouter catalog refreshes
CONCLAVE_NO_PRICING=1             # Disable the catalog (no network, no drift warnings)
```

## Using Conclave from agents and scripts

`--json` is the contract for callers: additive fields only, `status` is always `"success"` or
`"error"` per provider (a cache hit stays `"success"` and adds `cached: true`),
`responses.<provider>` is keyed by the bare provider name even when the token carried
`@cli` / `@api`, `responses.<provider>.transport` is present on every leg including CLI ones
(only `cost_usd` is API-only), so it is the field to read for vendor provenance, and
`execution.timeout_seconds` reports the real `-t`. Pair `--no-judge` with your own
aggregation when you want a majority vote across runs, and `--raw` when you want the
bodies with no parsing at all. Nothing is ever prompted for when stdin is not a terminal.

## Architecture Decisions

Key design decisions are recorded as ADRs in [`docs/adr/`](docs/adr/) — dual provider
modes, the LLM-as-judge synthesis, parallel/per-provider timeouts, the shared HTTP
client, credential precedence + OS-keyring fallback, the GLM Coding Plan transport, the
runtime pricing catalog, OpenRouter slash routing, the opt-in response cache, the
per-provider `@cli` / `@api` transport suffix, claude's isolation from the caller's
context, decision models as their own provider class, slash-routed decision models, and
price-performance frontiers from external, edition-pinned sources.

## License

MIT
