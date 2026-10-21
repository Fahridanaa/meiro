//go:build windows

package proc

import (
	"os/exec"
	"strconv"
	"syscall"
)

// hardenCommand makes cancellation kill the child's whole process tree.
func hardenCommand(cmd *exec.Cmd) {
	hideWindow(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		hideWindow(kill)
		return kill.Run()
	}
}

// hideWindow prevents a child process from flashing a console window.
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}
