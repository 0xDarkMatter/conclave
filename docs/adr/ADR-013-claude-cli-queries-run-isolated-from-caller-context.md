---
status: accepted
date: 2026-09-13
supersedes: []
superseded-by: []
extends: [ADR-002]
related: [ADR-001, ADR-002, ADR-007]
touches:
  - "internal/providers/claude.go"
  - "internal/providers/provider.go"
  - "internal/providers/gemini.go"
  - "internal/providers/claude_cli_json_noise_test.go"
---

# ADR-013: Claude CLI queries run isolated from the caller's project context

## Decision (one sentence)

Every `claude --print` panel query runs with `--strict-mcp-config`, `--setting-sources user` and `--no-session-persistence`, in an empty directory conclave owns rather than the caller's cwd, and never with `--bare`; context reaches the panel only through `-f` / stdin.

## Context

Conclave wraps the Claude Code CLI as one of its panel members (ADR-002). Invoked as a plain subprocess, `claude --print` behaves like an interactive session started in the caller's directory: it auto-discovers that project's `CLAUDE.md`, loads project and local settings (including hooks), and starts every configured MCP server, both project-scoped and the account's claude.ai connectors.

Two live observations on 2026-09-13 made this a decision rather than a default:

- A bare `"hi"` sent through conclave answered with a description of the caller's git worktree and cited a SessionStart hook. The panel member was answering a different question from the one the other panel members received, and the judge (ADR-001) would have weighed it as if it were the same question.
- MCP startup printed `Client.listTools() called but server does not advertise tools capability - returning empty list` on stdout ahead of the JSON envelope, which broke the response parser, and the handshakes added several seconds to a one-word answer.

Every provider in a judge panel must see the same prompt and nothing else. Whatever repo the user happens to run conclave from is not part of the prompt.

## Alternatives considered

- **`--bare`.** Does everything the chosen flags do in one switch, and also skips `CLAUDE.md` discovery. Rejected: it disables OAuth and keychain auth, so claude would stop running on the subscription and start billing an API key. The subscription-only rule for claude and codex (AGENTS.md Gotcha 7) is a hard constraint.
- **Flags only, keep the caller's cwd.** Simpler, but `CLAUDE.md` auto-discovery is not controlled by any of the three flags, so project instructions would still leak into answers. Rejected; the neutral cwd is what closes that hole.
- **Leave it, tell users to run conclave from a clean directory.** Not enforceable, and the failure is silent: nothing in the output says the answer was shaped by local context. Rejected.
- **Pass `--mcp-config` with an empty server list instead of `--strict-mcp-config`.** Equivalent effect, one more file to manage. `--strict-mcp-config` with no `--mcp-config` yields zero servers with no artefact.

## Consequences

### Positive
- Panel members answer the same prompt regardless of where conclave is invoked.
- No MCP handshakes per query: faster cold start and no MCP diagnostics on stdout.
- No resumable session written to disk for every panel query.

### Negative
- A user who relied on the caller's `CLAUDE.md` or project settings reaching claude loses that. The supported path is explicit: `-f` files and stdin.
- User-level settings still load (`--setting-sources user`), so a user-scope hook could still fire. Accepted: user settings are the user's own choice and travel with them; project settings are an accident of cwd.
- The neutral directory lives under the OS temp dir. If it cannot be created the query falls back to the caller's cwd with the flags still applied, so the degradation is partial, not total.

### Non-goals
- This does not isolate other CLI providers. gemini-cli and codex load their own config by their own rules; when one of them is caught leaking context it gets its own record.
- This does not change API mode (`-g`), which never had a cwd to leak.

## See also

- `internal/providers/claude.go` — `claudeIsolationArgs` and `claudeWorkDir`, with the guard comment naming each flag's job.
- `TestClaudeCLIRunsIsolatedFromCallerContext` in `internal/providers/claude_cli_json_noise_test.go` — reads the argv and cwd the CLI actually sees; fails if a flag is dropped, `--bare` appears, or the cwd is the caller's.
- AGENTS.md Gotchas 7 (subscription auth) and 13 (stdout noise, the symptom that led here).
- ADR-007 — precedent for a CLI-mode provider being routed around the CLI's own defaults.
