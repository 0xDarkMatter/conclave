//go:build !windows

package providers

import "os/exec"

// killTree ends the child on context cancellation. On POSIX the CLIs conclave
// wraps are real executables, not shell shims, so killing the direct child is
// enough; cliWaitDelay still bounds Run() if a grandchild holds a pipe open.
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
