package providers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestCLITimeoutBoundsShimGrandchild defends the per-provider deadline
// (ADR-003) against the npm-shim process tree. On Windows `codex` and `gemini`
// are .cmd shims: exec.CommandContext kills cmd.exe on timeout, but node keeps
// the inherited stdout pipe open and Run() waited for it, so a 1s timeout
// returned after the grandchild finished on its own (measured 19s). The shell
// shim below reproduces the same shape on every OS: the parent is killed, the
// sleeping child still holds stdout.
func TestCLITimeoutBoundsShimGrandchild(t *testing.T) {
	dir := t.TempDir()
	writeExec(t, filepath.Join(dir, "slowcli"), "#!/bin/sh\nsleep 20\n")
	writeExec(t, filepath.Join(dir, "slowcli.cmd"), "@echo off\r\nping -n 20 127.0.0.1 >nul\r\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	_, err := runCommand(ctx, "slowcli", nil, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	// 1s deadline + cliWaitDelay + scheduling slack. Before the fix this was
	// the full 20s of the grandchild.
	if limit := time.Second + cliWaitDelay + 3*time.Second; elapsed > limit {
		t.Fatalf("runCommand returned after %s; the %s deadline did not bound the shim's child (limit %s)", elapsed, time.Second, limit)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error should say the deadline passed so the user sees a timeout, not %q", err)
	}
	if runtime.GOOS == "windows" {
		t.Logf("returned after %s on Windows", elapsed)
	}
}
