package cmd

// Process exit codes, so a caller can act on WHY a command failed instead of
// grepping its message. `make check` and CI both depend on telling a real
// model-drift failure apart from an unreachable catalog: the first must fail
// the gate, the second must only skip it. Matching on message strings for that
// was brittle in exactly the way that matters, since a reworded error silently
// turns a hard failure into a skip.
const (
	// ExitDrift means a compiled default is absent from the catalog. Real,
	// actionable, and must fail a build.
	ExitDrift = 2
	// ExitCatalogUnavailable means the answer is unknown: offline, the feed is
	// down, or CONCLAVE_NO_PRICING is set. Never a reason to fail a build.
	ExitCatalogUnavailable = 3
)

// exitCoder is implemented by errors that carry a specific process exit code.
type exitCoder interface {
	error
	ExitCode() int
}

// exitError attaches an exit code to an error without changing its message.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) ExitCode() int { return e.code }
func (e *exitError) Unwrap() error { return e.err }

// withExitCode tags err so Execute exits with code instead of the default 1.
// A nil error stays nil, so it is safe to wrap a result directly.
func withExitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return &exitError{code: code, err: err}
}
