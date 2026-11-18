//go:build !windows

package codexwire

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup starts cmd as the leader of a new process group, so the whole
// tree it starts (the sandbox's children included) can be stopped together.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup kills cmd's whole process group. It is cmd.Cancel's body, so it runs
// while the process has not been waited for yet and its pid still names the group.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}
