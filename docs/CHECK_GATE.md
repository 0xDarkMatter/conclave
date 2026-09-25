# The `make check` Gate

One command that must pass before anything lands, and the same command CI runs.

```bash
make check
```

## What it runs

| Step | What it asserts |
|---|---|
| `go vet ./...` | no vet diagnostics |
| `gofmt -l` over the `go list ./...` source files | prints nothing; a non-empty list is the failure (not `gofmt -l .`, which walks `.claude/worktrees/`; see below) |
| `go test ./...` | the suite passes |
| `go test -race ./...` | no data races |
| build + `conclave models --check` | no compiled default has drifted out of the catalog |

Ordered so the cheapest checks fail first.

## Two steps adapt instead of being weakened

**The race pass skips on Windows.** It needs a working cgo toolchain, which
Windows commonly lacks, and the concurrent code here is not platform-specific,
so a Linux pass catches the same races.

**The catalog step branches on an exit code**, not on message text:

| Exit | Meaning | Effect on the gate |
|---|---|---|
| 0 | every compiled default resolves | pass |
| 2 | real drift: a default is not in the catalog | **fail** |
| 3 | catalog unreachable, or `CONCLAVE_NO_PRICING=1` | skip with a note |

Do not go back to matching the error message. It used to, and a reworded error
would silently turn a hard failure into a skip, which is the failure mode that
lets a retired model id ship.

## Why CI does not re-list the steps

`.github/workflows/check.yml` runs `make check` verbatim on ubuntu-latest and
windows-latest. An earlier version spelled the steps out, and the two drifted
almost immediately: the workflow grew a race pass the Makefile did not have, and
marked the catalog check `continue-on-error`, so real drift could never fail a
pull request. One gate, one definition.

The workflow depends on GNU make existing on both runner images. If that ever
changes, the job fails loudly rather than quietly checking less.

## When drift is real

`conclave models --check` compares the compiled defaults and cheap-mode models
in `internal/config/config.go` against the live OpenRouter catalog. On a exit-2
failure, verify against the vendor before changing anything: the catalog is a
proxy for the vendor's list, not the list itself (ADR-009). If the model is
genuinely gone, update `internal/config/config.go` and the tables in
`docs/MODEL_REGISTRY.md` in the same commit.

## Landmines

### `gofmt` fails in a fresh worktree, and every file looks dirty

**Symptom.** `make check` fails at the gofmt step immediately after
`git worktree add`, `git clone`, or a rebase, listing nearly every `.go` file.
The diff for any of them looks empty.

**Cause.** `core.autocrlf=true` writes CRLF into a new checkout, and git can
populate the working tree before it honours the `.gitattributes` that pins
`eol=lf`. Every committed blob is already LF; only the working tree is wrong.
gofmt treats a CR as a formatting difference, so it flags the file while
`git status` reports nothing to commit.

**Fix.** Force a re-checkout so the attributes apply:

```bash
git rm --cached -r . && git reset --hard && git checkout-index -a -f
```

Nothing is lost: this rewrites tracked files from the index, which already
holds the correct LF content. Commit your work first if the tree is dirty, and
note that `git reset --hard` discards uncommitted changes.

**Do not** "fix" it by running `gofmt -w` over the tree. That rewrites the
files with the same content and hides the real cause, and the next fresh
checkout brings it straight back.

This bit three separate sessions during the change that introduced
`.gitattributes`, which is why it is written down here.

### `gofmt` lists files under `.claude/worktrees/` that are not yours

**Symptom.** `make check` fails at the gofmt step in the MAIN checkout, and
every listed path lives under `.claude/worktrees/<name>/`. Your own files are
clean; `gofmt -l cmd internal` prints nothing.

**Cause.** `gofmt -l <dir>` recurses into every subdirectory, dot-directories
included, and the root package's directory is the repository root. Any gate
that hands gofmt `.` (or the root package dir from `go list -f '{{.Dir}}'`)
therefore sweeps every nested worktree other sessions have checked out, and
those trees are often CRLF for the reason in the landmine above.

**Fix.** Already in the Makefile since 44a53e2: `fmt-check` enumerates
`GoFiles`, `TestGoFiles` and `XTestGoFiles` via `go list` and hands gofmt
files, never directories. If you touch that target, keep it file-based and
re-prove both directions: a planted unformatted file must fail the gate, and a
clean tree must pass. `go vet ./...` and `go test ./...` are unaffected
because the `./...` pattern skips dot-directories on its own.

**Do not** work around it by deleting or reformatting another session's
worktree. Those trees are that session's private state; see
`~/.claude/rules/worktree-boundaries.md`.
