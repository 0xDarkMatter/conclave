//go:build windows

package providers

import (
	"os/exec"
	"strconv"
)

// killTree ends the whole process tree rooted at cmd.
//
// On Windows `codex` and `gemini` are npm .cmd shims: Go starts cmd.exe, which
// starts node. exec.CommandContext's default Cancel kills only cmd.exe, so node
// keeps running after the deadline (and, until cliWaitDelay existed, kept
// Run() blocked on the stdout pipe it inherited). taskkill /T walks the tree.
// If taskkill itself is unavailable, fall back to killing the direct child so
// the default behaviour is never worse than before.
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	if err := exec.Command("taskkill", "/T", "/F", "/PID", pid).Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
