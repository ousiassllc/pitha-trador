package proc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

func TestStatus_FailuresAreRecordedAndShownByKind(t *testing.T) {
	t.Run("crash after start", func(t *testing.T) {
		r := newRig(t)
		r.seedValid()
		if err := r.m.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		r.child(0).end(proc.Exit{Code: 3})
		eventually(t, "the exit to be recorded", func() bool { return r.m.Status().Failure != nil })
		f := r.m.Status().Failure
		if f.Kind != proc.FailExited || f.ExitCode != 3 || r.m.Status().Running {
			t.Fatalf("status %+v failure %+v", r.m.Status(), f)
		}
	})
	t.Run("port in use", func(t *testing.T) {
		r := newRig(t)
		r.seedValid()
		_ = r.m.Start(context.Background())
		r.child(0).end(proc.Exit{Code: 1, Stderr: `listen tcp 127.0.0.1:48110: bind: address already in use`})
		eventually(t, "the exit to be recorded", func() bool { return r.m.Status().Failure != nil })
		if k := r.m.Status().Failure.Kind; k != proc.FailPortInUse {
			t.Fatalf("kind = %q", k)
		}
	})
	t.Run("process refuses to listen (exit 0)", func(t *testing.T) {
		r := newRig(t)
		r.seedValid()
		_ = r.m.Start(context.Background())
		r.child(0).end(proc.Exit{Code: 0, Stderr: "paper-results-mcp: refusing to listen: no auth configured"})
		eventually(t, "the exit to be recorded", func() bool { return r.m.Status().Failure != nil })
		if k := r.m.Status().Failure.Kind; k != proc.FailRefused {
			t.Fatalf("kind = %q", k)
		}
	})
	t.Run("exit during the readiness wait", func(t *testing.T) {
		r := newRig(t)
		r.seedValid()
		r.m = rigWithSpawn(r, func(k *fakeChild) { k.end(proc.Exit{Code: 2}) })
		if err := r.m.Start(context.Background()); !errors.Is(err, proc.ErrExitedEarly) {
			t.Fatalf("Start = %v, want ErrExitedEarly", err)
		}
		if f := r.m.Status().Failure; f == nil || f.ExitCode != 2 {
			t.Fatalf("failure = %+v", f)
		}
	})
	t.Run("executable missing", func(t *testing.T) {
		r := newRig(t)
		r.seedValid()
		r.exe += ".gone"
		if err := r.m.Start(context.Background()); !errors.Is(err, proc.ErrExeMissing) {
			t.Fatalf("Start = %v", err)
		}
		if f := r.m.Status().Failure; f == nil || f.Kind != proc.FailExeMissing {
			t.Fatalf("failure = %+v", f)
		}
	})
	t.Run("OS refuses to start it", func(t *testing.T) {
		r := newRig(t)
		r.seedValid()
		r.spawnErr = errors.New("fork/exec: permission denied, secret path C:\\Users\\x")
		err := r.m.Start(context.Background())
		if !errors.Is(err, proc.ErrSpawnFailed) || contains(err.Error(), "secret path") {
			t.Fatalf("Start = %v; want ErrSpawnFailed without the OS text", err)
		}
		if f := r.m.Status().Failure; f == nil || f.Kind != proc.FailSpawn {
			t.Fatalf("failure = %+v", f)
		}
	})
	t.Run("a successful start clears the failure", func(t *testing.T) {
		r := newRig(t)
		r.seedValid()
		_ = r.m.Start(context.Background())
		r.child(0).end(proc.Exit{Code: 9})
		eventually(t, "the exit to be recorded", func() bool { return r.m.Status().Failure != nil })
		if err := r.m.Restart(context.Background()); err != nil {
			t.Fatal(err)
		}
		if r.m.Status().Failure != nil || !r.m.Status().Running {
			t.Fatalf("status after restart: %+v", r.m.Status())
		}
	})
	t.Run("an operator stop is not a failure", func(t *testing.T) {
		r := newRig(t)
		r.seedValid()
		_ = r.m.Start(context.Background())
		_ = r.m.Stop(context.Background())
		if r.m.Status().Failure != nil {
			t.Fatalf("a stop became a failure: %+v", r.m.Status().Failure)
		}
	})
}

// rigWithSpawn returns a Manager like r's whose spawner runs hook on each
// new child before handing it over.
func rigWithSpawn(r *rig, hook func(*fakeChild)) *proc.Manager {
	return proc.New(proc.Options{
		ConfigPath: r.path, ExePath: func() (string, error) { return r.exe, nil }, InterfaceAddrs: tailnetIfaces,
		Dial: func(context.Context, string) error { return errors.New("closed") },
		Spawn: func(proc.Spec) (proc.Child, error) {
			k := newFakeChild()
			hook(k)
			return k, nil
		},
		ReadyWait: 500_000_000, PollInterval: 5_000_000,
	})
}

// A restart that could not start again is refused before the running process
// is touched: it must not turn into a silent stop.
func TestRestart_RefusedSettingsLeaveTheRunningProcessAlone(t *testing.T) {
	r := newRig(t)
	r.seedValid()
	if err := r.m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.seed(settings.Settings{Enabled: false, Listen: settings.DefaultListen, APIKey: secretKey})
	if err := r.m.Restart(context.Background()); !errors.Is(err, proc.ErrDisabled) {
		t.Fatalf("Restart = %v, want ErrDisabled", err)
	}
	if r.child(0).terminate.Load() != 0 || !r.m.Status().Running {
		t.Fatal("a refused restart stopped the running process")
	}
}

func TestStatus_ADeadProcessIsNeverReportedRunning(t *testing.T) {
	r := newRig(t)
	r.seedValid()
	if err := r.m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.child(0).end(proc.Exit{Code: 5})
	// No waiting for the watcher: the read itself must notice the exit.
	if st := r.m.Status(); st.Running || st.Failure == nil || st.Failure.ExitCode != 5 {
		t.Fatalf("status right after the exit: %+v failure %+v", st, st.Failure)
	}
}

// While Stop waits for the process to go, the screen must not read the
// still-open port as "a process started elsewhere".
func TestStatus_StoppingIsReportedWhileTheProcessIsBeingEnded(t *testing.T) {
	r := newRig(t)
	r.seedValid()
	slow := newFakeChild()
	r.m = rigWithChild(r, slow)
	if err := r.m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	slow.hold = make(chan struct{})
	stopped := make(chan error, 1)
	go func() { stopped <- r.m.Stop(context.Background()) }()
	eventually(t, "Stopping", func() bool { return r.m.Status().Stopping })
	if st := r.m.Status(); st.Running {
		t.Fatalf("Running while stopping: %+v", st)
	}
	close(slow.hold)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if st := r.m.Status(); st.Stopping || st.Running {
		t.Fatalf("after Stop: %+v", st)
	}
}

func rigWithChild(r *rig, c proc.Child) *proc.Manager {
	return proc.New(proc.Options{
		ConfigPath: r.path, ExePath: func() (string, error) { return r.exe, nil }, InterfaceAddrs: tailnetIfaces,
		Dial:      func(context.Context, string) error { return errors.New("closed") },
		Spawn:     func(proc.Spec) (proc.Child, error) { return c, nil },
		ReadyWait: 20 * time.Millisecond, PollInterval: 2 * time.Millisecond, StopGrace: time.Second,
	})
}
