//go:build !linux && !windows

package proc

import (
	"os"
	"os/exec"
	"syscall"
)

// On other Unix systems there is no kernel hook that kills the child with the
// application. A graceful exit still stops it (Manager.Shutdown); after a
// crash the leftover process keeps listening and the next start sees the
// address answer and does not start a second one. Production is Windows
// (platform_windows.go).
func sysProcAttr() *syscall.SysProcAttr { return nil }

func lockThread() {}

func afterStart(*exec.Cmd) error { return nil }

func requestStop(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
