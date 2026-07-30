//go:build windows

package automation

import (
	"os/exec"
	"strconv"
	"syscall"
)

func prepareProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func terminateProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// taskkill is a Windows system executable and /T terminates descendants.
	// The fallback still terminates the direct process if taskkill is unavailable.
	killer := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	_ = killer.Run()
	_ = cmd.Process.Kill()
}
