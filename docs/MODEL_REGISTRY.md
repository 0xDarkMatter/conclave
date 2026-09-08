# Model Registry

> Canonical model reference for Conclave CLI providers. Prices in USD per million tokens.

**Last updated:** 2026-09-08
**Source:** OpenRouter public models feed (`https://openrouter.ai/api/v1/models`, 428 models on
the day of refresh), cross-checked against the defaults compiled into `internal/config/config.go`
and `internal/providers/*.go`. OpenRouter pass-through prices match vendor list prices for the
providers below unless noted. Vendor pages remain the authority for anything OpenRouter does not
carry (Perplexity request fees, Gemini long-context multipliers, GLM Coding Plan).

> **Which mode pays these prices.** Every price in this file is pay-as-you-go API pricing and
> applies to **API mode (`-g`) only**. OpenRouter has no notion of subscriptions. In CLI mode
> (the default) each provider's CLI authenticates against a subscription (Claude Max, ChatGPT /
> Codex, Google account for `gemini`, GLM Coding Plan) and the per-token cost is $0; the
> subscription is the cost, and rate limits rather than dollars are the constraint. This is the
> usual way Conclave is run here. Use the tables below to pick models and to budget `-g`, `-c`,
> and `--batch` runs, which do hit the metered APIs.

> **Live data beats this file.** `conclave models [provider]` prints the same feed, fetched
> within the last 24 hours, and `conclave models --check` verifies the compiled defaults
> against it (exit 1 on drift). This document is the annotated, human-readable layer on top;
> when the two disagree, the command is right. See ADR-009.

> **Maintenance:** the "Conclave Defaults" and "Cheap Mode" tables MUST match the maps in
> `internal/config/config.go`. If you change a default in code, change it here in the same commit.
> The "Drift Watch" section lists IDs the code still uses that OpenRouter no longer serves.

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
| **OpenRouter (aggregator)** | [openrouter.ai/models](https://openrouter.ai/models) | same page, per-model |

---

## Google Gemini

**API Base:** `https://generativelanguage.googleapis.com`
**Auth:** `GEMINI_API_KEY` or `GOOGLE_API_KEY`
**CLI mode:** `gemini` CLI on a Google account; no per-token charge.

| Model ID | Description | Context | Input $/M | Output $/M | Released |
|----------|-------------|---------|-----------|------------|----------|
| `gemini-3.1-pro-preview` | **Conclave default** - Frontier reasoning, strong SWE and agentic reliability | 1M | $2.00 | $12.00 | 2026-02 |
| `gemini-3.8-flash` | Newest Flash; Google's most capable Flash for SWE, agents, multi-step reasoning | 1M | $0.75 | $3.75 | 2026-09 |
| `gemini-3.7-flash` | Previous Flash generation | 1M | $0.75 | $3.75 | 2026-08 |
| `gemini-3.6-flash` | Previous Flash generation | 1M | $0.75 | $3.75 | 2026-07 |
| `gemini-3.5-flash` | Near-Pro coding at Flash speed; note the higher price point | 1M | $1.50 | $9.00 | 2026-05 |
| `gemini-3.5-flash-lite` | High-efficiency, tuned for subagents | 1M | $0.30 | $2.50 | 2026-07 |
| `gemini-3.1-flash-lite` | GA low-latency, high-volume multimodal | 1M | $0.25 | $1.50 | 2026-05 |
| `gemini-3-flash-preview` | **Conclave cheap** - Still served; superseded by the 3.x Flash line | 1M | $0.50 | $3.00 | 2025-12 |
| `gemini-2.5-pro` | Stable previous-gen flagship | 1M | $1.25 | $10.00 | 2025-06 |
| `gemini-2.5-flash` | Previous-gen balanced | 1M | $0.30 | $2.50 | 2025-06 |
| `gemini-2.5-flash-lite` | Previous-gen ultra-cheap | 1M | $0.10 | $0.40 | 2025-07 |

**Retired since last refresh:** `gemini-3-pro-preview` (shut down; replaced by `gemini-3.1-pro-preview`), `gemini-2.0-flash` (no longer listed).

**Notes:**
- Prompts >200K tokens: 2x input pricing for Pro models (vendor rule, not visible in the OpenRouter feed)
- Batch API: 50% discount on all models
- Gemini 1.x and 2.0 models are retired
- Flash naming moved to a rolling minor version (3.5, 3.6, 3.7, 3.8) at roughly monthly cadence; expect `gemini-3.9-flash` next

---

## OpenAI

**API Base:** `https://api.openai.com`
**Auth:** `OPENAI_API_KEY`
**CLI mode:** `codex` CLI on a ChatGPT subscription; no per-token charge.

| Model ID | Description | Context | Input $/M | Output $/M | Released |
|----------|-------------|---------|-----------|------------|----------|
| `gpt-5.6-sol` | Flagship of the GPT-5.6 series; complex reasoning, CLI and multi-step coding | 1M | $2.00 | $10.00 | 2026-07 |
| `gpt-5.6-terra` | Mid tier of GPT-5.6; everyday coding and agentic work | 1M | $2.00 | $12.00 | 2026-07 |
| `gpt-5.6-luna` | Cost tier of GPT-5.6; chat, classification, light agents | 1M | $0.20 | $1.20 | 2026-07 |
| `gpt-5.6-{sol,terra,luna}-pro` | Pro variants, same price as the base tier on OpenRouter | 1M | as base | as base | 2026-07 |
| `gpt-5.5` | **Conclave default** - Previous frontier, unified Codex+GPT line | 1M | $5.00 | $30.00 | 2026-04 |
| `gpt-5.5-pro` | Heavy-compute variant | 1M | $30.00 | $180.00 | 2026-04 |
| `gpt-5.4` | Previous frontier | 1M | $2.50 | $15.00 | 2026-03 |
| `gpt-5.4-mini` | Compact 5.4 | 400K | $0.75 | $4.50 | 2026-03 |
| `gpt-5.4-nano` | Smallest 5.4 | 400K | $0.20 | $1.25 | 2026-03 |
| `gpt-5.3-codex` | Codex line before unification | 400K | $1.75 | $14.00 | 2026-02 |
| `gpt-5.2` | Older frontier | 400K | $1.75 | $14.00 | 2025-12 |
| `gpt-5-nano` | **Conclave cheap** - Still served, cheapest GPT-5 family member | 400K | $0.05 | $0.40 | 2025-08 |
| `gpt-5-mini` | Small GPT-5 | 400K | $0.25 | $2.00 | 2025-08 |
| `gpt-4.1` | Legacy general purpose, 1M context | 1M | $2.00 | $8.00 | 2025-04 |
| `o3` | Legacy reasoning (price dropped sharply since first listing) | 200K | $2.00 | $8.00 | 2025-04 |
| `o4-mini` | Legacy compact reasoning | 200K | $1.10 | $4.40 | 2025-04 |

**Notes:**
- Reasoning tokens are billed as output tokens
- `gpt-5*`, `o1*`, `o3*` require `max_completion_tokens` instead of `max_tokens`; Conclave handles this in `api_openai.go` (`isReasoningModel`). The GPT-5.6 IDs start with `gpt-5` so they are covered.
- Batch API: 50% discount, 24hr turnaround
- The GPT-5.6 Sol tier is cheaper than GPT-5.5 on both input and output; consider it when next bumping the default
- `o1` is still listed at $15/$60 but there is no reason to use it

---

## Anthropic (Claude)

**API Base:** `https://api.anthropic.com`
**Auth:** `ANTHROPIC_API_KEY`
**CLI mode:** `claude` CLI on a Claude Max subscription; no per-token charge.

| Model ID | Description | Context | Input $/M | Output $/M | Released |
|----------|-------------|---------|-----------|------------|----------|
| `claude-fable-5-1` | Mythos-class tier above Opus; strongest agentic coding and long-running workflows | 1M | $10.00 | $50.00 | 2026-09 |
| `claude-fable-5` | First Fable release | 1M | $10.00 | $50.00 | 2026-06 |
| `claude-opus-5` | Flagship Opus; demanding reasoning, code review, bug finding, long-horizon agents | 1M | $5.00 | $25.00 | 2026-07 |
| `claude-sonnet-5` | Most capable Sonnet; adaptive thinking with selectable effort. Cheaper than Sonnet 4.x | 1M | $2.00 | $10.00 | 2026-06 |
| `claude-opus-4-8` | **Conclave default** - Last Opus 4.x, 1M context, reasoning | 1M | $5.00 | $25.00 | 2026-05 |
| `claude-opus-4-7` | Previous Opus 4.x | 1M | $5.00 | $25.00 | 2026-04 |
| `claude-opus-4-6` | Previous Opus 4.x | 1M | $5.00 | $25.00 | 2026-02 |
| `claude-sonnet-4-6` | Previous Sonnet | 1M | $3.00 | $15.00 | 2026-02 |
| `claude-sonnet-4-5` | Older Sonnet | 1M | $3.00 | $15.00 | 2025-09 |
| `claude-haiku-4-5-20251001` | **Conclave cheap** - Fast and efficient; latest Haiku available | 200K | $1.00 | $5.00 | 2025-10 |
| `claude-opus-4-5-20251101` | Older Opus (200K context) | 200K | $5.00 | $25.00 | 2025-11 |

**Notes:**
- Model IDs on the Anthropic API use hyphenated versions (`claude-opus-5`, `claude-sonnet-5`, `claude-fable-5-1`). OpenRouter uses dotted slugs (`anthropic/claude-opus-5`, `anthropic/claude-fable-5.1`). Conclave passes the vendor form.
- Long context (>200K): 2x input, 1.5x output pricing on 200K-class models; 1M-class models price flat
- Prompt caching: writes 1.25x, hits 0.1x
- Batch API: 50% discount (OpenRouter exposes this as `:batch` slugs)
- No Haiku 5 has shipped yet; Haiku 4.5 remains the cheap tier. Sonnet 5 at $2/$10 is now only 2x Haiku on input and worth considering as the cheap model when quality matters.
- Opus 5 and Opus 4.8 are priced identically, so a default bump to `claude-opus-5` is cost-neutral

---

## Perplexity

**API Base:** `https://api.perplexity.ai`
**Auth:** `PERPLEXITY_API_KEY`
**CLI mode:** `perplexity` CLI; metered by the account behind it.

| Model ID | Description | Context | Input $/M | Output $/M | Request Fee |
|----------|-------------|---------|-----------|------------|-------------|
| `sonar-pro` | **Conclave default** - Best factuality | 200K | $3.00 | $15.00 | $5-14/1K |
| `sonar` | **Conclave cheap** - Fast, cost-effective | 128K | $1.00 | $1.00 | $5-6/1K |
| `sonar-reasoning-pro` | Advanced reasoning | 128K | $2.00 | $8.00 | $5-8/1K |
| `sonar-deep-research` | Deep multi-step research | 128K | $2.00 | $8.00 | varies |
| `sonar-reasoning` | Reasoning-focused. Not listed on OpenRouter as of this refresh; verify against the vendor before relying on it | 128K | $1.00 | $5.00 | $5-6/1K |

**Notes:**
- Perplexity's lineup is unchanged since the last refresh; prices stable
- Request fees vary by search context depth (Low/Medium/High) and come from vendor docs, not OpenRouter
- All models include web search grounding
- `sonar-pro-search` exists only on OpenRouter (agentic Pro Search mode) and is not reachable through Conclave's direct-API provider

---

## xAI (Grok)

**API Base:** `https://api.x.ai`
**Auth:** `XAI_API_KEY`
**CLI mode:** `grok` CLI; metered by the account behind it.

| Model ID | Description | Context | Input $/M | Output $/M | Released |
|----------|-------------|---------|-----------|------------|----------|
| `grok-4.6` | Current flagship; frontier coding, knowledge work, STEM | 500K | $2.00 | $6.00 | 2026-08 |
| `grok-4.5` | Previous flagship | 500K | $2.00 | $6.00 | 2026-07 |
| `grok-4.3` | Reasoning, agentic, high factuality; batch available | 1M | $1.25 | $2.50 | 2026-04 |
| `grok-4.20` | Fast reasoning with agentic tool calling, low hallucination, 2M context | 2M | $1.25 | $2.50 | 2026-03 |
| `grok-4.20-multi-agent` | Multi-agent variant of 4.20 | 2M | $1.25 | $2.50 | 2026-03 |
| `grok-build-0.1` | Fast coding model for agentic SWE; natural CLI-mode candidate | 256K | $1.00 | $2.00 | 2026-05 |
| `grok-4-1-fast-reasoning` | **Conclave default** - See Drift Watch; no longer listed on OpenRouter | 2M | $0.20 | $0.50 | 2025-11 |
| `grok-4-1-fast-non-reasoning` | **Conclave cheap** - See Drift Watch; no longer listed on OpenRouter | 2M | $0.20 | $0.50 | 2025-11 |

**Retired since last refresh:** `grok-code-fast-1` (retired 2026-08-15 as scheduled), `grok-4`, `grok-3`, `grok-4-fast-reasoning`. None appear on OpenRouter any more.

**Notes:**
- xAI now brands as SpaceXAI in model descriptions
- The sub-dollar "fast" tier has vanished from OpenRouter; the cheapest current listing is `grok-build-0.1` at $1/$2. If the direct API has also dropped `grok-4-1-fast-*`, both Conclave grok defaults are broken and `grok-4.20` (reasoning, 2M context) is the closest replacement, with `grok-build-0.1` for cheap mode.
- Server-side tools: $5/1K calls (Web Search, X Search, Code Exec)

---

## Zhipu (GLM)

**Coding Plan endpoint (CLI mode):** `https://api.z.ai/api/coding/paas/v4`. OpenAI-compatible, flat-rate GLM Coding Plan subscription (no pay-as-you-go balance). Used directly over HTTP; **no `opencode` CLI needed**. Override with `GLM_BASE_URL`. See ADR-007.
**Pay-as-you-go endpoint (API mode):** `https://open.bigmodel.cn` (CN) / `https://api.z.ai/api/paas/v4` (Intl). Requires account balance. Disabled in Conclave, see ADR-006.
**Auth:** `GLM_API_KEY` / `ZAI_API_KEY` / `ZHIPU_API_KEY` (Bearer token)

| Model ID | Description | Context | Input $/M | Output $/M | Released |
|----------|-------------|---------|-----------|------------|----------|
| `glm-5.3` | Latest flagship; complex SWE and long-horizon agents, 1.3M context | 1.3M | $1.40 | $4.40 | 2026-08 |
| `glm-5.3-flash` | Native multimodal, hybrid sparse/linear attention; very cheap | 1.3M | $0.075 | $0.25 | 2026-08 |
| `glm-5.2` | **Conclave default** - Previous flagship, 1M context | 1M | $0.97 | $3.04 | 2026-06 |
| `glm-5.1` | Older 5.x | 200K | $0.97 | $3.04 | 2026-04 |
| `glm-5` | First 5.x | 200K | $0.60 | $1.92 | 2026-02 |
| `glm-5-turbo` | Speed-tuned 5 | 200K | $1.20 | $4.00 | 2026-03 |
| `glm-5v-turbo` | Vision turbo | 200K | $1.20 | $4.00 | 2026-04 |
| `glm-4.7` | Previous flagship | 200K | $0.40 | $1.75 | 2025-12 |
| `glm-4.7-flash` | Cheap 4.7 | 200K | $0.06 | $0.40 | 2026-01 |
| `glm-4.6v` | Vision-language | 128K | $0.30 | $0.90 | 2025-12 |
| `glm-4.5-air` | Lightweight | 128K | $0.13 | $0.85 | 2025-07 |
| `glm-4.6v-flashx` | **Conclave cheap** - See Drift Watch; not listed on OpenRouter | 128K | Free (vendor) | Free (vendor) | 2025-12 |

**Retired since last refresh:** `glm-4.5-flash`, `glm-4.6v-flash` (free-tier IDs no longer listed on OpenRouter; may persist on the vendor endpoint).

**Notes:**
- Prices above are pay-as-you-go pass-through. Under the Coding Plan (CLI mode, the way Conclave runs GLM) the per-token cost is $0; the subscription is the cost.
- GLM 5.2 pricing rose from the $0.60/$2.20 recorded in December 2025 to $0.97/$3.04
- `glm-5.3-flash` at $0.075/$0.25 is the obvious paid cheap-mode replacement if `glm-4.6v-flashx` disappears from the vendor API
- Context caching: reduced input rate on cached prefixes (vendor docs)

---

## Conclave Defaults

Models used when no `-m` override is given. Source of truth: `internal/config/config.go` `Models` map and each provider's `defaultModel`.

| Provider | CLI Mode | API Mode (`-g`) | Verified 2026-09-08 |
|----------|----------|-----------------|---------------------|
| gemini | `gemini-3.1-pro-preview` | `gemini-3.1-pro-preview` | API: 3.7s. CLI: needs `GEMINI_API_KEY` (free OAuth tier retired); falls back to the API if gemini-cli's auth is still set to OAuth |
| openai | `gpt-5.6-sol` | `gpt-5.6-sol` | CLI (codex, ChatGPT sub): 5s. API: 1.8s |
| claude | `claude-opus-5` | `claude-opus-5` | CLI (claude, Max sub): 8s. API: untested, key has no credit |
| perplexity | `sonar-pro` | `sonar-pro` | Listed, unchanged |
| grok | `grok-4.6` | `grok-4.6` | The only id the grok CLI offers (`grok models`). API: 2.5s |
| glm | `glm-5.3` (Coding Plan API) | `glm-5.3` (disabled, ADR-006) | Coding Plan: 3.2s |

Previous defaults (v1.2.0): openai `gpt-5.5`, claude `claude-opus-4-8`, grok `grok-4-1-fast-reasoning`, glm `glm-5.2`. All still served by their vendors as of the same date; override with `-m provider:model` if you need one.

---

## Cheap Mode (`-c`)

Models used when `--cheap` / `-c` is set. Cheap mode implies `-g`, so these are always metered. Source of truth: `internal/config/config.go` `CheapModels` map.

| Provider | Default Model | Cheap Model | Input $/M | Output $/M | Status vs OpenRouter feed |
|----------|---------------|-------------|-----------|------------|---------------------------|
| gemini | gemini-3.1-pro-preview | `gemini-3-flash-preview` | $0.50 | $3.00 | Listed; `gemini-3.1-flash-lite` is cheaper ($0.25/$1.50) |
| openai | gpt-5.6-sol | `gpt-5-nano` | $0.05 | $0.40 | Listed, still cheapest |
| claude | claude-opus-5 | `claude-haiku-4-5-20251001` | $1.00 | $5.00 | Listed, still the newest Haiku |
| perplexity | sonar-pro | `sonar` | $1.00 | $1.00 | Listed, current |
| grok | grok-4.6 | `grok-build-0.1` | $1.00 | $2.00 | Listed. `grok-4-1-fast-non-reasoning` ($0.20/$0.50) still works on xAI's API but is unlisted; set `CONCLAVE_CHEAP_GROK_MODEL` to use it |
| glm | glm-5.3 | `glm-5.3-flash` | $0.075 | $0.25 | Listed. Moot in practice: `-g glm` is disabled (ADR-006) |

**Cost comparison per 1K-token query (500 in / 500 out), API mode:**

| Mode | Est. Cost (5 providers + judge) |
|------|--------------------------------|
| Default (`-g`) | ~$0.04-0.06 |
| Cheap (`-c`) | ~$0.002-0.005 |
| CLI mode | $0 per token (subscriptions) |

---

## Drift Watch

`conclave models --check` passes as of 2026-09-08: every compiled default and cheap model resolves in the OpenRouter feed. Run it before each release; a MISSING row means either the vendor retired the id or OpenRouter dropped it, and only a smoke test against the vendor tells you which:

```bash
conclave models --check
conclave -g <provider> "Say hello" --no-judge
```

Resolved on 2026-09-08 (kept for the record):

| Where | Was | Now | Why |
|-------|-----|-----|-----|
| grok default (CLI + API) | `grok-4-1-fast-reasoning` | `grok-4.6` | Unlisted on OpenRouter (still served by xAI's API), and the grok CLI only offers `grok-4.6` |
| grok cheap | `grok-4-1-fast-non-reasoning` | `grok-build-0.1` | Cheapest listed Grok; the old id still works on the API and is 5x cheaper, so it is documented as an override above |
| glm cheap | `glm-4.6v-flashx` | `glm-5.3-flash` | Unlisted; replacement is $0.075/$0.25 with 1.3M context |
| `registry.go` display name | `claude-haiku-4-5-20251015` | `claude-haiku-4-5-20251001` | Now matches the cheap-model id in `config.go`, so cheap Claude prints "Claude Haiku 4.5" |
| openai default | `gpt-5.5` ($5/$30) | `gpt-5.6-sol` ($2/$10) | Newer flagship, 60% cheaper in API mode; codex CLI accepts it |
| claude default | `claude-opus-4-8` | `claude-opus-5` | Newer, identical price; claude CLI accepts it |
| glm default | `glm-5.2` | `glm-5.3` | Newer, larger context; Coding Plan serves it |

---

## Cost Estimation

Rough cost per 1K-token query (500 in, 500 out) in API mode, current Conclave defaults:

| Provider | Model | Est. Cost |
|----------|-------|-----------|
| Perplexity | sonar | $0.001 + request fee |
| Gemini | gemini-3-flash-preview | $0.00175 |
| GLM | glm-5.3 | $0.003 pay-as-you-go; $0 on Coding Plan |
| Grok | grok-4.6 | $0.004 |
| OpenAI | gpt-5.6-sol | $0.006 |
| Gemini | gemini-3.1-pro-preview | $0.007 |
| Claude | claude-opus-5 | $0.015 |

**Full Conclave query (5 providers + judge):** ~$0.03-0.05 with default models in API mode. In CLI mode the marginal cost is $0.

---

## Version History

- **2026-09-08:** Full refresh against the OpenRouter models feed. Added GPT-5.6 Sol/Terra/Luna, Claude Fable 5 / 5.1, Opus 5, Sonnet 5, Gemini 3.5 to 3.8 Flash, Grok 4.20 to 4.6 and Build 0.1, GLM 5.3 and 5.3 Flash. Marked retired IDs. Added the API-mode-only pricing note, Drift Watch, and the maintenance rule tying the defaults tables to `config.go`.
- **2026-06-18:** Defaults tables updated for v1.2.0 (gpt-5.5, gemini-3.1-pro-preview, claude-opus-4-8, glm-5.2, grok-4-1-fast-reasoning). Per-provider tables were not refreshed.
- **2025-12-25:** Initial registry with all provider pricing
