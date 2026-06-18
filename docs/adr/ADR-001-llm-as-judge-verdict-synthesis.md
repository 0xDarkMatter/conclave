---
status: accepted
date: 2025-12-24
supersedes: []
superseded-by: []
related: [ADR-003]
touches:
  - "internal/judge"
  - "cmd/root.go"
---

# ADR-001: LLM-as-judge verdict synthesis

## Decision (one sentence)

By default conclave sends every provider's response to a single judge model (default `claude`) that synthesizes one combined verdict, rather than returning the raw responses.

## Context

> Reconstructed retroactively on 2026-06-18 from git history (commit `a7b94b1`, conclave v1.0.0); the rationale below is inferred from the initial commit, the code, and `AGENTS.md` — it was not recorded at the time.

The product's value is "ask several models, get one trustworthy answer." Returning N raw responses pushes the comparison work onto the user. From v1.0.0 conclave instead designates a judge LLM that reads all responses and produces a synthesized verdict. This is why `--judge` defaults to `claude`, and why opt-outs exist: `--no-judge` (return raw blocks) and `--blind` (anonymize provider names so the judge isn't biased by brand).

## Alternatives considered

- **Return raw responses only, no synthesis.** Rejected as the default: it makes conclave a fan-out runner, not a decision tool. Preserved as `--no-judge` for piping/automation.
- **Programmatic merge (concatenate / vote / diff) instead of an LLM judge.** Rejected: the responses are free-form prose; a rule-based merge can't weigh correctness or reconcile disagreement the way a model can.
- **Always reveal provider identities to the judge.** Rejected as forced default: brand bias is a real risk for a judging model, so `--blind` exists to remove it on demand.

## Consequences

### Positive
- One actionable answer instead of N to reconcile.
- Judge is swappable per-run (`--judge`), and removable (`--no-judge`).
- `--blind` enables less biased synthesis.

### Negative
- Synthesis adds one extra LLM call (latency + cost) on top of the provider queries.
- The judge becomes a single point of opinion; a weak/biased judge degrades the result.

### Non-goals
- Does not mandate a specific judge model — that is configuration.
- Does not define the synthesis prompt's content (an implementation detail in `internal/judge`).

## See also

- `internal/judge/judge.go` — synthesis prompt and parsing.
- `cmd/root.go` — `--judge`, `--no-judge`, `--blind` wiring.
- [ADR-003](ADR-003-parallel-queries-with-per-provider-timeouts.md) — the parallel fan-out that feeds the judge.
