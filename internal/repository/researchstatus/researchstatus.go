// Package researchstatus persists research_tachibana_status: the current state
// of each 立花 research collection ("bars", "quotes"; issues #1082, #1083), so
// the read-only MCP can say whether it is off, waiting, running, or not
// configured. Nothing here is a trading input, and it never holds a secret.
package researchstatus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Components.
const (
	ComponentBars   = "bars"
	ComponentQuotes = "quotes"
)

// States.
const (
	StateDisabled          = "disabled"
	StateNotConfigured     = "not_configured"
	StateBrokerIsTachibana = "broker_is_tachibana"
	StateWaiting           = "waiting"
	StateRunning           = "running"
	StateOK                = "ok"
	StateError             = "error"
)

// Status is one component's row.
type Status struct {
	Component string
	State     string
	Detail    string // a plain description, never a credential or URL
	UpdatedAt time.Time
}

// Repository reads and writes research_tachibana_status.
type Repository struct{ db *sql.DB }

// New returns a Repository backed by db.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

// Set stores the state of component. Nothing is written when the state and
// detail are unchanged (a steady state costs no write), nor for "disabled"
// while no row exists: a default install, where every collection is off, gains
// no write at all.
func (r *Repository) Set(ctx context.Context, component, state, detail string, now time.Time) error {
	cur, found, err := r.Get(ctx, component)
	if err != nil {
		return err
	}
	if (found && cur.State == state && cur.Detail == detail) || (!found && state == StateDisabled) {
		return nil
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO research_tachibana_status (component, state, detail, updated_at)
		VALUES (?, ?, ?, ?) ON CONFLICT (component) DO UPDATE SET state = excluded.state,
		detail = excluded.detail, updated_at = excluded.updated_at`,
		component, state, detail, sqlutil.FormatTime(now))
	if err != nil {
		return fmt.Errorf("repository: set research status %q: %w", component, err)
	}
	return nil
}

// Get returns component's row; ok is false when it has none.
func (r *Repository) Get(ctx context.Context, component string) (Status, bool, error) {
	var (
		s       Status
		updated string
	)
	err := r.db.QueryRowContext(ctx, `SELECT component, state, detail, updated_at FROM research_tachibana_status
		WHERE component = ?`, component).Scan(&s.Component, &s.State, &s.Detail, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Status{}, false, nil
	}
	if err != nil {
		return Status{}, false, fmt.Errorf("repository: get research status %q: %w", component, err)
	}
	if s.UpdatedAt, err = sqlutil.ParseTime(updated); err != nil {
		return Status{}, false, err
	}
	return s, true, nil
}
