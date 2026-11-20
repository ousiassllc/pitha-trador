package jevreplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Repository is the runner's side: it advances a run and stores its result rows.
// It never touches jev_decisions (reads only, see SavedDecisions), the trial
// tables or the runtime settings.
type Repository struct {
	db *sql.DB
}

// QueryContext and QueryRowContext make Repository a Querier, the read handle
// SavedDecisions and the package's read functions take.
func (r *Repository) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return r.db.QueryContext(ctx, query, args...)
}

// QueryRowContext is QueryContext for one row.
func (r *Repository) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return r.db.QueryRowContext(ctx, query, args...)
}

// New returns a Repository backed by db.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

// NextActive returns the oldest pending or running run; ok is false for none.
func (r *Repository) NextActive(ctx context.Context) (run jevreplay.Run, ok bool, err error) {
	run, err = scanRun(r.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM jev_replay_runs WHERE status IN ('pending', 'running') ORDER BY id LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return jevreplay.Run{}, false, nil
	}
	return run, err == nil, err
}

// Get returns the run with the given id, or ErrRunNotFound.
func (r *Repository) Get(ctx context.Context, id int64) (jevreplay.Run, error) {
	return GetRun(ctx, r.db, id)
}

// StopRequested reads the run's stop flag now (the Run the runner holds may be
// stale). An unknown run reads as stop requested.
func (r *Repository) StopRequested(ctx context.Context, id int64) (bool, error) {
	var flag int
	err := r.db.QueryRowContext(ctx, `SELECT stop_requested FROM jev_replay_runs WHERE id = ?`, id).Scan(&flag)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("repository: read stop flag of jev replay run %d: %w", id, err)
	}
	return flag != 0, nil
}

// MarkRunning moves a pending run to running (a running one is left as it is).
func (r *Repository) MarkRunning(ctx context.Context, id int64, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `
UPDATE jev_replay_runs SET status = 'running', started_at = COALESCE(started_at, ?), updated_at = ?
WHERE id = ? AND status IN ('pending', 'running')`, sqlutil.FormatTime(at), sqlutil.FormatTime(at), id)
	if err != nil {
		return fmt.Errorf("repository: mark jev replay run %d running: %w", id, err)
	}
	return nil
}

// SetBlocked records why a running run makes no new call now ("" when it is
// making calls again). It writes only when the reason changes.
func (r *Repository) SetBlocked(ctx context.Context, id int64, reason string, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `
UPDATE jev_replay_runs SET blocked_reason = ?, updated_at = ? WHERE id = ? AND status = 'running' AND blocked_reason <> ?`,
		reason, sqlutil.FormatTime(at), id, reason)
	if err != nil {
		return fmt.Errorf("repository: set blocked reason of jev replay run %d: %w", id, err)
	}
	return nil
}

// Finish ends an active run as status (completed, stopped or failed) with its
// stop reason ("" for completed).
func (r *Repository) Finish(ctx context.Context, id int64, status, stopReason string, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `
UPDATE jev_replay_runs SET status = ?, stop_reason = ?, blocked_reason = '', finished_at = ?, updated_at = ?
WHERE id = ? AND status IN ('pending', 'running')`, status, stopReason, sqlutil.FormatTime(at), sqlutil.FormatTime(at), id)
	if err != nil {
		return fmt.Errorf("repository: finish jev replay run %d: %w", id, err)
	}
	return nil
}
