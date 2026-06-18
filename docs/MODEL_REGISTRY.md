# Model Registry

> Canonical model reference for Conclave CLI providers. Prices in USD per million tokens.

**Last updated:** December 2025

---

## Canonical Documentation

| Provider | Models Page | Pricing Page |
|----------|-------------|--------------|
| **Google Gemini** | [ai.google.dev/gemini-api/docs/models](https://ai.google.dev/gemini-api/docs/models) | [ai.google.dev/gemini-api/docs/pricing](https://ai.google.dev/gemini-api/docs/pricing) |
| **OpenAI** | [platform.openai.com/docs/models](https://platform.openai.com/docs/models) | [openai.com/api/pricing](https://openai.com/api/pricing/) |
| **Anthropic** | [docs.anthropic.com/en/docs/models](https://docs.anthropic.com/en/docs/about-claude/models) | [claude.com/pricing](https://claude.com/pricing) |
| **Perplexity** | [docs.perplexity.ai](https://docs.perplexity.ai) | [docs.perplexity.ai/getting-started/pricing](https://docs.perplexity.ai/getting-started/pricing) |
| **xAI (Grok)** | [docs.x.ai/docs/models](https://docs.x.ai/docs/models) | [docs.x.ai/docs/models](https://docs.x.ai/docs/models) |
| **Zhipu (GLM)** | [docs.z.ai](https://docs.z.ai) | [docs.z.ai/guides/overview/pricing](https://docs.z.ai/guides/overview/pricing) |

---

## Google Gemini

**API Base:** `https://generativelanguage.googleapis.com`
**Auth:** `GEMINI_API_KEY` or `GOOGLE_API_KEY`

| Model ID | Description | Context | Input $/M | Output $/M |
|----------|-------------|---------|-----------|------------|
| `gemini-3-pro-preview` | **Default** - Latest flagship, best reasoning | 1M | $2.00 | $12.00 |
| `gemini-3-flash-preview` | Fast multimodal, strong coding | 1M | $0.50 | $3.00 |
| `gemini-2.5-pro` | Stable flagship with adaptive thinking | 1M | $1.25 | $10.00 |
| `gemini-2.5-flash` | Balanced speed/quality | 1M | $0.30 | $2.50 |
| `gemini-2.5-flash-lite` | Ultra-efficient, high-frequency tasks | 1M | $0.10 | $0.40 |
| `gemini-2.0-flash` | Previous gen, still capable | 1M | $0.10 | $0.40 |

**Notes:**
- Prompts >200K tokens: 2x input pricing for Pro models
- Batch API: 50% discount on all models
- Gemini 1.x models are retired (404 errors)

---

## OpenAI

**API Base:** `https://api.openai.com`
**Auth:** `OPENAI_API_KEY`

| Model ID | Description | Context | Input $/M | Output $/M |
|----------|-------------|---------|-----------|------------|
| `gpt-5.5` | **Default** - Best for coding/agents | 1M | $5.00 | $30.00 |
| `gpt-5.2` | Previous frontier model | 400K | $1.75 | $14.00 |
| `gpt-4.1` | General purpose, 1M context | 1M | ~$2.00 | ~$8.00 |
| `gpt-4o` | Multimodal, balanced | 128K | $2.50 | $10.00 |
| `o4-mini` | Compact reasoning model | 200K | $1.10 | $4.40 |
| `o3` | Advanced reasoning | 200K | $10.00 | $40.00 |
| `o1` | Reasoning-first | 200K | $15.00 | $60.00 |

**Notes:**
- Reasoning tokens are billed as output tokens
- Batch API: 50% discount, 24hr turnaround
- Cached input available at reduced rates

---

## Anthropic (Claude)

**API Base:** `https://api.anthropic.com`
**Auth:** `ANTHROPIC_API_KEY`

| Model ID | Description | Context | Input $/M | Output $/M |
|----------|-------------|---------|-----------|------------|
| `claude-opus-4-5-20251101` | **Default** - Best for coding/agents | 200K | $5.00 | $25.00 |
| `claude-sonnet-4-5-20250929` | Balanced, 1M context available | 200K/1M | $3.00 | $15.00 |
| `claude-haiku-4-5-20251015` | Fast and efficient | 200K | $1.00 | $5.00 |
| `claude-opus-4-1-20250414` | Previous Opus, strong reasoning | 200K | $15.00 | $75.00 |
| `claude-sonnet-4-20250514` | Previous Sonnet | 200K | $3.00 | $15.00 |

**Notes:**
- Long context (>200K): 2x input, 1.5x output pricing
- Prompt caching: writes 1.25x, hits 0.1x (5-min TTL)
- Batch API: 50% discount

---

## Perplexity

**API Base:** `https://api.perplexity.ai`
**Auth:** `PERPLEXITY_API_KEY`

| Model ID | Description | Context | Input $/M | Output $/M | Request Fee |
|----------|-------------|---------|-----------|------------|-------------|
| `sonar-pro` | **Default** - Best factuality | 200K | $3.00 | $15.00 | $5-14/1K |
| `sonar` | Fast, cost-effective | 128K | $1.00 | $1.00 | $5-6/1K |
| `sonar-reasoning` | Reasoning-focused | 128K | $1.00 | $5.00 | $5-6/1K |
| `sonar-reasoning-pro` | Advanced reasoning | 128K | $2.00 | $8.00 | $5-8/1K |
| `sonar-deep-research` | Deep multi-step research | 128K | $2.00 | $8.00 | varies |

**Notes:**
- Request fees vary by search context depth (Low/Medium/High)
- Citation tokens no longer billed (except Deep Research)
- All models include web search grounding

---

## xAI (Grok)

**API Base:** `https://api.x.ai`
**Auth:** `XAI_API_KEY`

| Model ID | Description | Context | Input $/M | Output $/M |
|----------|-------------|---------|-----------|------------|
| `grok-4-1-fast-reasoning` | **Default** - Best tool-calling, 2M context | 2M | $0.20 | $0.50 |
| `grok-4-1-fast-non-reasoning` | Fast without reasoning overhead | 2M | $0.20 | $0.50 |
| `grok-4` | Flagship reasoning model | 256K | $3.00 | $15.00 |
| `grok-4-fast-reasoning` | Previous fast model | 256K | $0.20 | $0.50 |
| `grok-3` | Previous generation | 128K | $3.00 | $15.00 |
| `grok-code-fast-1` | Optimized for code | 128K | $0.20 | $0.50 |

**Notes:**
- Large context (>256K): 2x pricing
- Server-side tools: $5/1K calls (Web Search, X Search, Code Exec)
- Knowledge cutoff: November 2024

---

## Zhipu (GLM)

**API Base:** `https://open.bigmodel.cn` (CN) / `https://api.z.ai` (Intl)
**Auth:** `ZHIPU_API_KEY`

| Model ID | Description | Context | Input $/M | Output $/M |
|----------|-------------|---------|-----------|------------|
| `glm-5.2` | **Default** - Latest, 1M context, coding-first | 1M | $0.60 | $2.20 |
| `glm-4.7` | Previous flagship | 200K | $0.60 | $2.20 |
| `glm-4.6` | Older flagship | 128K | $0.60 | $2.20 |
| `glm-4.5` | Stable release | 128K | $0.60 | $2.20 |
| `glm-4.5-air` | Lightweight, fast | 128K | $0.20 | $1.10 |
| `glm-4.5-flash` | Free tier | 128K | Free | Free |
| `glm-4.6v` | Vision-language | 128K | $0.30 | $0.90 |
| `glm-4.6v-flash` | Vision, free tier | 128K | Free | Free |

**Notes:**
- Context caching: $0.11/M (vs $0.60 standard)
- Currently disabled in Conclave due to slow response times (~20s)
- Extremely cost-effective for high-volume workloads

---

## Conclave Defaults

These are the models used when no `-m` override is specified:

| Provider | CLI Mode | API Mode (`-g`) |
|----------|----------|-----------------|
| gemini | `gemini-3.1-pro-preview` | `gemini-3.1-pro-preview` |
| openai | `gpt-5.5` | `gpt-5.5` |
| claude | `claude-opus-4-8` | `claude-opus-4-8` |
| perplexity | `sonar-pro` | `sonar-pro` |
| grok | `grok-code-fast-1` | `grok-4-1-fast-reasoning` |
| glm | `zai-coding-plan/glm-5.2` | `glm-5.2` (disabled) |

---

## Cheap Mode (`-c`)

Models used when `--cheap` / `-c` flag is set (for pipelines and batch processing):

| Provider | Default Model | Cheap Model | Input $/M | Output $/M |
|----------|---------------|-------------|-----------|------------|
| gemini | gemini-3.1-pro-preview | `gemini-3-flash-preview` | $0.50 | $3.00 |
| openai | gpt-5.5 | `gpt-5-nano` | $0.10 | $0.40 |
| claude | claude-opus-4-8 | `claude-haiku-4-5-20251001` | $1.00 | $5.00 |
| perplexity | sonar-pro | `sonar` | $1.00 | $1.00 |
| grok | grok-4-1-fast | `grok-4-1-fast-non-reasoning` | $0.20 | $0.50 |
| glm | glm-5.2 | `glm-4.6v-flashx` | Free | Free |

**Cost comparison per 1K-token query:**

| Mode | Est. Cost (5 providers + judge) |
|------|--------------------------------|
| Default | ~$0.03-0.05 |
| Cheap (`-c`) | ~$0.002-0.005 |

Cheap mode is ~10x more cost-effective for batch/pipeline workloads.

---

## Cost Estimation

Rough cost per 1K-token query (typical: 500 in, 500 out):

| Provider | Model | Est. Cost |
|----------|-------|-----------|
| Grok | grok-4-1-fast | $0.00035 |
| GLM | glm-5.2 | $0.0014 |
| Gemini | gemini-3-flash | $0.00175 |
| Perplexity | sonar | $0.001 + request fee |
| Gemini | gemini-3.1-pro | $0.007 |
| OpenAI | gpt-5.5 | $0.0175 |
| Claude | opus-4.8 | $0.015 |

**Full Conclave query (5 providers + judge):**
~$0.03-0.05 per query with default models

---

## Version History

- **2025-12-25:** Initial registry with all provider pricing
