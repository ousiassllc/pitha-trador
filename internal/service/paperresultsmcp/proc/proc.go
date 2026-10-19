// Package proc starts, stops and watches the read-only Paper results MCP as a
// child process of the application (issue #795,
// docs/architecture/paper-results-mcp-lifecycle.md).
//
// The MCP stays a separate process: this package only launches the
// paper-results-mcp executable. It hands over no database connection, no
// open file and no application environment, and never the API key - the
// process reads that from its 0600 settings file (safety conditions 1 and 3).
package proc

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// Why Start (or AutoStart) did not start the process. Each is a fixed reason
// the Settings screen can word; none carries settings content.
var (
	ErrAlreadyRunning   = errors.New("paper-results-mcp: already running")
	ErrAlreadyListening = errors.New("paper-results-mcp: the address already answers (a process started outside the application)")
	ErrNotRunning       = errors.New("paper-results-mcp: not running")
	ErrNotManaged       = errors.New("paper-results-mcp: running, but not started by the application")
	ErrExeMissing       = errors.New("paper-results-mcp: executable not found")
	ErrClosed           = errors.New("paper-results-mcp: the application is shutting down")
	ErrExitedEarly      = errors.New("paper-results-mcp: the process exited right after starting")
	ErrSpawnFailed      = errors.New("paper-results-mcp: the operating system could not start the process")

	ErrSettingsUnusable = errors.New("paper-results-mcp: settings file unusable")
	ErrDisabled         = errors.New("paper-results-mcp: disabled")
	ErrNoKey            = errors.New("paper-results-mcp: no API key")
	ErrAccessIncomplete = errors.New("paper-results-mcp: access header verification without id or secret")
)

// Options configure a Manager. ConfigPath, ExePath and Dial are required; the
// rest default to production values and exist for tests.
type Options struct {
	// ConfigPath is the MCP's settings file.
	ConfigPath string
	// ExePath returns where paper-results-mcp is expected.
	ExePath func() (string, error)
	// Dial reports whether a TCP connection to address succeeds.
	Dial func(ctx context.Context, address string) error
	// InterfaceAddrs lists this host's addresses for settings.ValidateListen.
	InterfaceAddrs settings.InterfaceAddrs

	// Env is the child's environment (default ChildEnv(os.Environ())).
	Env func() []string
	// Args are the child's arguments (default `-config <ConfigPath>`).
	Args func(configPath string) []string
	// Spawn starts the process (default: os/exec).
	Spawn Spawner
	// Now is the clock (default time.Now).
	Now func() time.Time

	// StartGrace: how long a started process may take to open its port before
	// the state becomes an error (default 10 s). ReadyWait: how long Start
	// waits for the port before returning (3 s). StopGrace: SIGTERM to kill
	// (5 s). HandoverWait: how long AutoStart waits for a previous process's
	// port to close (5 s). PollInterval: probe interval (100 ms).
	StartGrace, ReadyWait, StopGrace, HandoverWait, PollInterval time.Duration
}

func (o *Options) fill() {
	if o.Env == nil {
		o.Env = func() []string { return ChildEnv(os.Environ()) }
	}
	if o.Args == nil {
		o.Args = func(path string) []string { return []string{"-config", path} }
	}
	if o.Spawn == nil {
		o.Spawn = osSpawn
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	for _, d := range []struct {
		v *time.Duration
		d time.Duration
	}{{&o.StartGrace, 10 * time.Second}, {&o.ReadyWait, 3 * time.Second}, {&o.StopGrace, 5 * time.Second},
		{&o.HandoverWait, 5 * time.Second}, {&o.PollInterval, 100 * time.Millisecond}} {
		if *d.v == 0 {
			*d.v = d.d
		}
	}
}

// Manager owns at most one MCP child process.
type Manager struct {
	opts Options
	// opMu serialises Start, Stop, Restart, AutoStart and Shutdown: two
	// operations never race to spawn (no double start).
	opMu   sync.Mutex
	closed atomic.Bool

	// mu guards the fields below; the watcher and View take it, never opMu,
	// so the status stays readable while an operation waits.
	mu        sync.Mutex
	child     Child
	stopping  bool
	startedAt time.Time
	listen    string
	cfgStamp  time.Time
	failure   *Failure
}

// New returns a Manager over opts.
func New(opts Options) *Manager {
	opts.fill()
	return &Manager{opts: opts}
}

// Status is a snapshot for the Settings screen. It never holds the key.
type Status struct {
	// Running: the application started a process that is alive.
	Running bool
	// Listen is the address that process was started for (empty if none).
	Listen    string
	StartedAt time.Time
	// Starting: the process is within StartGrace of its start, so not
	// listening yet is not an error yet.
	Starting bool
	// ConfigChanged: the settings file changed after the process started, so
	// it still runs on the old settings until restarted.
	ConfigChanged bool
	// Failure is the last start failure or unexpected exit, nil if none.
	Failure *Failure
}

// Status reports the current process state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Failure: m.failure}
	if m.child != nil && !m.stopping {
		st.Running, st.Listen, st.StartedAt = true, m.listen, m.startedAt
		st.Starting = m.opts.Now().Sub(m.startedAt) < m.opts.StartGrace
		info, err := os.Stat(m.opts.ConfigPath)
		st.ConfigChanged = err == nil && !info.ModTime().Equal(m.cfgStamp)
	}
	return st
}

// ClearFailure forgets the last failure; the screen calls it when the operator
// changes the settings that caused it.
func (m *Manager) ClearFailure() {
	m.mu.Lock()
	m.failure = nil
	m.mu.Unlock()
}

// Check reports why cfg (loaded with loadErr) must not be started, or nil.
// It applies the rules the process applies to itself when it starts
// (settings.ValidateListen, an API key, Access options), so the application
// never spawns a process that would only refuse to listen.
func (m *Manager) Check(cfg settings.Settings, loadErr error) error {
	switch {
	case loadErr != nil:
		return ErrSettingsUnusable
	case !cfg.Enabled:
		return ErrDisabled
	case cfg.APIKey == "":
		return ErrNoKey
	case cfg.VerifyAccessHeaders && (cfg.AccessClientID == "" || cfg.AccessClientSecret == ""):
		return ErrAccessIncomplete
	}
	return settings.ValidateListen(cfg.Listen, m.opts.InterfaceAddrs)
}

// Start launches the process when the stored settings allow it, waits briefly
// for its port, and returns. It refuses to start a second process: when the
// application already started one, or when the address already answers.
func (m *Manager) Start(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.start(ctx)
}

func (m *Manager) start(ctx context.Context) error {
	if m.closed.Load() {
		return ErrClosed
	}
	m.mu.Lock()
	alive := m.child != nil
	m.mu.Unlock()
	if alive {
		return ErrAlreadyRunning
	}
	cfg, loadErr := settings.Load(m.opts.ConfigPath)
	if err := m.Check(cfg, loadErr); err != nil {
		return err
	}
	listen := cfg.Listen
	cfg = settings.Settings{} // the key is not kept past the check
	exe, err := m.opts.ExePath()
	if err == nil {
		err = statExe(exe)
	}
	if err != nil {
		m.fail(&Failure{Kind: FailExeMissing, ExitCode: -1, At: m.opts.Now()})
		return ErrExeMissing
	}
	if m.opts.Dial(ctx, listen) == nil {
		return ErrAlreadyListening
	}
	stamp := configStamp(m.opts.ConfigPath)
	child, err := m.opts.Spawn(Spec{Exe: exe, Args: m.opts.Args(m.opts.ConfigPath), Env: m.opts.Env()})
	if err != nil {
		m.fail(&Failure{Kind: FailSpawn, ExitCode: -1, At: m.opts.Now()})
		slog.WarnContext(ctx, "paper-results-mcp: could not start the process")
		return ErrSpawnFailed
	}
	m.mu.Lock()
	m.child, m.stopping, m.failure = child, false, nil
	m.startedAt, m.listen, m.cfgStamp = m.opts.Now(), listen, stamp
	m.mu.Unlock()
	go m.watch(child)
	slog.InfoContext(ctx, "paper-results-mcp: process started", "listen", listen)
	return m.awaitReady(ctx, child, listen)
}

// awaitReady waits for the port or an early exit, up to ReadyWait; a process
// that is merely slow stays "starting" and Start returns nil.
func (m *Manager) awaitReady(ctx context.Context, child Child, listen string) error {
	deadline := time.NewTimer(m.opts.ReadyWait)
	defer deadline.Stop()
	tick := time.NewTicker(m.opts.PollInterval)
	defer tick.Stop()
	for {
		select {
		case <-child.Done():
			m.exited(child)
			return ErrExitedEarly
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return nil
		case <-tick.C:
			if m.opts.Dial(ctx, listen) == nil {
				return nil
			}
		}
	}
}

// watch records an exit the operator did not ask for as the failure to show.
func (m *Manager) watch(child Child) {
	<-child.Done()
	m.exited(child)
}

// exited is idempotent: the watcher and awaitReady both call it.
func (m *Manager) exited(child Child) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.child != child {
		return // stopped by Stop/Shutdown, or already recorded
	}
	m.child = nil
	if m.stopping {
		return
	}
	exit := child.Exit()
	m.failure = classify(exit, m.opts.Now())
	slog.Warn("paper-results-mcp: process exited unexpectedly", "kind", m.failure.Kind, "exit_code", exit.Code)
}

func (m *Manager) fail(f *Failure) {
	m.mu.Lock()
	m.failure = f
	m.mu.Unlock()
}
