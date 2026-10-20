package mcpset

import (
	"context"
	"database/sql"
	"fmt"
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

// inTx runs fn in one application-database transaction, stamped now: it
// commits when fn succeeds and rolls everything back when fn fails, so a
// setting and its audit row are stored together or not at all (issue #813).
func (g *Gate) inTx(ctx context.Context, fn func(tx *sql.Tx, now time.Time) error) error {
	tx, err := g.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mcpset: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx, g.now()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mcpset: commit transaction: %w", err)
	}
	return nil
}

// Recent returns the newest audit rows, newest first.
func (g *Gate) Recent(ctx context.Context, limit int) ([]Entry, error) {
	return ListAudit(ctx, g.db, limit, 0)
}

// Watches reports whether changes of key are recorded: the whitelisted keys.
func (g *Gate) Watches(key string) bool { return mcpwritesettings.IsAllowed(key) }

// Record stores a GUI change of key on its own: before and after are the
// effective values around it, and reset is true for a reset to the default.
// Audited records inside the change's transaction instead (recordOn).
func (g *Gate) Record(ctx context.Context, key string, before, after opsettings.Value, reset bool) error {
	return recordOn(ctx, g.db, g.now(), key, before, after, reset)
}

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
