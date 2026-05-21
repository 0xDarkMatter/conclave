# Conclave

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Built with Charm](https://img.shields.io/badge/Built%20with-Charm-ff69b4?logo=data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI+PHBhdGggZmlsbD0id2hpdGUiIGQ9Ik0xMiAyQzYuNDggMiAyIDYuNDggMiAxMnM0LjQ4IDEwIDEwIDEwIDEwLTQuNDggMTAtMTBTMTcuNTIgMiAxMiAyem0wIDE4Yy00LjQxIDAtOC0zLjU5LTgtOHMzLjU5LTggOC04IDggMy41OSA4IDgtMy41OSA4LTggOHoiLz48L3N2Zz4=)](https://charm.sh)

> Stop juggling six AI CLIs. Query any model with one syntax, or unleash them all and let a judge synthesize the chaos.

Tired of memorizing whether it's `--file` or `-f` or piping to stdin? Sick of context-switching between `gemini`, `claude`, `codex`, and whatever CLI Grok ships this week? **Conclave is your universal remote for LLMs** - one command, one syntax, any provider. Learn it once, query everything.

But here's where it gets interesting: why trust a single AI's opinion when you can convene an entire council? Conclave queries multiple models in parallel, then hands their responses to a judge who synthesizes a verdict with confidence levels, agreements, disagreements, and actionable recommendations. It's like having a room full of very expensive consultants who actually have to reach consensus before billing you.

Built with [Charm](https://charm.sh)'s Bubble Tea for a terminal UI that doesn't look like it crawled out of 1985. Animated spinners, real-time progress, token counts - because if you're going to burn API credits, you should at least enjoy watching the meter spin.

## Why Conclave?

- **One interface** - Same syntax for Gemini, Claude, GPT, Grok, Perplexity, GLM
- **Reduce bias** - No single model's quirks dominate the response
- **Increase confidence** - Agreement across models = higher signal
- **Catch blind spots** - Different models notice different issues
- **Faster iteration** - Parallel queries, one synthesized answer
- **Beautiful TUI** - Animated progress with [Charm](https://charm.sh) (Bubble Tea)

## Terminal UI

Conclave features a rich terminal interface powered by [Bubble Tea](https://github.com/charmbracelet/bubbletea):

```
▸ Querying 3 providers...
  ├── ⠹ Google Gemini 3 Pro [02.34s]
  ├── ✓ xAI Grok 4.1 Fast [01.21s / 000168 tokens]
  └── ⠼ Anthropic Claude Opus 4.5 [03.12s]

▸ Crystallizing... ⠋ [02.45s]
```

- **Animated spinners** - Braille animation for active providers
- **Real-time progress** - Token counts and timing as providers complete
- **Synthesis verbs** - 25 rotating verbs during verdict synthesis
- **Non-TTY fallback** - Clean output for CI/CD and piped commands

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
conclave gemini "Explain" -m gemini:gemini-2.5-flash  # Fast
conclave gemini "Explain" -m gemini:gemini-3-pro-preview  # Thorough
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
| **gemini** | `gemini` | `npm install -g @anthropic-ai/gemini-cli` |
| **claude** | `claude` | `npm install -g @anthropic-ai/claude-code` |
| **openai** | `codex` | `npm install -g @openai/codex` |
| **grok** | `grok` | See [xAI Grok CLI](https://github.com/xai-org/grok-cli) |
| **perplexity** | `perplexity` | See [Perplexity CLI](https://github.com/perplexity-ai/perplexity-cli) |
| **glm** | `opencode` | See [OpenCode CLI](https://github.com/opencode-ai/opencode) |

**Check what's available:**

```bash
conclave --list-providers      # CLI mode - shows installed CLIs
conclave --list-providers -g   # API mode - shows configured API keys
```

**Tip:** Start with API mode (`-g`) to get running quickly. Add CLI tools later if you want their coding-specific optimizations.

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

**Cheap mode models:**

| Provider | Default Model | Cheap Model |
|----------|---------------|-------------|
| gemini | gemini-3-pro-preview | gemini-3-flash-preview |
| openai | gpt-5.2 | gpt-5-nano |
| claude | claude-opus-4-5 | claude-haiku-4-5 |
| perplexity | sonar-pro | sonar |
| grok | grok-4-1-fast | grok-4-1-fast-non-reasoning |
| glm | glm-4.7 | glm-4.6v-flashx |

### Batch Mode (`--batch`)

Process thousands of items with parallel workers, rate limiting, and resume capability. Built in Go for performant concurrent execution - scales to 200 parallel workers with minimal overhead. Uses cheap mode by default.

```bash
# Process a JSONL file with a single provider
conclave -c grok "Classify this account" --batch items.jsonl -o results.jsonl

# Parallel workers for faster throughput
conclave -c gemini "Analyze" --batch items.jsonl --workers 50 -o results.jsonl

# Resume an interrupted job
conclave -c claude "Analyze" --batch items.jsonl -o results.jsonl --resume
```

**Input format (JSONL):**
```jsonl
{"id": "1", "context": "Username: @acme_corp\nBio: Enterprise solutions...\nFollowers: 50K\n\nRecent posts:\n..."}
{"id": "2", "context": "Username: @jane_dev\nBio: Software engineer, coffee lover\nFollowers: 2K\n\nRecent posts:\n..."}
```

**Performance (99 items, 50 workers):**

| Provider | Time | Cost | Best For |
|----------|------|------|----------|
| **Grok** | 23s | $0.05 | Speed & cost efficiency |
| Gemini | 33s | $0.28 | Budget with decent quality |
| Claude | 39s | $0.65 | Accuracy, depth, nuanced analysis |
| OpenAI | 88s | $0.18 | Reliable fallback |

**Note:** Complex prompts slow throughput by 1.4-2.3x. Claude produces the most comprehensive analysis but at higher cost.

See [docs/BATCH_MODE.md](docs/BATCH_MODE.md) for full documentation and [docs/BATCH_BENCHMARKS.md](docs/BATCH_BENCHMARKS.md) for detailed performance benchmarks.

## Providers

| Provider | CLI Mode | API Mode (`-g`) | Env Variable |
|----------|----------|-----------------|--------------|
| gemini | `gemini` CLI | Gemini API | `GEMINI_API_KEY` |
| openai | `codex` CLI | OpenAI API | `OPENAI_API_KEY` |
| claude | `claude` CLI | Anthropic API | `ANTHROPIC_API_KEY` |
| perplexity | `perplexity` CLI | Perplexity API | `PERPLEXITY_API_KEY` |
| grok | `grok` CLI | xAI API | `XAI_API_KEY` |
| glm | `opencode` CLI | Zhipu API | `ZHIPU_API_KEY` |

### Default Models

| Provider | CLI Mode | API Mode |
|----------|----------|----------|
| gemini | gemini-3-pro-preview | gemini-3-pro-preview |
| openai | gpt-5.2 | gpt-5.2 |
| claude | claude-opus-4-5-20251101 | claude-opus-4-5-20251101 |
| perplexity | sonar-pro | sonar-pro |
| grok | grok-code-fast-1 | grok-4-1-fast-reasoning |

Override with `-m provider:model`:
```bash
conclave gemini,claude "Review this" -m gemini:gemini-2.5-flash -m claude:sonnet
```

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

### Check Available Providers

```bash
# CLI mode
conclave --list-providers

# API mode
conclave --list-providers -g
```

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

### Human-Readable (Default)

Shows verdict, confidence, reasoning, agreements, disagreements, and recommendations in a formatted display.

### JSON (`--json`)

```bash
conclave gemini,claude "Analyze" --judge claude --json | jq '.verdict'
```

Structured output for scripting and CI/CD integration.

### Brief (`--brief`)

One-line summary: verdict, confidence, and key recommendation.

### Quiet (`-q`)

Verdict only - for scripts that just need the answer.

### Raw (`--raw`)

Sentinel-separated provider blocks only - no header art, no judge, no styling. For piping into downstream parsers.

```bash
conclave -g gemini,openai "Classify" --raw -f items.txt | my-extractor
```

Format:
```
===PROVIDER:openai MODEL:gpt-5.2 STATUS:success===
<response body>
===PROVIDER:claude MODEL:claude-opus-4-5 STATUS:error===
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
  -m, --model <p:model>  Override model for provider

Mode Flags:
  -g, --general          Use API mode (no coding restrictions)
  -c, --cheap            Cheap mode: smaller/faster models, implies -g
  -a, --all              Query all available providers
      --blind            Anonymize providers for unbiased judging

Batch Mode:
      --batch <file>     JSONL input file for batch processing
      --workers <n>      Number of parallel workers (default: 5)
  -o, --output <file>    Output file (default: stdout)
      --resume           Resume from checkpoint, skip processed items
      --retries <n>      Retry failed batch items N times with exponential backoff (batch mode only)

Output Flags:
      --json             Structured JSON output
      --verbose          Include full provider responses
      --brief            Short verdict only
  -q, --quiet            Minimal output (verdict only)
      --raw              Sentinel-separated provider blocks only (implies --no-judge)

Other:
      --list-providers   List available providers and exit
      --version          Show version
```

## Features

### Parallel Execution

All providers are queried simultaneously. Total time ≈ slowest provider, not sum of all.

### Automatic Retry

Transient failures (429 rate limits, 5xx errors) automatically retry with exponential backoff:
- Up to 3 retries
- 1s → 2s → 4s delays with jitter
- Respects `Retry-After` headers

This is built-in for **all** single-call queries via API mode. The `--retries` flag is separate and applies only to **batch mode** (`--batch`) — it retries failed items in the JSONL pipeline. 400-class errors (auth, bad params) never retry in either path since they won't fix themselves.

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
default_judge: claude
timeout_seconds: 60

models:
  gemini: gemini-3-pro-preview
  openai: gpt-5.2
  claude: claude-opus-4-5-20251101

# Override cheap mode models (optional)
cheap_models:
  gemini: gemini-2.5-flash      # Upgrade from flash-lite
  claude: claude-sonnet-4-5     # Balance speed/quality
```

### Environment Variables

```bash
CONCLAVE_TIMEOUT=30               # Override timeout
CONCLAVE_GEMINI_MODEL=...         # Override default model
CONCLAVE_CHEAP_CLAUDE_MODEL=...   # Override cheap mode model
CONCLAVE_EXCLUDE=glm,grok         # Exclude providers from --all
```

## How It Works

```
┌─────────────────────────────────────────────────────────────┐
│                         CONCLAVE                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐        │
│  │ Gemini  │  │ OpenAI  │  │ Claude  │  │  Grok   │  ...   │
│  └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘        │
│       │            │            │            │              │
│       └────────────┴─────┬──────┴────────────┘              │
│                          │                                  │
│                          ▼                                  │
│                    ┌───────────┐                            │
│                    │   Judge   │                            │
│                    │  (Claude) │                            │
│                    └─────┬─────┘                            │
│                          │                                  │
│                          ▼                                  │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ Verdict: SAFE (high confidence)                      │  │
│  │ Agreements: [...]                                    │  │
│  │ Disagreements: [...]                                 │  │
│  │ Recommendations: [...]                               │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

1. **Query Phase** - Prompt sent to all providers in parallel
2. **Judge Phase** - Designated LLM synthesizes responses
3. **Output Phase** - Formatted result with confidence and reasoning

## Use Cases

### Single Provider (Unified Interface)

- **Quick queries** - Ask any LLM with consistent syntax
- **Model comparison** - Same prompt, different providers, see which you prefer
- **Specialized tasks** - Perplexity for search, Claude for code, Grok for X context

### Multi-Provider (Consensus)

- **Code Review** - Multiple perspectives on security, quality, performance
- **Fact-Checking** - Cross-reference claims across models
- **Architecture Decisions** - Consensus on design trade-offs
- **Research Synthesis** - Combine knowledge from multiple sources
- **Risk Assessment** - Identify blind spots in analysis

## Claude Code Integration

A skill is available for Claude Code users at `~/.claude/skills/conclave/SKILL.md` with:
- Usage patterns and examples
- Integration guidance for spawning LLMs from Claude Code sessions
- Batch processing workflows
- Prompt + context passing patterns

## License

MIT
