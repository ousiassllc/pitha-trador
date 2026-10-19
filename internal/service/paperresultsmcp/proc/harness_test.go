package proc_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// secretKey is the API key every test seeds. Nothing the application hands
// the process, logs or shows may contain it.
const secretKey = "pitha-mcp-TESTKEY-must-never-leak-0123456789"

func tailnetIfaces() ([]net.Addr, error) {
	return []net.Addr{&net.IPNet{IP: net.ParseIP("100.101.102.103"), Mask: net.CIDRMask(32, 32)}}, nil
}

// fakeChild is a Child the test controls.
type fakeChild struct {
	done      chan struct{}
	once      sync.Once
	exit      proc.Exit
	terminate atomic.Int32
	// hold, when set, keeps Terminate waiting until it is closed.
	hold chan struct{}
}

func newFakeChild() *fakeChild { return &fakeChild{done: make(chan struct{})} }

func (c *fakeChild) Done() <-chan struct{} { return c.done }
func (c *fakeChild) Exit() proc.Exit       { return c.exit }
func (c *fakeChild) Terminate(time.Duration) {
	c.terminate.Add(1)
	if c.hold != nil {
		<-c.hold
	}
	c.end(proc.Exit{Code: -1})
}

// end makes the process exit (a crash, or the result of Terminate).
func (c *fakeChild) end(e proc.Exit) {
	c.once.Do(func() { c.exit = e; close(c.done) })
}

// rig is a Manager over a temp settings file, a fake executable, a fake
// spawner and a fake "is the port open" probe.
type rig struct {
	t     *testing.T
	m     *proc.Manager
	path  string
	exe   string
	logs  *bytes.Buffer
	mu    sync.Mutex
	specs []proc.Spec
	kids  []*fakeChild
	// open decides what Dial reports; nil: closed.
	open atomic.Pointer[func() bool]
	// spawnErr makes Spawn fail.
	spawnErr error
	env      []string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, path: filepath.Join(t.TempDir(), settings.FileName), logs: &bytes.Buffer{}}
	r.exe = filepath.Join(filepath.Dir(r.path), "paper-results-mcp")
	if err := os.WriteFile(r.exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	r.env = []string{"PATH=/bin", "PITHA_DB_PATH=/tmp/x.db"}
	r.m = proc.New(proc.Options{
		ConfigPath:     r.path,
		ExePath:        func() (string, error) { return r.exe, nil },
		InterfaceAddrs: tailnetIfaces,
		Dial: func(context.Context, string) error {
			if f := r.open.Load(); f != nil && (*f)() {
				return nil
			}
			return errors.New("connection refused")
		},
		Env: func() []string { return r.env },
		Spawn: func(s proc.Spec) (proc.Child, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.spawnErr != nil {
				return nil, r.spawnErr
			}
			k := newFakeChild()
			r.specs, r.kids = append(r.specs, s), append(r.kids, k)
			return k, nil
		},
		ReadyWait: 50 * time.Millisecond, HandoverWait: 200 * time.Millisecond,
		PollInterval: 5 * time.Millisecond, StopGrace: time.Second, StartGrace: time.Hour,
		AddressWait: 30 * time.Millisecond, AddressPoll: 2 * time.Millisecond,
	})
	t.Cleanup(r.m.Shutdown)
	return r
}

func (r *rig) seed(s settings.Settings) {
	r.t.Helper()
	if err := settings.Save(r.path, s); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) seedValid() {
	r.seed(settings.Settings{Enabled: true, Listen: settings.DefaultListen, APIKey: secretKey})
}

func (r *rig) listening(f func() bool) { r.open.Store(&f) }

func (r *rig) spawns() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.specs)
}

func (r *rig) child(i int) *fakeChild {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kids[i]
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
