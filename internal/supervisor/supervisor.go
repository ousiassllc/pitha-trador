// Package supervisor restarts a child process after an abnormal exit
// (docs/requirements/non-functional.md §3: プロセス監視による自動再起動).
//
// cmd/desktop runs it when launched with --supervise (the Windows Startup
// shortcut the installer creates passes that flag): the supervising
// process re-executes its own binary as the real Wails app and relaunches
// it whenever it exits non-zero or is killed. A clean exit (code 0 -
// operator quit, self-update quit, startup-failure dialog) ends
// supervision so it never fights an intentional shutdown.
package supervisor

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"time"
)

// Flag is the command-line flag that selects supervisor mode.
const Flag = "--supervise"

// Config tunes the restart policy. Zero values take the defaults below.
type Config struct {
	// MinBackoff is the delay before the first restart after a crash.
	MinBackoff time.Duration
	// MaxBackoff caps the exponentially growing restart delay, so a crash
	// loop (e.g. an unparsable config) cannot spin the CPU.
	MaxBackoff time.Duration
	// StableAfter is how long a run must last to count as healthy; a
	// healthy run resets the backoff to MinBackoff.
	StableAfter time.Duration

	// now and after are test seams for the clock.
	now   func() time.Time
	after func(time.Duration) <-chan time.Time
}

func (c Config) withDefaults() Config {
	if c.MinBackoff <= 0 {
		c.MinBackoff = time.Second
	}
	if c.MaxBackoff < c.MinBackoff {
		c.MaxBackoff = 5 * time.Minute
	}
	if c.StableAfter <= 0 {
		c.StableAfter = time.Minute
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.after == nil {
		c.after = time.After
	}
	return c
}

// Run calls run repeatedly. run blocks for one child lifetime and returns
// nil for a clean exit (supervision ends, Run returns nil) or an error for
// a crash (Run waits the backoff delay, then calls run again). Run returns
// ctx.Err() once ctx is cancelled.
func Run(ctx context.Context, cfg Config, run func(ctx context.Context) error) error {
	cfg = cfg.withDefaults()
	backoff := cfg.MinBackoff
	for {
		started := cfg.now()
		err := run(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			return nil
		}
		if cfg.now().Sub(started) >= cfg.StableAfter {
			backoff = cfg.MinBackoff
		}
		slog.Error("supervisor: child crashed; restarting", "error", err, "restart_in", backoff.String())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-cfg.after(backoff):
		}
		backoff = min(backoff*2, cfg.MaxBackoff)
	}
}

// ExecRun returns a run func for Run that starts path with args, wires
// the child to this process's stdio and waits for it. A non-zero exit
// code or a signal kill is returned as an error; exit code 0 as nil.
func ExecRun(path string, args ...string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // G204: path is this application's own executable, supplied by the caller (not user input)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		err := cmd.Run()
		if err == nil {
			return nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr
		}
		return err // could not start the child at all: also retried
	}
}

// ChildArgs returns args without the supervisor Flag, i.e. the argument
// list the supervised child is started with. The second result reports
// whether Flag was present (supervisor mode requested).
func ChildArgs(args []string) ([]string, bool) {
	child := make([]string, 0, len(args))
	found := false
	for _, a := range args {
		if a == Flag {
			found = true
			continue
		}
		child = append(child, a)
	}
	return child, found
}
