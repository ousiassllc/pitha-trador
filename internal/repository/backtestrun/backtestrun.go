// Package backtestrun is the record of the research backtests the Paper
// results MCP starts (issue #1086; docs/architecture/er/tables-backtest.md):
// one backtest_runs row per run, holding the request, the code version, the
// data actually covered and the result of every parameter set.
//
// It is research-only. Nothing here is an input of trading: Policy, Risk,
// order placement, execution, the Paper guard, the Live path, the Jev prompts,
// the meta-model training and the self-improvement flow never read it
// (isolation_test.go walks the source tree and fails when one does). The
// statements are the table's whole write surface: one INSERT per run, the
// UPDATEs of its lifecycle and the orphan sweep. No other table is written.
package backtestrun

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Statuses of a run.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

// Statuses lists the statuses in lifecycle order.
func Statuses() []string {
	return []string{StatusQueued, StatusRunning, StatusSucceeded, StatusFailed}
}

// ValidStatus reports whether s is one of Statuses.
func ValidStatus(s string) bool {
	for _, v := range Statuses() {
		if v == s {
			return true
		}
	}
	return false
}

// The fixed error codes of a failed run (never a raw error text or a path).
const (
	ErrorTimedOut    = "timed_out"
	ErrorInterrupted = "interrupted"
	ErrorUnavailable = "unavailable"
	ErrorInternal    = "internal"
)

// NewRun is what the MCP records when it accepts a run.
type NewRun struct {
	// Owner is the token of the creating process (see Run.Owner).
	Owner string
	// Client is the caller's host (audit).
	Client string
	// FromDate / ToDate are the JST dates of the requested period [from, to).
	FromDate, ToDate string
	ReplayMode       string
	CodeVersion      string
	ExploredCount    int
	// RequestJSON is the canonical request: the variants and the extra costs.
	RequestJSON string
}

// Run is one backtest_runs row.
type Run struct {
	ID     int64
	Status string
	// Owner is the token of the process that created the row; a restart gives a
	// new token, so rows left running by the old process are told apart.
	Owner      string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	// FromDate / ToDate are the JST dates of the requested period [from, to).
	FromDate, ToDate string
	// FirstSnapshotAt / LastSnapshotAt are the first and last snapshot actually
	// found in the period (nil: none, or the run has not finished).
	FirstSnapshotAt *time.Time
	LastSnapshotAt  *time.Time
	ReplayMode      string
	CodeVersion     string
	ExploredCount   int
	RequestJSON     string
	// ResultJSON is the result of a succeeded run; empty otherwise. List does
	// not read it.
	ResultJSON string
	// ErrorCode is the fixed code of a failed run.
	ErrorCode string
}

// Outcome is how a run ended.
type Outcome struct {
	// ErrorCode is "" for a success, else one of the Error* codes.
	ErrorCode string
	// ResultJSON is the result of a success.
	ResultJSON      string
	FirstSnapshotAt *time.Time
	LastSnapshotAt  *time.Time
}

// Execer is the transaction handle the writes get (*sql.Tx).
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Queryer is the handle the reads get (*sql.Tx or *sql.DB).
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ErrNotFound is returned by Get for an id that has no row.
var ErrNotFound = errors.New("backtestrun: run not found")

// The table's whole write surface.
const (
	insertSQL = `
INSERT INTO backtest_runs (status, owner, client, created_at, period_from, period_to, replay_mode, code_version, explored_count, request_json)
VALUES ('queued', ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	startSQL = `UPDATE backtest_runs SET status = 'running', started_at = ? WHERE id = ? AND status = 'queued'`

	finishSQL = `
UPDATE backtest_runs
SET status = ?, finished_at = ?, started_at = COALESCE(started_at, ?), first_snapshot_at = ?, last_snapshot_at = ?, result_json = ?, error_code = ?
WHERE id = ? AND status IN ('queued', 'running')`

	orphanSQL = `
UPDATE backtest_runs SET status = 'failed', finished_at = ?, error_code = 'interrupted'
WHERE status IN ('queued', 'running') AND owner <> ?`
)

// Insert records req as a queued run created at now and returns it.
func Insert(ctx context.Context, tx Execer, now time.Time, req NewRun) (Run, error) {
	res, err := tx.ExecContext(ctx, insertSQL, req.Owner, req.Client, sqlutil.FormatTime(now), req.FromDate, req.ToDate,
		req.ReplayMode, req.CodeVersion, req.ExploredCount, req.RequestJSON)
	if err != nil {
		return Run{}, fmt.Errorf("backtestrun: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Run{}, fmt.Errorf("backtestrun: insert id: %w", err)
	}
	return Run{
		ID: id, Status: StatusQueued, Owner: req.Owner, CreatedAt: now.UTC(), FromDate: req.FromDate, ToDate: req.ToDate,
		ReplayMode: req.ReplayMode, CodeVersion: req.CodeVersion, ExploredCount: req.ExploredCount, RequestJSON: req.RequestJSON,
	}, nil
}

// Start marks the queued run id as running at now. A run that is not queued is
// left alone.
func Start(ctx context.Context, tx Execer, now time.Time, id int64) error {
	if _, err := tx.ExecContext(ctx, startSQL, sqlutil.FormatTime(now), id); err != nil {
		return fmt.Errorf("backtestrun: start %d: %w", id, err)
	}
	return nil
}

// Finish ends the queued or running run id at now with out. A run that already
// ended is left alone.
func Finish(ctx context.Context, tx Execer, now time.Time, id int64, out Outcome) error {
	status, result, code := StatusSucceeded, any(out.ResultJSON), any(nil)
	if out.ErrorCode != "" {
		status, result, code = StatusFailed, nil, out.ErrorCode
	}
	if _, err := tx.ExecContext(ctx, finishSQL, status, sqlutil.FormatTime(now), sqlutil.FormatTime(now),
		sqlutil.NullableTime(out.FirstSnapshotAt), sqlutil.NullableTime(out.LastSnapshotAt), result, code, id); err != nil {
		return fmt.Errorf("backtestrun: finish %d: %w", id, err)
	}
	return nil
}

// FailOrphans ends every queued or running run whose owner is not owner as
// failed (interrupted) at now: a restart leaves such rows behind. It returns
// how many rows it changed.
func FailOrphans(ctx context.Context, tx Execer, now time.Time, owner string) (int64, error) {
	res, err := tx.ExecContext(ctx, orphanSQL, sqlutil.FormatTime(now), owner)
	if err != nil {
		return 0, fmt.Errorf("backtestrun: fail orphans: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("backtestrun: fail orphans: %w", err)
	}
	return n, nil
}
