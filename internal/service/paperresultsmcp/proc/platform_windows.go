//go:build windows

package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createNoWindow keeps the console-subsystem MCP from opening a window.
const createNoWindow = 0x08000000

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNoWindow}
}

func lockThread() {}

var (
	jobOnce   sync.Once
	jobHandle windows.Handle
	jobErr    error
)

// killOnCloseJob returns the process-wide Job Object whose
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE limit makes Windows terminate every
// process in it when the last handle closes. Only this process holds a
// handle (it is not inheritable and is never closed), so the handle closes
// when the application ends however it ends - crash, kill, power-off of the
// session - and the MCP cannot outlive it.
func killOnCloseJob() (windows.Handle, error) {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			jobErr = fmt.Errorf("create job object: %w", err)
			return
		}
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			_ = windows.CloseHandle(h)
			jobErr = fmt.Errorf("set job object limits: %w", err)
			return
		}
		jobHandle = h
	})
	return jobHandle, jobErr
}

// afterStart puts the started process into the kill-on-close job. A failure
// is returned so the caller kills the process: an MCP that could outlive the
// application is never left running. (The window between CreateProcess and
// this call is microseconds; a kill of the application inside it is the one
// case that leaves an orphan, and the next start refuses to double up on it.)
func afterStart(cmd *exec.Cmd) error {
	job, err := killOnCloseJob()
	if err != nil {
		return err
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fmt.Errorf("open process: %w", err)
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		return fmt.Errorf("assign process to job object: %w", err)
	}
	return nil
}

// requestStop ends the process: Windows has no SIGTERM for a console process
// without a window. The MCP is a read-only reader of a WAL database, so
// terminating it loses nothing.
func requestStop(p *os.Process) error {
	if p == nil {
		return errors.New("no process")
	}
	return p.Kill()
}
