// Package sleepguard keeps the PC from sleeping during a trading day's watch
// hours when the operator turns the Settings switch on (issue #863). A
// sleeping PC stops the app and with it the market-data collection, which
// cannot be filled in afterwards.
//
// The decision logic - hold from 08:45 JST on a trading day until the close,
// give the PC back outside those hours, when the switch is turned off and when
// the app exits, do nothing while the switch is off - is OS independent and
// talks to the OS only through Keeper, so it is tested on every platform. The
// Windows Keeper (SetThreadExecutionState) is in keeper_windows.go; other
// systems get a Keeper that does nothing.
package sleepguard

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/healthalert"
)

// CheckInterval is how often the Guard re-evaluates the switch and the clock.
// It bounds how late the PC is held after 08:45 or given back after the close.
const CheckInterval = 30 * time.Second

// Keeper is the OS side: it asks the OS not to sleep and hands that back.
// Hold and Release are idempotent.
type Keeper interface {
	// Hold makes the OS keep the system awake (the display may still turn
	// off) until Release.
	Hold() error
	// Release gives the power management back to the OS settings.
	Release() error
}

// Settings reads the Settings switch; Source of internal/config/sleepguardsettings
// implements it. It is read at every check, so a change needs no restart.
type Settings interface {
	SleepGuardEnabled(ctx context.Context) (bool, error)
}

// Config wires a Guard. Now defaults to time.Now.
type Config struct {
	Settings Settings
	Keeper   Keeper
	Now      func() time.Time
}

// Guard holds the PC awake while the switch is on and a trading day's watch
// window (healthalert.KabuWindow: 08:45 JST up to the close, lunch break
// included) is open.
type Guard struct {
	cfg  Config
	mu   sync.Mutex
	held bool
}

// New returns a Guard that holds nothing yet.
func New(cfg Config) *Guard {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Guard{cfg: cfg}
}

// Check evaluates the switch and the clock once and moves the Keeper to
// match. A failure to read the switch leaves the state as it is; a failed
// Hold or Release is retried at the next check.
func (g *Guard) Check(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	enabled, err := g.cfg.Settings.SleepGuardEnabled(ctx)
	if err != nil {
		return fmt.Errorf("sleepguard: read switch: %w", err)
	}
	now := g.cfg.Now()
	_, inWindow := healthalert.KabuWindow(now)
	switch want := enabled && inWindow; {
	case want && !g.held:
		if err := g.cfg.Keeper.Hold(); err != nil {
			return fmt.Errorf("sleepguard: hold: %w", err)
		}
		g.held = true
		slog.Info("sleep guard: keeping the PC awake for the trading day", "at", now)
	case !want && g.held:
		return g.release(now, releaseReason(enabled))
	}
	return nil
}

// Close gives the PC back if it is held; the app calls it on exit (Run does
// when its context ends). It is safe to call more than once.
func (g *Guard) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.held {
		return nil
	}
	return g.release(g.cfg.Now(), "app exit")
}

func (g *Guard) release(now time.Time, reason string) error {
	if err := g.cfg.Keeper.Release(); err != nil {
		return fmt.Errorf("sleepguard: release: %w", err)
	}
	g.held = false
	slog.Info("sleep guard: gave the PC back to the OS power settings", "reason", reason, "at", now)
	return nil
}

// releaseReason names why a held PC is given back at a check: the switch
// went off, or the watch window is over.
func releaseReason(enabled bool) string {
	if !enabled {
		return "switch off"
	}
	return "outside the trading hours"
}

// Run checks every CheckInterval, the first time at once, until ctx is done,
// and then gives the PC back.
func (g *Guard) Run(ctx context.Context) {
	defer func() {
		if err := g.Close(); err != nil {
			slog.Error("sleep guard: release on exit failed", "error", err)
		}
	}()
	wait := time.Duration(0)
	safego.Loop(ctx, "sleep guard check", func() time.Duration {
		w := wait
		wait = CheckInterval
		return w
	}, g.Check)
}
