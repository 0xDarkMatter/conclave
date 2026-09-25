package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestInterruptedQueryCleansUpAndSkipsTheJudge runs a real single query
// against a fake grok CLI that hangs, and cancels the command's context the
// way Ctrl-C now does. Before, Ctrl-C killed the process, so grok's temp
// prompt file (and claude's temp working directory) were never removed. The
// run must end quickly as an interruption, with nothing left in TMP.
func TestInterruptedQueryCleansUpAndSkipsTheJudge(t *testing.T) {
	bin := t.TempDir()
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "grok"), []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "grok.cmd"), []byte("@echo off\r\nping -n 30 127.0.0.1 >nul\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	t.Setenv("TMPDIR", tmp)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CONCLAVE_NO_PRICING", "1")
	t.Setenv("CONCLAVE_TIMEOUT", "")

	oldStdin, oldSkip, oldGeneral := flagNoStdin, flagSkipPreflight, flagGeneral
	flagNoStdin, flagSkipPreflight, flagGeneral = true, true, false
	t.Cleanup(func() { flagNoStdin, flagSkipPreflight, flagGeneral = oldStdin, oldSkip, oldGeneral })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rootCmd.SetContext(ctx)
	t.Cleanup(func() { rootCmd.SetContext(context.Background()) })
	go func() {
		time.Sleep(1500 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := runConclave(rootCmd, []string{"grok", "hi"})
	if !errors.Is(err, errInterrupted) {
		t.Fatalf("got %v, want errInterrupted", err)
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("interrupted run took %s; cancellation did not stop the CLI", d)
	}
	left, _ := filepath.Glob(filepath.Join(tmp, "conclave-*"))
	if len(left) > 0 {
		t.Fatalf("interrupted run left temp files behind: %v", left)
	}
}
