# The `make check` Gate

One command that must pass before anything lands, and the same command CI runs.

```bash
make check
```

## What it runs

| Step | What it asserts |
|---|---|
| `go vet ./...` | no vet diagnostics |
| `gofmt -l .` | prints nothing; a non-empty list is the failure |
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
