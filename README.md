# Conclave

> One CLI for every LLM. Query any model, or all of them at once.

Conclave is a unified interface for major LLM providers. Use it as a single-command gateway to Gemini, Claude, GPT, Grok, Perplexity, and GLM - or query them all in parallel and synthesize their responses into a verdict with confidence levels and actionable recommendations.

## Why Conclave?

- **One interface** - Same syntax for Gemini, Claude, GPT, Grok, Perplexity, GLM
- **Reduce bias** - No single model's quirks dominate the response
- **Increase confidence** - Agreement across models = higher signal
- **Catch blind spots** - Different models notice different issues
- **Faster iteration** - Parallel queries, one synthesized answer

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
  -a, --all              Query all available providers
      --blind            Anonymize providers for unbiased judging

Output Flags:
      --json             Structured JSON output
      --verbose          Include full provider responses
      --brief            Short verdict only
  -q, --quiet            Minimal output (verdict only)

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
```

### Environment Variables

```bash
CONCLAVE_TIMEOUT=30           # Override timeout
CONCLAVE_GEMINI_MODEL=...     # Override default model
CONCLAVE_EXCLUDE=glm,grok     # Exclude providers from --all
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

## License

MIT
