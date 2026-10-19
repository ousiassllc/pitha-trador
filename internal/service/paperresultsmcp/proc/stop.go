package proc

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// Stop ends the process the application started. A process started outside
// the application is not touched (ErrNotManaged).
func (m *Manager) Stop(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.stop(ctx)
}

func (m *Manager) stop(ctx context.Context) error {
	m.mu.Lock()
	child := m.child
	if child == nil {
		m.failure = nil
		m.mu.Unlock()
		if listen := m.startableListen(); listen != "" && m.opts.Dial(ctx, listen) == nil {
			return ErrNotManaged
		}
		return ErrNotRunning
	}
	m.stopping = true
	m.mu.Unlock()
	child.Terminate(m.opts.StopGrace)
	m.mu.Lock()
	if m.child == child {
		m.child = nil
	}
	m.stopping, m.failure = false, nil
	m.mu.Unlock()
	slog.InfoContext(ctx, "paper-results-mcp: process stopped")
	return nil
}

// startableListen is the stored listen address when the stored settings could
// be started, "" otherwise. A stop request uses it to tell "nothing runs" from
// "something runs that the application did not start".
func (m *Manager) startableListen() string {
	cfg, err := settings.Load(m.opts.ConfigPath)
	if m.Check(cfg, err) != nil {
		return ""
	}
	return cfg.Listen
}

// Restart stops the process the application started (if any) and starts it
// again with the current settings.
func (m *Manager) Restart(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if err := m.stop(ctx); err != nil && !errors.Is(err, ErrNotRunning) {
		return err
	}
	return m.start(ctx)
}

// AutoStart is the application's start-up hook: it starts the process when
// the stored settings are enabled, hold an API key and a permitted address,
// and does nothing otherwise (the default is off). After the supervisor
// restarts a crashed application the previous process may still be closing its
// port, so it first waits (up to HandoverWait) for the address to stop
// answering; one that keeps answering is a process started outside the
// application, and no second one is started. It never fails the application:
// outcomes are logged by reason only.
func (m *Manager) AutoStart(ctx context.Context) {
	cfg, loadErr := settings.Load(m.opts.ConfigPath)
	if err := m.Check(cfg, loadErr); err != nil {
		slog.InfoContext(ctx, "paper-results-mcp: not starting with the application", "reason", err.Error())
		return
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.awaitHandover(ctx, cfg.Listen)
	switch err := m.start(ctx); {
	case err == nil:
	case errors.Is(err, ErrAlreadyListening), errors.Is(err, ErrClosed), errors.Is(err, context.Canceled):
		slog.InfoContext(ctx, "paper-results-mcp: not starting with the application", "reason", err.Error())
	default:
		slog.WarnContext(ctx, "paper-results-mcp: could not start with the application", "reason", err.Error())
	}
}

func (m *Manager) awaitHandover(ctx context.Context, listen string) {
	deadline := time.NewTimer(m.opts.HandoverWait)
	defer deadline.Stop()
	tick := time.NewTicker(m.opts.PollInterval)
	defer tick.Stop()
	for !m.closed.Load() && m.opts.Dial(ctx, listen) == nil {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-tick.C:
		}
	}
}

// Shutdown stops the process and refuses every later start. The application
// calls it when it exits; a crash is covered by the operating system
// (platform_*.go), not by this method.
func (m *Manager) Shutdown() {
	m.closed.Store(true)
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	running := m.child != nil
	m.mu.Unlock()
	if !running {
		return
	}
	if err := m.stop(context.Background()); err == nil {
		slog.Info("paper-results-mcp: stopped with the application")
	}
}

func statExe(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return os.ErrNotExist
	}
	return nil
}

func configStamp(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
