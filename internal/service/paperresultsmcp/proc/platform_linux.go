//go:build linux

package proc

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

// sysProcAttr makes the kernel kill the child when the application dies,
// however it dies (crash, SIGKILL): without it a killed app leaves the MCP
// running as an orphan.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

// lockThread: Pdeathsig fires when the *thread* that started the child exits,
// not the process. Locking this goroutine to its thread and never unlocking
// makes the thread exit with the goroutine, i.e. once the child has been
// waited for.
func lockThread() { runtime.LockOSThread() }

func afterStart(*exec.Cmd) error { return nil }

func requestStop(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
