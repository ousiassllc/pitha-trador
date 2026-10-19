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

const (
	// createNoWindow keeps the console-subsystem MCP from opening a window.
	createNoWindow = 0x08000000
	// createSuspended starts the process with its main thread stopped, so it
	// runs no instruction before afterStart has put it into the Job Object.
	createSuspended = 0x00000004
	// processSuspendResume is the access right NtResumeProcess needs.
	processSuspendResume = 0x0800
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNoWindow | createSuspended}
}

func lockThread() {}

var (
	jobMu     sync.Mutex
	jobHandle windows.Handle
)

// killOnCloseJob returns the process-wide Job Object whose
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE limit makes Windows terminate every
// process in it when the last handle closes. Only this process holds a
// handle (it is not inheritable and is never closed), so the handle closes
// when the application ends however it ends - crash, kill, power-off of the
// session - and the MCP cannot outlive it. A failed creation is not cached:
// the next start tries again.
func killOnCloseJob() (windows.Handle, error) {
	jobMu.Lock()
	defer jobMu.Unlock()
	if jobHandle != 0 {
		return jobHandle, nil
	}
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create job object: %w", err)
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(h)
		return 0, fmt.Errorf("set job object limits: %w", err)
	}
	jobHandle = h
	return h, nil
}

var ntResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")

// afterStart puts the suspended process into the kill-on-close job and then
// lets it run. The process executes nothing before it is in the job, so there
// is no window in which the application could die and leave it behind. A
// failure is returned so the caller kills the process: an MCP that could
// outlive the application is never left running.
func afterStart(cmd *exec.Cmd) error {
	job, err := killOnCloseJob()
	if err != nil {
		return err
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|processSuspendResume, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fmt.Errorf("open process: %w", err)
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		return fmt.Errorf("assign process to job object: %w", err)
	}
	if status, _, _ := ntResumeProcess.Call(uintptr(h)); status != 0 {
		return fmt.Errorf("resume process: NTSTATUS %#x", status)
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
