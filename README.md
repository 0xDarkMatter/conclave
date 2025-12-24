# Conclave CLI

> Universal multi-LLM consensus tool. Query multiple AI models in parallel, synthesize verdicts.

## Installation

```bash
# From source
git clone https://github.com/0xDarkMatter/conclave-cli
cd conclave-cli
make install  # installs to ~/.local/bin

# Or with sudo for global install
sudo make install-global  # installs to /usr/local/bin
```

## Usage

```bash
# Single provider (no judge)
conclave gemini "What does this code do?"

# Multiple providers with judge
conclave gemini,openai,glm "Is this secure?" --judge claude

# Pipe file content
cat src/auth.ts | conclave gemini,openai "Review this code" --judge claude

# Multiple files
conclave gemini,openai "Compare these" -f impl_a.go -f impl_b.go

# JSON output
conclave gemini,openai "Analyze" --judge claude --json

# Brief output
conclave gemini,openai "Is this correct?" --judge claude --brief

# List available providers
conclave --list-providers
```

## Providers

| Provider | CLI | Default Model |
|----------|-----|---------------|
| gemini | `gemini` | gemini-2.5-pro |
| openai | `codex` | gpt-5.2 |
| claude | `claude` | sonnet |
| perplexity | `perplexity` | sonar-pro |
| grok | `grok` | grok-code-fast-1 |
| glm | `opencode` | zai-coding-plan/glm-4.7 |

## Configuration

Create `~/.config/conclave/config.yaml`:

```yaml
default_providers:
  - gemini
  - openai
  - claude

default_judge: claude
timeout_seconds: 60

models:
  gemini: gemini-2.5-pro
  openai: gpt-5.2
  claude: sonnet
  perplexity: sonar-pro
  grok: grok-code-fast-1
  glm: zai-coding-plan/glm-4.7
```

Or use environment variables:

```bash
export CONCLAVE_GEMINI_MODEL=gemini-2.5-flash
export CONCLAVE_TIMEOUT=30
```

## Flags

```
-f, --file <path>      Include file content (repeatable)
-j, --judge <provider> LLM that synthesizes verdict (default: claude)
    --no-judge         Return raw results, skip synthesis
-t, --timeout <secs>   Per-provider timeout (default: 60)
-m, --model <p:model>  Override model for provider
    --json             Output structured JSON
    --verbose          Include full provider responses
    --brief            Short verdict only
-q, --quiet            Minimal output (verdict only)
    --list-providers   List available providers and exit
    --version          Show version
```

## How It Works

1. **Query Phase**: Sends your prompt to all specified providers in parallel
2. **Judge Phase**: A designated LLM (default: Claude) synthesizes the responses into a verdict
3. **Output Phase**: Formats the result (JSON, human-readable, or brief)

The judge identifies:
- Points of agreement across models
- Disagreements and which reasoning is stronger
- Final verdict with confidence level
- Actionable recommendations

## License

MIT
