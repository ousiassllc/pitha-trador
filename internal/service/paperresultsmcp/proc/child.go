package proc

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

// Spec is what Manager asks a Spawner to start. Args and Env carry no secret.
type Spec struct {
	Exe  string
	Args []string
	Env  []string
}

// Exit is how a Child ended.
type Exit struct {
	Code   int
	Stderr string // bounded tail; used for classification only
}

// Child is a started MCP process.
type Child interface {
	// Done is closed once the process has exited.
	Done() <-chan struct{}
	// Exit is valid after Done is closed.
	Exit() Exit
	// Terminate asks the process to stop and, after grace, kills it. It
	// returns once the process is gone.
	Terminate(grace time.Duration)
}

// Spawner starts a process. osSpawn is the production one; tests inject their
// own to watch what the application hands over.
type Spawner func(Spec) (Child, error)

type osChild struct {
	cmd  *exec.Cmd
	tail *tailBuffer
	done chan struct{}
	exit Exit
}

// osSpawn starts spec as a child of this process. Only the standard streams
// are inherited (stdin and stdout are the null device, stderr is a bounded
// tail), so neither the application's database connection nor any other open
// file reaches the process; Go closes every other descriptor on exec and
// passes an explicit handle list on Windows (safety condition 1).
func osSpawn(spec Spec) (Child, error) {
	c := &osChild{
		cmd:  exec.CommandContext(context.Background(), spec.Exe, spec.Args...), //nolint:gosec // G204: Exe is the fixed paper-results-mcp beside the application; Args carry only the settings path
		tail: &tailBuffer{},
		done: make(chan struct{}),
	}
	// A nil Env would make os/exec hand over the application's whole
	// environment: an allow-list that matched nothing must stay empty.
	c.cmd.Env = append([]string{}, spec.Env...)
	c.cmd.Stderr = c.tail
	c.cmd.SysProcAttr = sysProcAttr()
	started := make(chan error, 1)
	go c.run(started)
	if err := <-started; err != nil {
		return nil, err
	}
	return c, nil
}

// run owns the process from Start to Wait on one goroutine, so lockThread can
// tie the child's lifetime to this goroutine's thread where the OS needs it.
func (c *osChild) run(started chan<- error) {
	lockThread()
	err := c.cmd.Start()
	if err == nil {
		if err = afterStart(c.cmd); err != nil {
			// Not bound to our lifetime: never leave it running.
			_ = c.cmd.Process.Kill()
		}
	}
	started <- err
	if c.cmd.Process == nil {
		return
	}
	waitErr := c.cmd.Wait()
	c.exit = Exit{Code: -1, Stderr: c.tail.String()}
	if c.cmd.ProcessState != nil {
		c.exit.Code = c.cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		c.exit.Code = -1
	}
	close(c.done)
}

func (c *osChild) Done() <-chan struct{} { return c.done }
func (c *osChild) Exit() Exit            { return c.exit }

func (c *osChild) Terminate(grace time.Duration) {
	select {
	case <-c.done:
		return
	default:
	}
	_ = requestStop(c.cmd.Process)
	select {
	case <-c.done:
	case <-time.After(grace):
		_ = c.cmd.Process.Kill()
		<-c.done
	}
}
