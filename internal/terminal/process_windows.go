//go:build windows

package terminal

import (
	"os/exec"
	"strconv"

	pty "github.com/aymanbagabas/go-pty"
)

// terminateProcessTree force-kills cmd's entire descendant tree, not just
// cmd itself. Process.Kill() (used to be the only thing Session.Close did)
// only terminates the single tracked PID via TerminateProcess — any
// children it spawned (very common: these are usually Node.js CLIs invoked
// through a shell, or a CLI that shells out to sub-tools) are orphaned and
// keep running in the background. Over a long SessionHub session with many
// tabs started/stopped, those orphans accumulate and degrade the whole
// machine's responsiveness — surfacing as exactly the kind of system-wide
// sluggishness that gets misread as "keys aren't registering."
// taskkill's own process-tree walk (via each process's recorded parent PID)
// works regardless of how the child was spawned, unlike a process-group
// based kill, which is why internal/automation's equivalent helper uses the
// same approach for command-step subprocesses.
func terminateProcessTree(cmd *pty.Cmd) {
	if cmd.Process == nil {
		return
	}
	killer := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	_ = killer.Run()
	_ = cmd.Process.Kill()
}
