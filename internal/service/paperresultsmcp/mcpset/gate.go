package mcpset

import (
	"context"
	"database/sql"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// Gate is the application's side of the same state the Writer changes: it
// reads and flips the write switch (the Settings screen's toggle), lists the
// audit log for the Settings panel, and (through Audited) records the Settings
// screen's own changes to whitelisted keys, so settings_diff and
// the audit log see every change of those keys whoever made it.
type Gate struct {
	db       *sql.DB
	settings *system.RuntimeSettingsRepository
	now      func() time.Time
}

// NewGate returns a Gate over the application's database.
func NewGate(db *sql.DB) *Gate {
	return &Gate{db: db, settings: system.NewRuntimeSettingsRepository(db), now: time.Now}
}

// Enabled reports the write switch (on while nothing is stored).
func (g *Gate) Enabled(ctx context.Context) (bool, error) { return WriteEnabled(ctx, g.settings) }

// SetEnabled stores the switch and records it as a GUI change.
func (g *Gate) SetEnabled(ctx context.Context, enabled bool) error {
	return g.inTx(ctx, func(tx *sql.Tx, now time.Time) error {
		store := system.NewRuntimeSettingsRepositoryOn(tx)
		was, err := WriteEnabled(ctx, store)
		if err != nil {
			return err
		}
		if err := store.Set(ctx, mcpwritesettings.KeyWriteEnabled, mcpwritesettings.Text(enabled), now); err != nil {
			return err
		}
		_, err = insertAudit(ctx, tx, now, switchEntry(ActorGUI, "", was, enabled))
		return err
	})
}

// inTx runs fn in one application-database transaction (see runInTx),
// stamped now.
func (g *Gate) inTx(ctx context.Context, fn func(tx *sql.Tx, now time.Time) error) error {
	return runInTx(ctx, g.db, "transaction", func(tx *sql.Tx) error { return fn(tx, g.now()) })
}

// Recent returns the newest audit rows, newest first.
func (g *Gate) Recent(ctx context.Context, limit int) ([]Entry, error) {
	return ListAudit(ctx, g.db, limit, 0)
}

// Watches reports whether changes of key are recorded: the whitelisted keys.
func (g *Gate) Watches(key string) bool { return mcpwritesettings.IsAllowed(key) }

// recordOn appends the audit row of a GUI change of key through ex (a
// transaction, when the row must commit with the change).
func recordOn(ctx context.Context, ex sqlutil.Execer, now time.Time, key string, before, after opsettings.Value, reset bool) error {
	if reset && !before.Overridden {
		return nil // nothing was stored: nothing changed
	}
	e := Entry{Actor: ActorGUI, Action: ActionSet, Key: key}
	if before.Overridden {
		e.Before = &before.Current
	}
	if reset {
		e.Action = ActionReset
	} else {
		e.After = &after.Current
	}
	_, err := insertAudit(ctx, ex, now, e)
	return err
}
