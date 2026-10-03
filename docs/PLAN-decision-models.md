# Plan: decision models (System One) as a panel class

> Phase 1 built (2026-10-02); Phase 0 partly probed; Phases 2-4 planned. Branch
> `lane/decision-models`, 2026-10-02, written against `main` at `2bc6cca` (v1.4.0). The architecture is decided in
> [ADR-016](adr/ADR-016-decision-models-are-a-separate-provider-class.md); this file is
> the build order. Mark phases done here as they land; history goes to CHANGELOG.

## Goals

Let Conclave put decision models (Jev, Clef, Clef-flash) on a panel of their own:
typed questions in, typed answers with probabilities out, combined per question by
arithmetic rather than by an LLM judge. Then reuse that panel where Conclave already
does classification work: batch mode first, rubric scoring (reliability plan Feature
5) second.

## Non-goals

No change to `Provider`, `--all`, the judge, ADR-012's transport grammar or the
pricing catalog. No images in phase 1. No vendor fine-tuning features. Praxis is not
modified. Its switch to decision-model scoring is a decision for Praxis after the
calibration eval (Phase 0).

## Wire contract (both vendors)

Request, verbatim from the Typesafe quickstart. Clef claims full compatibility.

```json
{ "model": "jev-latest",
  "state": "<string or structured data>",
  "questions": {
    "department":  {"type": "choice", "instructions": "...", "criteria": {"billing": "...", "technical": "..."}},
    "frustration": {"type": "score",  "instructions": "...", "criteria": ["Calm", "Frustrated", "Angry"]},
    "is_urgent":   {"type": "noul",   "instructions": "..."} } }
```

Response:

```json
{ "model": "jev-1.13.0",
  "answers": {
    "department":  {"type": "choice", "choice": "technical", "confidence": 0.78,
                    "probabilities": {"technical": 0.85, "sales": 0.0, "billing": 0.15}},
    "frustration": {"type": "score", "score": 1.0, "confidence": 1.0,
                    "legend": {"0": "Calm", "1": "Frustrated", "2": "Angry"},
                    "probabilities": {"0": 0.0, "1": 1.0, "2": 0.0}},
    "is_urgent":   {"type": "noul", "noul": 1.0} },
  "usage": {"input_tokens": 392, "output_tokens": 65} }
```

| Decider | Endpoint | Auth | Default model | Context | Price (2026-10) |
|---|---|---|---|---|---|
| `jev` | `POST https://api.typesafe.ai/v1/systemone`, else `POST https://openrouter.ai/api/alpha/decisions` | `Bearer TYPESAFE_API_KEY`, else `OPENROUTER_API_KEY` | `jev-latest` | 32k | $0.042/M in, output free |
| `clef` | `POST https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/ai/run/@cf/cloudflare/clef` | `Bearer CLOUDFLARE_API_TOKEN` | `clef` | 64k | $0.24/M in |
| `clef-flash` | same, `@cf/cloudflare/clef-flash` | same | `clef-flash` | 64k | | 64k | unpublished at 2026-10-02; confirm |.09/M in (pricing page, updated 2026-10-01) |

Limits from the Clef model page and schema: 1-64 questions per call, 2-255 choice options, 2-10 score levels; up to 4 images (PNG/JPEG/WebP,
4 MiB, 16 MP each).

## Phase 0: live probes (about 2 h, before any code)

Unknowns that change code shape. Credentials come from Keeper as agent-01; a Typesafe
key may need to be added there first.

1. **Workers AI wrapper.** Does `/ai/run` return the answers bare, or wrapped in
   `{"result": {...}, "success": true, "errors": [], "messages": []}` like other Workers
   AI models? This decides whether the shared client needs a per-backend unwrap.
2. **Error shapes.** Send one malformed question (choice without criteria), an
   oversized state and a bad key to each backend, and record status codes and bodies
   for the `BillingError` and 4xx mapping.
3. **`noul` fields.** Does Clef return `confidence` on `noul`? Jev returns only the
   value.
4. **Preflight.** Is there a cheap auth check? Cloudflare has
   `GET /client/v4/user/tokens/verify`; Typesafe has none documented. A one-question
   call with a 1-word state may be the fallback, so measure its cost.
5. **Clef-flash price** from the Workers AI pricing page.
6. **Calibration eval.** On 30-50 Praxis questions that already have a gold grade,
   compare per-criterion `noul` from clef and jev against the current gemini/openai/
   claude majority. Record agreement and the probability spread on disagreements.
   This gates Phase 3, not Phases 1-2.

Write the findings into this file under each probe.

**Findings (live against Clef and Clef-flash on Workers AI, 2026-10-03; Jev via OpenRouter below).**

1. *Wrapper:* every success is `{"result":{model,answers,usage},"success":true,"errors":[],"messages":[]}`.
   The clef backend now REQUIRES it and refuses a bare body; Jev stays bare (Typesafe docs).
   `result.model` is the selector (`clef`, `clef-flash`), not a version id.
2. *Errors:* bad token -> 401 `{"result":null,...,"errors":[{"code":10000,"message":"Authentication error"}]}`;
   malformed question -> 400 code 5006 (the message is the vendor's generic "required properties"
   text, not the actual defect - local validation stays the useful error); 65 questions -> 422
   (`at most 64 items`); ~80k-token state -> 413 code 5021 (`exceeded this model context window
   limit (65536)`). None of these are retried (isRetryable is 429 + 5xx only). The quota 429
   (suspected code 3036) could not be triggered cheaply, so `TODO(phase0-probe2)` at `billingCode`
   stays open.
3. *Answer fields:* noul returns `noul` only (no `confidence`); choice and score return
   `confidence`; `score` is the probability-WEIGHTED value (e.g. 2.9573 on a 0-3 scale), not an
   index - consensus already works from `probabilities`. `usage.output_tokens` is 0 (input-only
   billing confirmed).
4. *Preflight:* the ev7 token is user-scoped: `GET /client/v4/user/tokens/verify` -> 200, while
   `/accounts/{id}/tokens/verify` -> 401 "Invalid API Token". A preflight must try the user
   endpoint and fall back to the account one; neither proves Workers AI permission (a valid
   R2-only token also verifies), so a one-noul call on a 1-word state (142 input tokens, about
   $0.00003 on clef) is the only real check. Not built yet.
5. *Clef-flash price:* $0.090/M input, no output price (https://developers.cloudflare.com/workers-ai/platform/pricing/,
   updated 2026-10-01).
6. *Calibration eval:* open.

The published input schema (`GET /accounts/{id}/ai/models/schema?model=@cf/cloudflare/clef`)
corrected four local rules, now enforced: question ids are 1-100 of `[A-Za-z0-9_.-]`; noul takes
OPTIONAL criteria `{"true": ..., "false": ...}`; choice takes 2-255 options; score takes 2-10
levels. `model` defaults to `clef` when omitted; `state` may be a string or structured data.
Not yet supported locally: criteria descriptions and `instructions` given as objects/arrays (the
schema allows them; ours are strings), and `images`.

**Jev via OpenRouter (live, 2026-10-03).** `POST https://openrouter.ai/api/alpha/decisions` with
`OPENROUTER_API_KEY` serves Jev 1.13 at the same $0.042/M (no markup; `usage.cost` matched the
table exactly). The response is the bare System One shape plus `usage.cost`, `id` and `provider`;
it reports the model as `typesafe/jev-1.13-20260917`. `jev-latest`, `~typesafe/jev-latest` and
`typesafe/jev-1.13` are all accepted; `model` is required (400 when omitted). Two identical calls
returned identical answers (Clef did not: see Phase 0 probe 3). 65 questions were accepted (only
Clef caps at 64); a ~40k-token state is a 400 `max_tokens_exceeded`; a bad key is 401 `User not
found.`. Errors are `{"error":{"message","code":<number>}}`, which `parseAPIError` now decodes
(it rendered raw JSON before). The jev decider uses this route when `TYPESAFE_API_KEY` is unset.

**Other decision models on OpenRouter (survey, 2026-10-03; same 3-question request to each).**
OpenRouter lists decision models as `output_modalities=decisions`. Six vendors as of this date:

| Slug | $/M in | Context | Live result |
|---|---|---|---|
| `typesafe/jev-1.13` | 0.042 | 32k | all 3 types; 2-decimal probabilities; deterministic |
| `liquid/d1` | 0.04 | 64k | all 3 types; routed the outage to **billing** (0.73), the only dissent |
| `upstage/solar-decide` | 0.05 | 512k | all 3 types; ~6x the input tokens for the same request |
| `inception/mercury-decide:free` | free | 32k | all 3 types |
| `togethercomputer/tev1-4b-experimental` | 0.042 | 32k | all 3 types despite a "choice only" description; open weights |
| `jaredpalmer/kev-4b` | 0.042 | 8k | all 3 types; reports output tokens (192) though they are free; open weights |
| `respan/span-01`, `-lite` | 0.02 / free | n/a | noul only (400 on choice/score); behaviour scoring, not general judgment |

None of these are reachable from `conclave decide` yet except jev. Routing any `vendor/model`
decider token through OpenRouter (the ADR-010 pattern, priced from the reported `usage.cost`)
is the natural next step and needs its own ADR (017).

## Phase 1: deciders + `conclave decide` — DONE (2026-10-02)

Shipped as below, with these resolutions: `--all-deciders` was not added (omitting the
decider list means every configured decider; the open question is closed); a single
positional argument is the decider list only when it is a comma list of decider or provider
names, otherwise the state; `Decision` carries no `Raw` (answers are re-encoded through the
typed `Answer`, which holds every wire field); `conclave init` saves decider keys without a
live check until probe 4 finds a spend-free one; the human table lives in
`internal/decide/render.go`, not `internal/output`. `TestClefAcceptsBareAndWorkersAIEnvelope`
pins both shapes until probe 1 completes.

**Interface** (`internal/providers/decider.go`). It lives in `providers` so it can
embed `apiBaseProvider` (ADR-004 retry, `BillingError`, `NewKeyRotator`/keyring).

```go
type Decider interface {
    Name() string
    DefaultModel() string
    IsAvailable() bool
    Decide(ctx context.Context, req DecisionRequest, model string) (*Decision, time.Duration, *Metrics, error)
}
type DecisionRequest struct {
    State     string              // positional/stdin/-f, assembled as for Query
    Questions map[string]Question // validated before any network call
}
type Question struct {
    Type         string // noul | choice | score
    Instructions string
    Choices      map[string]string // choice criteria
    Scale        []string          // score criteria (ordinal, index = score)
}
type Decision struct {
    Model   string            // as reported by the vendor (jev-1.13.0)
    Answers map[string]Answer // vendor answer, typed; Raw kept for --json fidelity
}
```

**Files.** `decide_systemone.go` (shared wire client plus per-backend URL/auth/unwrap),
`decide_jev.go`, `decide_clef.go` (registers `clef` and `clef-flash`), and
`registry.go:AllDeciders()` with `GetDecider(token)`. Before the "unknown provider"
error, `GetProvider` checks `GetDecider` and says "clef is a decision model; use
`conclave decide`". A transport suffix on a decider token is an error.

**Command** (`cmd/decide.go`):

```
conclave decide [deciders] [state] --questions q.yaml [-f file] [--json] [-t secs] [--cache]
conclave decide clef,jev -f ticket.txt --questions triage.yaml --json
conclave decide "..." --ask "Is this urgent?"      # no deciders = all configured; --ask = one noul named "q"
```

Default deciders: every available one. The questions file is YAML or JSON in the wire
shape (`criteria` stays a map for choice and a list for score). Local validation
before any spend:
- 1-64 questions, ids 1-100 of `[A-Za-z0-9_.-]` (schema, 2026-10-03)
- choice criteria: map of 2-255 entries
- score criteria: list of 2-10 entries
- noul: optional criteria `{"true": ..., "false": ...}` only
- a missing `instructions` is an error

**Consensus** (`internal/decide/consensus.go`). Pure functions over successful
decisions, so they can be table-tested:
- `choice`: average each option's probability across deciders (equal weight, ADR-016).
  `choice` = argmax of the averages. `votes` = argmax count per decider.
  `contested` = deciders' argmaxes differ.
- `score`: averaged distribution, plus `expected` (probability-weighted mean index) and
  `score` = argmax of the averaged distribution. `contested` = argmaxes differ by 1
  step or more.
- `noul`: mean value. `contested` = at least one decider >= 0.5 and at least one below (0.5 counts as yes).
- `agreement` per question = 1 - Jensen-Shannon divergence of the decider
  distributions (`noul` as a Bernoulli distribution). 1.0 means identical. This is
  reported, not thresholded; callers decide what is "too contested".

**`--json`** (additive, its own top level, not the panel `Result`):

```json
{ "deciders": {
    "clef": {"model": "@cf/cloudflare/clef", "status": "success", "duration_ms": 212,
             "answers": { "...vendor answer verbatim..." }, "metrics": {"input_tokens": 392, "cost_usd": 0.000094}},
    "jev":  {"status": "error", "error": "HTTP 401 ..."} },
  "consensus": {
    "department": {"type": "choice", "choice": "technical", "probabilities": {"technical": 0.83, "billing": 0.17, "sales": 0.0},
                   "votes": {"technical": 1}, "contested": false, "succeeded": 1, "requested": 2} },
  "meta": {"total_cost_usd": 0.00011, "duration_ms": 215} }
```

The exit policy reuses `cmd/exit.go`: 0 if at least one decider succeeded, 1 if all
failed, 130 on Ctrl-C. The envelope still prints on exit 1, as the panel path does.
Human output is a per-question table: question, consensus answer, probability,
agreement, and each decider's pick.

**Pricing** (`internal/pricing/deciders.go`): `{decider, model, in_per_m, out_per_m,
as_of, source_url}` rows. `conclave decide` prices through `pricing.DeciderCost` directly; `Catalog.CostOf` never sees deciders (ADR-016 keeps them out of the provider cost paths).
`conclave models` lists the rows with their `as_of`.

**Cache.** `cache.Key("api", decider, model, state, canonicalJSON(questions))`, with
sorted keys so map order cannot change the key. A comment at the construction site
explains the reuse of the `system` slot.

**Setup.** Add `TYPESAFE_API_KEY`, `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID`
to `providerSetupInfo` (`cmd/init.go`) and the keyring list. `CLOUDFLARE_ACCOUNT_ID` is
not a secret but is required; `IsAvailable` needs both.

**Tests** (each must be seen failing first):
- `decider_token_on_provider_path_points_to_decide`
- `decider_rejects_transport_suffix`
- `questions_invalid_choice_rejected_before_network` (httptest server asserts zero hits)
- `consensus_choice_averages_not_votes` (2 deciders: 0.9/0.1 and 0.4/0.6 → first option, contested)
- `consensus_noul_split_is_contested`
- `consensus_score_expected_vs_argmax`
- `consensus_one_failed_decider_still_reports`
- `cache_key_changes_with_questions_not_question_order`
- `clef_unwraps_workers_ai_envelope` (shape from Phase 0)
- `decider_priced_from_table_not_catalog`

**Docs in the same commits.** README section and Recent Updates, `docs/MODEL_REGISTRY.md`
(a Decision models table), CHANGELOG `[Unreleased]`, AGENTS.md: architecture tree
(`internal/decide/`), Important Files, and a Gotcha ("deciders are not providers;
`AllDeciders` vs `AllAPIProviders`; cache key reuses the system slot").

## Phase 2: batch with questions (about 1 day)

`conclave decide --batch items.jsonl --questions q.yaml --output out.jsonl` reuses
`internal/batch` workers, rate limiter, `--resume`, `--retries` and `--budget`. Each
item's `context` (plus optional `prompt`) becomes `state`. An output line carries `id`,
`consensus` and, under `--verbose`, per-decider answers. The batch processor gets a
second item function rather than a mode flag inside the existing one. Batch's
`-c`/`-g` implication does not apply, since deciders have no cheap tier or CLI.

Tests: `batch_decide_resume_skips_done_ids`, `batch_decide_budget_counts_input_only`.

## Phase 3: rubric scoring backend (folds into reliability plan Feature 5)

Feature 5's rubric criteria map 1:1 onto questions:

| Rubric `scale` | Question |
|---|---|
| `pass_fail` | `noul` (pass = value ≥ 0.5) |
| `1-5`, `0-10` | `score` with generated anchors, or the criterion's own `anchors:` list |
| enum (new, optional) | `choice` |

Add `--scorer decide:<deciders>` alongside the LLM path. Ties come from the averaged
probability instead of a vote count, so `tie_expected` and `tie_forbidden` apply only
when the averaged value sits inside a dead band (default 0.45-0.55, set in the rubric
front matter). Gated on the Phase 0 calibration eval. Before Feature 5 is built, add a
note to `docs/PLAN-reliability-judging.md` Feature 5 so its criteria schema keeps this
mapping possible: per-criterion `type` and `anchors` fields.

## Phase 4: decision verdict over an LLM panel (optional, after Feature 4)

`--judge decide:clef` asks a decider a fixed question set over the anonymised panel
(Feature 4's blind prompt): verdict `choice` (from `--verdicts A,B,C`) and an
`agreement` score. It fills `verdict.result` and `verdict.confidence`, plus a new
additive `verdict.probabilities`. An LLM judge may still run for the prose fields.
Oversized panels above the decider's context are refused before the panel spends
anything, as the slash-judge preflight does.

## Open questions

- ~~Does `--all-deciders` belong?~~ Resolved in Phase 1: no; omitting the list means all available.
- Should `conclave decide` accept a chat-model scorer in Phase 1 (an LLM asked the same
  questions, its answers marked `calibrated: false` and excluded from consensus) for
  side-by-side comparison? Useful for the Phase 0 eval; deferred unless that eval wants it.
