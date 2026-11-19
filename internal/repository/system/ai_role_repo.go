package system

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// AI role history kinds and actors (migration 000057).
const (
	AIRoleSaved   = "saved"
	AIRoleStarted = "started"

	aiRoleActorGUI = "gui" // the Settings screen is the only path that saves a role's setting
	aiRoleActorApp = "app"
)

// AIRoleChange is one ai_role_history row.
type AIRoleChange struct {
	At   time.Time
	Kind string
	Role string
	// Before is the setting before a saved change (the zero Setting for a
	// started row).
	Before airole.Setting
	After  airole.Setting
}

// AIRoleRepository stores the AI roles' provider/model settings
// (runtime_settings ai.role.<role>) together with their audit rows
// (ai_role_history, issue #1080).
type AIRoleRepository struct {
	db *sql.DB
	// now is the clock of the rows' at.
	now func() time.Time
}

// NewAIRoleRepository returns an AIRoleRepository over db.
func NewAIRoleRepository(db *sql.DB) *AIRoleRepository {
	return &AIRoleRepository{db: db, now: time.Now}
}

// SetClock replaces the clock; tests use it.
func (r *AIRoleRepository) SetClock(now func() time.Time) { r.now = now }

// Save stores s as role's setting and records the change from the setting in
// force before (the stored one, or the default) in one transaction, so the
// setting and its history row commit or roll back together. s must already be
// valid (airole.Normalize); a save that changes nothing records nothing.
func (r *AIRoleRepository) Save(ctx context.Context, role string, s airole.Setting) error {
	value, err := airole.Encode(role, s)
	if err != nil {
		return fmt.Errorf("repository: save ai role %q: %w", role, err)
	}
	return r.inTx(ctx, role, func(tx *sql.Tx, before airole.Setting) error {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO runtime_settings (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			airole.Key(role), value, sqlutil.FormatTime(r.now())); err != nil {
			return fmt.Errorf("repository: set ai role %q: %w", role, err)
		}
		return r.recordSaved(ctx, tx, role, before, s)
	})
}

// Reset removes role's stored setting so the default applies again, and
// records the switch back to it.
func (r *AIRoleRepository) Reset(ctx context.Context, role string) error {
	if !airole.IsRole(role) {
		return fmt.Errorf("repository: reset ai role %q: unknown role", role)
	}
	return r.inTx(ctx, role, func(tx *sql.Tx, before airole.Setting) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM runtime_settings WHERE key = ?`, airole.Key(role)); err != nil {
			return fmt.Errorf("repository: delete ai role %q: %w", role, err)
		}
		return r.recordSaved(ctx, tx, role, before, airole.DefaultSetting())
	})
}

// inTx runs fn in one transaction with the setting in force for role as
// before: the stored one, or the default when none is stored or the stored
// value is unusable (a damaged value must stay replaceable from the screen).
func (r *AIRoleRepository) inTx(ctx context.Context, role string, fn func(tx *sql.Tx, before airole.Setting) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repository: begin ai role %q change: %w", role, err)
	}
	defer func() { _ = tx.Rollback() }()
	before := airole.DefaultSetting()
	var raw string
	switch err := tx.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, airole.Key(role)).Scan(&raw); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("repository: read ai role %q: %w", role, err)
	default:
		if s, perr := airole.Parse(role, raw); perr == nil {
			before = s
		}
	}
	if err := fn(tx, before); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repository: commit ai role %q change: %w", role, err)
	}
	return nil
}

func (r *AIRoleRepository) recordSaved(ctx context.Context, x sqlutil.Execer, role string, before, after airole.Setting) error {
	if before == after {
		return nil
	}
	if _, err := x.ExecContext(ctx, `
INSERT INTO ai_role_history (at, kind, actor, role, before_provider, before_model, provider, model) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sqlutil.FormatTime(r.now()), AIRoleSaved, aiRoleActorGUI, role, before.Provider, before.Model, after.Provider, after.Model); err != nil {
		return fmt.Errorf("repository: record ai role change: %w", err)
	}
	return nil
}

// RecordStarted records the setting role actually runs on since this start,
// unless the last started row already says so. It reports whether it wrote a
// row. Only the provider and model name go in.
func (r *AIRoleRepository) RecordStarted(ctx context.Context, role string, running airole.Setting) (bool, error) {
	last, ok, err := r.LastStarted(ctx, role)
	if err != nil {
		return false, err
	}
	if ok && last.After == running {
		return false, nil
	}
	if _, err := r.db.ExecContext(ctx, `
INSERT INTO ai_role_history (at, kind, actor, role, provider, model) VALUES (?, ?, ?, ?, ?, ?)`,
		sqlutil.FormatTime(r.now()), AIRoleStarted, aiRoleActorApp, role, running.Provider, running.Model); err != nil {
		return false, fmt.Errorf("repository: record ai role start: %w", err)
	}
	return true, nil
}

// LastStarted returns the newest started row of role (ok=false for none).
func (r *AIRoleRepository) LastStarted(ctx context.Context, role string) (AIRoleChange, bool, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT at, kind, role, before_provider, before_model, provider, model FROM ai_role_history
WHERE role = ? AND kind = 'started' ORDER BY id DESC LIMIT 1`, role)
	if err != nil {
		return AIRoleChange{}, false, fmt.Errorf("repository: read ai role start: %w", err)
	}
	changes, err := scanAIRoleHistory(rows)
	if err != nil || len(changes) == 0 {
		return AIRoleChange{}, false, err
	}
	return changes[0], true, nil
}

// History returns the newest saved/started rows first, at most limit.
func (r *AIRoleRepository) History(ctx context.Context, limit int) ([]AIRoleChange, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT at, kind, role, before_provider, before_model, provider, model FROM ai_role_history
ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: read ai role history: %w", err)
	}
	return scanAIRoleHistory(rows)
}

// scanAIRoleHistory reads and closes rows of the columns both queries select.
func scanAIRoleHistory(rows *sql.Rows) ([]AIRoleChange, error) {
	defer func() { _ = rows.Close() }()
	var out []AIRoleChange
	for rows.Next() {
		var at string
		var c AIRoleChange
		var bp, bm sql.NullString
		if err := rows.Scan(&at, &c.Kind, &c.Role, &bp, &bm, &c.After.Provider, &c.After.Model); err != nil {
			return nil, fmt.Errorf("repository: scan ai role history: %w", err)
		}
		t, err := sqlutil.ParseTime(at)
		if err != nil {
			return nil, fmt.Errorf("repository: ai role history: %w", err)
		}
		c.At = t
		c.Before = airole.Setting{Provider: bp.String, Model: bm.String}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: read ai role history: %w", err)
	}
	return out, nil
}
