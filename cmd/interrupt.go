package cmd

// Ctrl-C handling for every command. Lives in its own file because root.go
// imports internal/context under the name "context", which shadows the
// standard library package this file needs.
//
// Contract: the FIRST Ctrl-C cancels the command's context instead of killing
// the process, so in-flight work unwinds through its defers (grok's prompt
// file, claude's temp working directory, the CLI process-tree kill) and the
// partial panel is still rendered. The signal is then handed back to the
// runtime, so a SECOND Ctrl-C kills immediately. An interrupted run exits 130.
// Batch keeps its own handler for its "finishing in-flight items" message;
// both receive the signal and both only cancel.

import (
	"context"
	"errors"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

// ExitInterrupted is the conventional exit status for a SIGINT (128 + 2).
const ExitInterrupted = 130

// interruptContext returns a context cancelled by the first Ctrl-C, and a
// stop function to call once the command has returned.
func interruptContext() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	go func() {
		<-ctx.Done()
		// Unregister as soon as the first signal lands, so the next Ctrl-C
		// gets the default behaviour (terminate) instead of being swallowed.
		stop()
	}()
	return ctx, stop
}

// runInterrupted reports whether the command's context was cancelled by
// Ctrl-C. Tests call RunE directly with no context, hence the nil check.
func runInterrupted(cmd *cobra.Command) bool {
	ctx := cmd.Context()
	return ctx != nil && ctx.Err() != nil
}

// errInterrupted is what a single-query run returns after rendering the
// partial panel of an interrupted run.
var errInterrupted = errors.New("interrupted: no verdict was produced, and any panel responses above are partial")

// exitCodeFor maps a command's result to the process exit status. An
// interruption wins over whatever error the cancelled work produced ("context
// canceled" from a provider, a failed judge), since that error is a symptom.
func exitCodeFor(err error, interrupted bool) int {
	if err == nil {
		return 0
	}
	if interrupted {
		return ExitInterrupted
	}
	var coded exitCoder
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 1
}
