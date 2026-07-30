//go:build !windows

package terminal

import (
	"syscall"

	pty "github.com/aymanbagabas/go-pty"
)

// terminateProcessTree force-kills cmd's entire process group, not just the
// single tracked PID — see the Windows counterpart's doc comment for why
// this matters (orphaned descendants accumulating over a long session).
// go-pty's own unix Start() unconditionally sets SysProcAttr.Setsid = true
// for every PTY-attached command, which as a side effect makes the child's
// process group ID equal its own PID; signalling -pid therefore reaches
// that whole group, however many processes it spawned.
func terminateProcessTree(cmd *pty.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
