package proc_test

import (
	"context"
	"errors"
	"net"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// The application's start-up hook starts the MCP only for enabled settings
// with a key and a permitted address; everything else leaves the default
// (off) alone and spawns nothing.
func TestAutoStart_StartsOnlyWhenEnabledWithKeyAndPermittedAddress(t *testing.T) {
	const tailnet = "100.101.102.103:48110"
	tests := []struct {
		name  string
		seed  func(r *rig)
		spawn int
	}{
		{"enabled, key, loopback", func(r *rig) { r.seedValid() }, 1},
		{"enabled, key, tailnet address on this host", func(r *rig) {
			r.seed(settings.Settings{Enabled: true, Listen: tailnet, APIKey: secretKey})
		}, 1},
		{"disabled", func(r *rig) {
			r.seed(settings.Settings{Enabled: false, Listen: settings.DefaultListen, APIKey: secretKey})
		}, 0},
		{"enabled, empty key", func(r *rig) {
			r.seed(settings.Settings{Enabled: true, Listen: settings.DefaultListen})
		}, 0},
		{"enabled, key, 0.0.0.0", func(r *rig) {
			r.seed(settings.Settings{Enabled: true, Listen: "0.0.0.0:48110", APIKey: secretKey})
		}, 0},
		{"enabled, key, LAN address", func(r *rig) {
			r.seed(settings.Settings{Enabled: true, Listen: "192.168.1.5:48110", APIKey: secretKey})
		}, 0},
		{"enabled, key, hostname", func(r *rig) {
			r.seed(settings.Settings{Enabled: true, Listen: "example.com:48110", APIKey: secretKey})
		}, 0},
		{"enabled, key, tailnet address not on this host", func(r *rig) {
			r.seed(settings.Settings{Enabled: true, Listen: "100.64.0.9:48110", APIKey: secretKey})
		}, 0},
		{"no settings file (the default install)", func(*rig) {}, 0},
		{"unparseable settings file", func(r *rig) {
			if err := os.WriteFile(r.path, []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			tc.seed(r)
			r.m.AutoStart(context.Background())
			if got := r.spawns(); got != tc.spawn {
				t.Fatalf("spawned %d processes, want %d", got, tc.spawn)
			}
			if st := r.m.Status(); st.Running != (tc.spawn == 1) {
				t.Errorf("Running = %v, want %v", st.Running, tc.spawn == 1)
			}
		})
	}
}

func TestAutoStart_InsecureSettingsFileIsNotStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced on Windows (settings.Load)")
	}
	r := newRig(t)
	r.seedValid()
	if err := os.Chmod(r.path, 0o644); err != nil {
		t.Skip("cannot loosen file permissions here")
	}
	r.m.AutoStart(context.Background())
	if r.spawns() != 0 {
		t.Fatal("started from a world-readable settings file, which the MCP itself refuses to read")
	}
}

// After the supervisor restarts a crashed app, the previous process may still
// be closing its port: AutoStart waits for it, and still never doubles up.
func TestAutoStart_WaitsForThePreviousPortToCloseThenStarts(t *testing.T) {
	r := newRig(t)
	r.seedValid()
	var probes atomic.Int32
	r.listening(func() bool { return probes.Add(1) <= 3 }) // answers 3 times, then the old process is gone
	r.m.AutoStart(context.Background())
	if r.spawns() != 1 {
		t.Fatalf("spawned %d, want 1 once the old process had gone", r.spawns())
	}
}

func TestAutoStart_AProcessThatKeepsAnsweringIsNotDoubled(t *testing.T) {
	r := newRig(t)
	r.seedValid()
	r.listening(func() bool { return true }) // started outside the application
	r.m.AutoStart(context.Background())
	if r.spawns() != 0 {
		t.Fatalf("spawned %d next to a process already listening", r.spawns())
	}
}

func TestAutoStart_NothingStartsAfterShutdown(t *testing.T) {
	r := newRig(t)
	r.seedValid()
	r.m.Shutdown()
	r.m.AutoStart(context.Background())
	if r.spawns() != 0 {
		t.Fatal("AutoStart spawned after Shutdown")
	}
}

// The tailnet interface may come up after the application does (right after
// logon): AutoStart waits for the address instead of giving up at once.
func TestAutoStart_WaitsForATailnetAddressToAppear(t *testing.T) {
	const tailnet = "100.101.102.103:48110"
	var calls atomic.Int32
	r := newRig(t)
	r.seed(settings.Settings{Enabled: true, Listen: tailnet, APIKey: secretKey})
	r.m = proc.New(proc.Options{
		ConfigPath: r.path, ExePath: func() (string, error) { return r.exe, nil },
		InterfaceAddrs: func() ([]net.Addr, error) {
			if calls.Add(1) < 4 { // Tailscale is not up yet for the first few looks
				return nil, nil
			}
			return tailnetIfaces()
		},
		Dial: func(context.Context, string) error { return errors.New("closed") },
		Spawn: func(s proc.Spec) (proc.Child, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			k := newFakeChild()
			r.specs, r.kids = append(r.specs, s), append(r.kids, k)
			return k, nil
		},
		ReadyWait: 20 * time.Millisecond, PollInterval: 2 * time.Millisecond, AddressPoll: 2 * time.Millisecond, AddressWait: 5 * time.Second,
	})
	t.Cleanup(r.m.Shutdown)
	r.m.AutoStart(context.Background())
	if r.spawns() != 1 {
		t.Fatalf("spawned %d, want 1 once the tailnet address appeared", r.spawns())
	}
}

func TestAutoStart_GivesUpWhenTheTailnetAddressNeverAppears(t *testing.T) {
	r := newRig(t)
	r.seed(settings.Settings{Enabled: true, Listen: "100.64.0.9:48110", APIKey: secretKey}) // not on this host
	r.m = proc.New(proc.Options{
		ConfigPath: r.path, ExePath: func() (string, error) { return r.exe, nil }, InterfaceAddrs: tailnetIfaces,
		Dial: func(context.Context, string) error { return errors.New("closed") },
		Spawn: func(proc.Spec) (proc.Child, error) {
			t.Error("spawned without a valid address")
			return newFakeChild(), nil
		},
		AddressPoll: 2 * time.Millisecond, AddressWait: 30 * time.Millisecond,
	})
	r.m.AutoStart(context.Background())
}

// Shutdown is never kept waiting by an AutoStart that waits for an address.
func TestAutoStart_ShutdownEndsTheWaitForAnAddress(t *testing.T) {
	r := newRig(t)
	r.seed(settings.Settings{Enabled: true, Listen: "100.64.0.9:48110", APIKey: secretKey})
	polled := make(chan struct{}, 1)
	m := proc.New(proc.Options{
		ConfigPath: r.path,
		ExePath:    func() (string, error) { return r.exe, nil },
		InterfaceAddrs: func() ([]net.Addr, error) {
			select {
			case polled <- struct{}{}:
			default:
			}
			return tailnetIfaces()
		},
		Dial:        func(context.Context, string) error { return errors.New("closed") },
		Spawn:       func(proc.Spec) (proc.Child, error) { t.Error("spawned after Shutdown"); return newFakeChild(), nil },
		AddressPoll: 2 * time.Millisecond, AddressWait: time.Hour,
	})
	done := make(chan struct{})
	go func() { m.AutoStart(context.Background()); close(done) }()
	<-polled // AutoStart is inside its wait for the address
	m.Shutdown()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AutoStart kept waiting for an address after Shutdown")
	}
}

// Shutdown is never kept waiting by the hand-over wait either.
func TestAutoStart_ShutdownEndsTheHandoverWait(t *testing.T) {
	r := newRig(t)
	r.seedValid()
	polled := make(chan struct{}, 1)
	m := proc.New(proc.Options{
		ConfigPath:     r.path,
		ExePath:        func() (string, error) { return r.exe, nil },
		InterfaceAddrs: tailnetIfaces,
		Dial: func(context.Context, string) error { // the old process never lets go
			select {
			case polled <- struct{}{}:
			default:
			}
			return nil
		},
		Spawn:        func(proc.Spec) (proc.Child, error) { t.Error("spawned next to a listener"); return newFakeChild(), nil },
		HandoverWait: time.Hour, PollInterval: 2 * time.Millisecond,
	})
	done := make(chan struct{})
	go func() { m.AutoStart(context.Background()); close(done) }()
	<-polled // AutoStart is inside the hand-over wait
	shut := make(chan struct{})
	go func() { m.Shutdown(); close(shut) }()
	select {
	case <-shut:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown blocked behind the hand-over wait")
	}
	<-done
}
