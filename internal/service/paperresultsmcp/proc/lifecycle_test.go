package proc_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

func TestStart_RefusesWhatTheProcessWouldRefuse(t *testing.T) {
	tests := []struct {
		name string
		s    settings.Settings
		want error
	}{
		{"disabled", settings.Settings{Listen: settings.DefaultListen, APIKey: secretKey}, proc.ErrDisabled},
		{"no key", settings.Settings{Enabled: true, Listen: settings.DefaultListen}, proc.ErrNoKey},
		{"all interfaces", settings.Settings{Enabled: true, Listen: "0.0.0.0:48110", APIKey: secretKey}, settings.ErrListenNotAllowed},
		{"access verification without secret", settings.Settings{Enabled: true, Listen: settings.DefaultListen, APIKey: secretKey, VerifyAccessHeaders: true, AccessClientID: "id"}, proc.ErrAccessIncomplete},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.seed(tc.s)
			if err := r.m.Start(context.Background()); !errors.Is(err, tc.want) {
				t.Fatalf("Start = %v, want %v", err, tc.want)
			}
			if r.spawns() != 0 {
				t.Fatal("spawned a process that would only refuse to listen")
			}
		})
	}
}

// No double start: a second Start, concurrent Starts, and an address that
// already answers all end with exactly the one process.
func TestStart_NeverStartsASecondProcess(t *testing.T) {
	t.Run("second call", func(t *testing.T) {
		r := newRig(t)
		r.seedValid()
		if err := r.m.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := r.m.Start(context.Background()); !errors.Is(err, proc.ErrAlreadyRunning) {
			t.Fatalf("second Start = %v, want ErrAlreadyRunning", err)
		}
		if r.spawns() != 1 {
			t.Fatalf("spawned %d", r.spawns())
		}
	})
	t.Run("concurrent calls", func(t *testing.T) {
		r := newRig(t)
		r.seedValid()
		var wg sync.WaitGroup
		var ok atomic.Int32
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if r.m.Start(context.Background()) == nil {
					ok.Add(1)
				}
			}()
		}
		wg.Wait()
		if r.spawns() != 1 || ok.Load() != 1 {
			t.Fatalf("spawned %d, %d Starts succeeded; want 1 and 1", r.spawns(), ok.Load())
		}
	})
	t.Run("address already answers", func(t *testing.T) {
		r := newRig(t)
		r.seedValid()
		r.listening(func() bool { return true })
		if err := r.m.Start(context.Background()); !errors.Is(err, proc.ErrAlreadyListening) {
			t.Fatalf("Start = %v, want ErrAlreadyListening", err)
		}
		if r.spawns() != 0 {
			t.Fatal("spawned next to a listener")
		}
	})
	t.Run("application restart while the old one runs", func(t *testing.T) {
		// A second Manager (the restarted application) sees the address answer.
		r := newRig(t)
		r.seedValid()
		if err := r.m.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		r.listening(func() bool { return true })
		other := newRig(t)
		other.path, other.exe = r.path, r.exe
		other.listening(func() bool { return true })
		if err := other.m.Start(context.Background()); err == nil {
			t.Fatal("a second application instance started a second process")
		}
	})
}

func TestStopAndRestart(t *testing.T) {
	r := newRig(t)
	r.seedValid()
	if err := r.m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.child(0).terminate.Load() != 1 || r.m.Status().Running {
		t.Fatal("Stop did not end the process")
	}
	if err := r.m.Stop(context.Background()); !errors.Is(err, proc.ErrNotRunning) {
		t.Fatalf("Stop with nothing running = %v, want ErrNotRunning", err)
	}
	if err := r.m.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.m.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.spawns() != 3 || r.child(1).terminate.Load() != 1 {
		t.Fatalf("spawns = %d, second child terminated %d times; want 3 and 1", r.spawns(), r.child(1).terminate.Load())
	}
	if !r.m.Status().Running {
		t.Fatal("not running after Restart")
	}
}

func TestStop_LeavesAProcessStartedElsewhereAlone(t *testing.T) {
	r := newRig(t)
	r.seedValid()
	r.listening(func() bool { return true })
	if err := r.m.Stop(context.Background()); !errors.Is(err, proc.ErrNotManaged) {
		t.Fatalf("Stop = %v, want ErrNotManaged", err)
	}
}

// The application exits: the MCP is stopped with it and cannot be started
// again.
func TestShutdown_StopsTheProcessAndRefusesLaterStarts(t *testing.T) {
	r := newRig(t)
	r.seedValid()
	if err := r.m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.m.Shutdown()
	if r.child(0).terminate.Load() != 1 {
		t.Fatal("the process was not terminated by Shutdown")
	}
	select {
	case <-r.child(0).Done():
	default:
		t.Fatal("the process is still alive after Shutdown")
	}
	if err := r.m.Start(context.Background()); !errors.Is(err, proc.ErrClosed) {
		t.Fatalf("Start after Shutdown = %v, want ErrClosed", err)
	}
	if err := r.m.Restart(context.Background()); !errors.Is(err, proc.ErrClosed) {
		t.Fatalf("Restart after Shutdown = %v, want ErrClosed", err)
	}
	r.m.Shutdown() // idempotent
}

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
