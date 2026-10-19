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
		var spawned atomic.Int32
		other := proc.New(proc.Options{
			ConfigPath:     r.path,
			ExePath:        func() (string, error) { return r.exe, nil },
			InterfaceAddrs: tailnetIfaces,
			Dial:           func(context.Context, string) error { return nil },
			Spawn:          func(proc.Spec) (proc.Child, error) { spawned.Add(1); return newFakeChild(), nil },
		})
		if err := other.Start(context.Background()); !errors.Is(err, proc.ErrAlreadyListening) {
			t.Fatalf("a second application instance: Start = %v, want ErrAlreadyListening", err)
		}
		if spawned.Load() != 0 {
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
