---
status: accepted
date: 2026-09-08
supersedes: []
superseded-by: []
extends: []
related: [ADR-001, ADR-002, ADR-009, ADR-010]
touches:
  - "internal/cache"
  - "internal/providers/provider.go"
  - "internal/orchestrator/orchestrator.go"
  - "internal/batch/processor.go"
  - "cmd/cache.go"
  - "cmd/root.go"
---

# ADR-011: Opt-in response cache keyed on the full prompt, never on the judge

## Decision (one sentence)

Conclave caches individual provider responses on disk, addressed by a sha256 of `(mode, provider, model, full prompt including file and stdin context, system prompt)`, only when the user asks for it with `--cache[=TTL]` or `CONCLAVE_CACHE_TTL`; judge synthesis is never cached.

## Context

Iterating on a prompt, a judge choice, or an output format means running the same question against the same models repeatedly. In API mode each of those runs is billed, and every run waits for the slowest provider. Most of that spend and latency buys an answer conclave already had.

Three properties of conclave shape what a cache may do here. A query is a fan-out to several providers plus a synthesis step, so the unit that can be cached is one provider response, not one invocation. The prompt actually sent is assembled from the question plus any `-f` files and piped stdin (`internal/context`), so "the same question" is not the same thing as "the same command line". And CLI mode and API mode send materially different requests for the same provider name, because the CLI wrappers carry their own coding-tuned behaviour.

## Alternatives considered

- **Cache on by default.** Rejected. Conclave's answer is a claim about what a model says now. A silent cache makes two runs a second apart look identical even when the model changed underneath, and nothing on screen would say why. Opt-in keeps the default honest and puts the freshness trade in the hands of whoever is making it.
- **Key on the user's question only, with context hashed separately or ignored.** Rejected. Attached context is the largest and most volatile part of most prompts. Any key that does not cover it will eventually serve an answer about an older version of a file, which is the worst failure this feature could have.
- **Cache the judge verdict too.** Rejected, and this is the sharpest constraint here. A verdict is a function of the *set* of responses the judge saw. That set is not a component of any single provider's key, and it changes whenever a provider is added, removed, times out, or errors. A cached verdict would confidently describe a panel that did not convene. ADR-001 makes synthesis the product; serving a stale one is worse than paying for a fresh one.
- **Key on the model alias rather than the resolved model id.** Rejected: `-m claude:x` and a changed default must both invalidate, so the key uses the id actually passed to `Query`.
- **A single flat directory of entries.** Rejected for a store that can reach thousands of files; entries shard on the first two hex characters of the key.

## Consequences

### Positive
- Re-running an identical query in API mode is free and instant, and says so: `(cached)` on the progress line and the provider block, `cached: true` in `--json`, and a cost of zero everywhere including the batch budget total.
- Because the key covers the full prompt, changing one byte of an attached file is a miss. The cache cannot answer a question that was not asked.
- Failures are never cached, so one transient 429 does not stick for the whole TTL.
- Every cache fault degrades to a live query. A corrupt entry, an unreadable directory, or a full disk costs a cache miss, never an error.

### Negative
- The hit rate is brittle by construction. Any whitespace change, any edit to an attached file, any model default bump is a miss. That is the intended trade, but it means the cache helps repetition and does nothing for near-duplicates.
- Entries are stored in plain JSON under the user cache directory. Prompts and responses are readable by anything with access to that directory, so the cache should stay off for sensitive material. It is off by default.
- Decorating a provider hides the optional `Preflighter` interface, because embedding the `Provider` interface promotes only the four methods it declares. Rather than depend on wrapping order, every decorator now implements `Unwrap() Provider` and `providers.RunPreflight` follows that chain to the real provider. Preflight is never served from the cache, so a cached answer cannot mask a revoked credential.
- On Windows a `Put` racing a concurrent read of the same entry can be refused, because a reader holding the file open blocks the rename. Put retries briefly and then gives up: a refused write costs one cache miss. Reads are never partial.
- Expired entries are ignored, not deleted. The store only shrinks on `conclave cache clear`.

## Notes

`conclave cache stats` reports the directory, entry count and size; `conclave cache clear` empties it. The store lives beside the pricing cache (ADR-009) at `$XDG_CACHE_HOME/conclave/responses/`, so conclave owns exactly one cache root.
