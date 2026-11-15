//go:build windows

package codexwire

import (
	"os/exec"
	"strconv"
	"syscall"
)

// createNewProcessGroup is CREATE_NEW_PROCESS_GROUP; createNoWindow keeps a
// console-subsystem child from opening a window.
const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
)

func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | createNoWindow}
}

// killGroup stops cmd's whole process tree (taskkill /T), then the process itself.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run() // #nosec G204 -- fixed program, numeric pid
	return cmd.Process.Kill()
}
