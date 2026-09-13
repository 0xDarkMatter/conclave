# OpenRouter: any model as a panel member or judge

> User guide for the OpenRouter integration. Rationale lives in
> [ADR-010](adr/ADR-010-openrouter-as-a-slash-routed-api-backend.md); agent-facing rules in
> `AGENTS.md`. This file explains how to use it, what it costs, and what the errors mean.

Conclave has six direct providers (gemini, openai, claude, perplexity, grok, glm). For anything
else, or for a second model from a vendor you already use, write an
[OpenRouter](https://openrouter.ai/models) slug (`vendor/model`) where you would normally write a
provider name. That works in API mode only (`-g`), because OpenRouter is pay-as-you-go and has no
subscription path.

```bash
conclave -g deepseek/deepseek-v4-pro,anthropic/claude-opus-5 "Compare these" --judge openai/gpt-5.6-sol
conclave -g google/gemini-3.8-flash,claude "Summarise"        # mix with direct providers
conclave -g qwen/qwen3.8-max-0902 "Translate to French" --no-judge   # single model, no judge
```

The slug is both the provider name and the model id. It appears verbatim on the progress line
(with the catalog's display name when available), in the result boxes, as the judge label, and in
`--json` output under `execution.providers` / `execution.judge` and as the key of each response.

---

## 1. Setup

1. Create a key at <https://openrouter.ai/settings/keys> and add credit at
   <https://openrouter.ai/credits>.
2. Give it to conclave, any of these ways (highest precedence first):

   ```bash
   export OPENROUTER_API_KEY=sk-or-...            # shell / CI
   echo "OPENROUTER_API_KEY=sk-or-..." >> ~/.config/conclave/.env
   conclave keyring set OPENROUTER_API_KEY         # OS keyring, hidden prompt
   conclave init                                   # interactive; validates the key via /auth/key
   ```

3. Check it registered:

   ```bash
   conclave --list-providers -g
   #   openrouter    <vendor>/<model>                [ready]
   ```

The `openrouter` row is a placeholder to show key status. It is **not** a provider name;
`conclave -g openrouter ...` is an error that points you at the slash syntax.

## 2. Finding slugs

Conclave keeps a cached copy of OpenRouter's model feed (ADR-009). Use it rather than guessing:

```bash
conclave models                      # the six direct vendors, newest first
conclave models deepseek/x           # every model from one OpenRouter vendor (the part after / is ignored)
conclave models --all deepseek/x     # include :free / :batch / :thinking variants
conclave models --refresh            # fetch now instead of waiting for the daily refresh
conclave models --json | jq -r '.models[].id | select(startswith("mistralai/"))'
```

Variants are part of the slug and pass through untouched: `deepseek/deepseek-v4-flash:free`,
`openai/gpt-5.6-sol:nitro`, `anthropic/claude-opus-5:thinking`. Conclave does no rewriting of
slash tokens. If you type a vendor-style id (`anthropic/claude-opus-4-8`) instead of the
OpenRouter slug (`anthropic/claude-opus-4.8`) you get a drift warning and, from OpenRouter, a 400.

Slugs are lowercase. `DeepSeek/DeepSeek-V4-Pro` is not normalised; it triggers the drift warning.

## 3. Cost

- Prices are OpenRouter's pass-through vendor list prices plus a platform fee of roughly 5%.
  `conclave models` shows the price OpenRouter charges per million tokens.
- **Subscriptions cannot be used.** Claude Max, ChatGPT/Codex, and the GLM Coding Plan only work
  in CLI mode through the direct providers. Every OpenRouter token is metered.
- Batch mode (`--batch`) prices OpenRouter slugs from the live catalog, so cost estimates are
  accurate for them without any table maintenance.

**When to use a direct provider instead.** For gemini, openai and claude the direct API is
cheaper (no fee), sends provider-specific fields (for example `max_completion_tokens` for gpt-5.x,
Anthropic's native wire format), and in CLI mode is subscription-billed. Use OpenRouter for models
conclave has no direct provider for, or when you want two models from the same vendor on one panel.

## 4. The judge rule

Any slug can be the judge (`--judge anthropic/claude-sonnet-5`). Two things happen before the
panel runs that are specific to slash-routed judges:

1. The judge is resolved and preflighted **first**. A judge that cannot be built (missing key,
   wrong mode, malformed slug) fails before any provider is paid for.
2. A judge slug that is **not in the catalog is refused**:

   ```
   Error: judge "deepseek/nope" is not in the OpenRouter catalog (fetched 2026-09-08); the panel
   would run and then fail at synthesis. Check `conclave models deepseek/nope`, or pass
   --skip-preflight to send it anyway
   ```

   Panel members only get a warning for the same condition, because one failed member does not
   waste the others. The judge failing wastes the whole panel, hence the refusal. The catalog can
   lag a brand-new model by a day; `--skip-preflight` sends the slug regardless.

## 5. What is checked before a query

With an OpenRouter token on the panel, preflight does one `GET /api/v1/auth/key` per run. It
spends no tokens and turns two conditions into a clear stop:

```
  Preflight auth check failed:

    ✗  deepseek/deepseek-v4-pro  OpenRouter key check failed: HTTP 401 ...
       → Set OPENROUTER_API_KEY (or: conclave keyring set OPENROUTER_API_KEY) and check credit at https://openrouter.ai/credits
```

- **HTTP 401**: the key is wrong or revoked.
- **"no credit remaining (limit $X, used $Y)"**: the key has a spend limit and has hit it. Keys
  without a limit never trigger this; a 402 from the query itself is the signal then.

`--skip-preflight` bypasses both checks.

## 6. Error decoder

| You see | Meaning | Do |
|---|---|---|
| `provider "x/y" is an OpenRouter model ... OpenRouter is API-only: add -g` | Slash token without `-g` | Add `-g` |
| `malformed OpenRouter slug "/y": expected vendor/model` | Empty vendor or model half | Fix the slug |
| `"openrouter" is not a provider: name an OpenRouter model as vendor/model` | Used the placeholder row as a name | Use a slug |
| `provider x/y not available (OPENROUTER_API_KEY not set)` | No key in env, `.env`, or keyring | Section 1 |
| `warning: OpenRouter slug "x/y" is not in the catalog ... Newest listed: ...` | Catalog does not know the slug (typo, or newer than the cache) | Check `conclave models x/…`, or `--refresh` |
| `HTTP 400: ... is not a valid model ID` | OpenRouter rejected the slug | Same as above; the warning will have fired first |
| `HTTP 402: Insufficient credits` | Account balance exhausted mid-run | Add credit |
| `OpenRouter error 502: Provider returned error` | OpenRouter answered 200 but relayed an upstream failure | Retry, or pick another variant/vendor |
| `HTTP 429 rate limited` | OpenRouter or the upstream throttled; conclave retries 3× with backoff | Reduce `--workers` in batch mode |

## 7. Interactions with other flags

- `--all -g` never includes OpenRouter models. Name them explicitly. With only an OpenRouter key
  configured, `--all -g` reports "no providers available" and says why.
- `-c` (cheap mode) has no cheap OpenRouter model; the slug you typed is used as is.
- `-m` overrides work on slugs too (`-m deepseek/deepseek-v4-pro:deepseek/deepseek-v4-flash`),
  but writing the slug you want directly is simpler.
- `--blind` anonymises OpenRouter models like any other (Provider A, B, ...); the judge never sees
  the slug.
- `CONCLAVE_NO_PRICING=1` disables the catalog: display names fall back to the raw slug, drift
  warnings and the judge refusal are skipped, and batch estimates use the offline fallback table
  (which does not price OpenRouter slugs, so they estimate as $0).
- `CONCLAVE_EXCLUDE` is irrelevant: OpenRouter models are never auto-included.
- `--cache` treats a slug like any provider: the key includes the slug and the full prompt, a hit
  costs nothing and never counts toward `--budget`. `--no-cache` overrides an env-enabled cache.
- `@cli` / `@api` transport suffixes (ADR-012): a slug is API-only, so `deepseek/deepseek-v4@cli`
  is an error that says so. `deepseek/deepseek-v4@api` is accepted and makes `-g` unnecessary for
  that token. This is what lets a slug sit beside a subscription CLI in one panel:
  `conclave deepseek/deepseek-v4@api,claude@cli "..." --judge claude@cli`. The slug stays the
  provider name in every output; only `responses.<slug>.transport` says `"api"`.

## 8. Not supported (by design)

- OpenRouter's provider-routing preferences, fallback lists, and per-request `provider` object.
  Encode what you can in the slug variant (`:nitro`, `:floor`); everything else is out of scope.
- Streaming. Conclave collects whole responses.
- A plain `openrouter` provider, or routing the direct providers through OpenRouter. See
  ADR-010 for why both were rejected.

## See also

- [ADR-010](adr/ADR-010-openrouter-as-a-slash-routed-api-backend.md): the decision and its rejected alternatives.
- [ADR-009](adr/ADR-009-runtime-pricing-catalog-from-openrouter.md): the catalog that supplies slugs, prices and display names.
- [MODEL_REGISTRY.md](MODEL_REGISTRY.md): the annotated reference for the direct providers.
- [BATCH_MODE.md](BATCH_MODE.md): batch processing, which prices OpenRouter slugs from the catalog.
