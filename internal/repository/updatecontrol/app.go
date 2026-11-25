package updatecontrol

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// The application's side: it publishes the status and takes the requests.

// Store is the application's handle on the two tables.
type Store struct{ db *sql.DB }

// New returns a Store over the application's database.
func New(db *sql.DB) *Store { return &Store{db: db} }

// Publish writes s as the current status (heartbeat_at is now).
func (s *Store) Publish(ctx context.Context, now time.Time, st Status) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO update_control_status (id, heartbeat_at, current_version, checked_at, dev_build, no_release, available,
	latest_version, blocked, blocked_kind, ready, error_kind)
VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET heartbeat_at = excluded.heartbeat_at, current_version = excluded.current_version,
	checked_at = excluded.checked_at, dev_build = excluded.dev_build, no_release = excluded.no_release,
	available = excluded.available, latest_version = excluded.latest_version, blocked = excluded.blocked,
	blocked_kind = excluded.blocked_kind, ready = excluded.ready, error_kind = excluded.error_kind`,
		sqlutil.FormatTime(now), st.CurrentVersion, sqlutil.NullableTime(st.CheckedAt), st.DevBuild, st.NoRelease, st.Available,
		st.LatestVersion, st.Blocked, st.BlockedKind, st.Ready, st.ErrorKind)
	if err != nil {
		return fmt.Errorf("updatecontrol: publish status: %w", err)
	}
	return nil
}

// Retire removes the published status, so the MCP answers "no updater" once the application is going away.
func (s *Store) Retire(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM update_control_status WHERE id = 1`); err != nil {
		return fmt.Errorf("updatecontrol: retire status: %w", err)
	}
	return nil
}

// ExpireUnfinished ends every request a previous run of the application left pending or running, as expired:
// a late start must not execute what its caller has long given up on.
func (s *Store) ExpireUnfinished(ctx context.Context, now time.Time) error {
	_, err := finish(ctx, s.db, now, `state <> 'done'`, OutcomeExpired, "")
	return err
}

// HasPending is a read-only probe, so an idle poll takes no write lock.
func (s *Store) HasPending(ctx context.Context) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM update_control_requests WHERE state = 'pending' LIMIT 1`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Claim marks the oldest pending request running and returns it; ok is false
// when another claim won it or none is pending.
func (s *Store) Claim(ctx context.Context) (Request, bool, error) {
	row := s.db.QueryRowContext(ctx, `
UPDATE update_control_requests SET state = 'running'
WHERE id = (SELECT id FROM update_control_requests WHERE state = 'pending' ORDER BY id LIMIT 1) AND state = 'pending'
RETURNING `+requestColumns)
	r, err := scanRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, false, nil
	}
	if err != nil {
		return Request{}, false, fmt.Errorf("updatecontrol: claim request: %w", err)
	}
	return r, true, nil
}

// Finish ends the running request id with outcome and reason.
func (s *Store) Finish(ctx context.Context, now time.Time, id int64, outcome, reason string) error {
	_, err := finish(ctx, s.db, now, `id = ? AND state = 'running'`, outcome, reason, id)
	return err
}
