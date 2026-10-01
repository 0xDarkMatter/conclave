---
status: accepted
date: 2026-10-01
supersedes: []
superseded-by: []
related: [ADR-004]
touches:
  - "internal/batch/processor.go"
  - "internal/providers/api_base.go"
---

# ADR-015: Batch retries skip permanent failures, and running out of credit does not abort a batch

## Decision (one sentence)

Batch's item-level `--retries` loop stops retrying an item once every provider in its panel failed permanently (`providers.IsPermanent`: a `BillingError`, or a typed `APIError` with a 4xx status other than 429), and a batch whose key runs out of credit keeps dispatching, warning once on stderr, instead of aborting.

## Context

The HTTP layer (`doRequest`, ADR-004) never retried a 4xx other than 429, and since 573883c never retries a billing failure either. The batch layer above it did not know any of that: `processItem` re-ran any failed item `--retries` times with 1s/2s/4s backoff, so a revoked key, a rejected parameter or an out-of-credit account was sent the same request again and waited on, for a refusal that cannot change. The README already promised "400-class errors never retry in either path", which was false for batch.

Classifying needs a type. `parseAPIError` returned plain `fmt` errors, so batch could only have matched text. It now returns `*APIError{StatusCode, Text}` with `Error()` byte-identical to the old rendering (verified across 135 status/body pairs against the previous function, then pinned by `TestClientErrorsArePermanentAndKeepTheirText`).

The tempting next step is to abort the whole batch on the first out-of-credit item, since with one key every remaining item fails the same way. That premise does not hold here:

- `KeyRotator` rotates comma-separated keys per request, so one dead key among several fails only its share of items.
- OpenRouter can answer 402 for a single request whose `max_tokens` the remaining credit cannot cover, while smaller items still succeed.
- A top-up or auto-recharge mid-run makes later items succeed.
- In a mixed panel one provider out of credit only degrades the panel; the item fails only when every provider does.

With the retry fix each such item costs one unbilled request and no backoff, so continuing is cheap, and every failed item still gets its error line and stays out of the checkpoint for `--resume`.

## Alternatives considered

- **Abort on the first all-billing failure.** Rejected: key rotation and per-request 402s make it a false positive that drops items which would have answered.
- **Abort after N consecutive all-billing items.** Rejected: with rotation across two keys and concurrent workers, completion order is jittered, so a streak of N dead-key items turns up by chance over a large batch. A threshold only moves the false positive around.
- **Treat every 4xx as permanent, 429 included.** Rejected: a 429 that outlasted `doRequest`'s retries is exactly what batch's slower, rate-limited retry exists for.
- **Derive "permanent" from `isRetryable` alone, so any status `doRequest` won't retry counts.** Rejected: `isRetryable` omits Anthropic's 529 "overloaded", which would then stop being retried at the batch level too. Only the 4xx class is classified; every 5xx stays transient.

## Consequences

### Positive
- A dead key or a bad parameter fails each item on its first attempt instead of after `--retries` backoffs (seconds per item, per worker).
- The README's "400-class errors never retry in either path" is true.
- Callers classify API failures by type (`errors.As(err, *APIError)`, `IsPermanent`), never by matching text.

### Negative
- A batch on a single dead key still walks the whole input, one fast failed request per item, and writes an error line for each. The once-per-run stderr warning is the mitigation; Ctrl-C then `--resume` after topping up is the remedy.
- Untyped errors (CLI stderr, transport failures, timeouts, an empty answer) are always treated as transient, so a CLI provider's auth failure is still retried. Wrongly retrying costs a request; wrongly giving up loses an answer.
- `IsPermanent` follows `isRetryable` for the 4xx class: adding a 4xx to `isRetryable` (408, say) makes batch retry it too.

### Non-goals
- Does not change what `--resume` retries: permanent failures are permanent for one run only, so they stay out of the checkpoint.
- Does not change `doRequest`'s own retry policy, including the missing 529 (a separate fix).
- Does not cover the judge: a judge failure is a failed item and was never retried.

## See also

- `internal/batch/processor.go`: `processItem` (the retry loop and the no-abort warning), `everyCause`.
- `internal/providers/api_base.go`: `APIError`, `IsPermanent`, `parseAPIError` / `apiErrorText`.
- `TestPermanentFailureIsNotRetried` (`internal/batch/processor_test.go`), `TestClientErrorsArePermanentAndKeepTheirText` (`internal/providers/api_openai_test.go`).
- [ADR-004](ADR-004-shared-openai-compatible-http-client-with-retry-backoff.md): the HTTP-layer retry this mirrors.
