package backtestrun

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// The columns every read selects by name (never SELECT *). The list leaves
// result_json out: a run's result is read only by Get.
const (
	listColumns = `id, status, owner, created_at, started_at, finished_at, period_from, period_to,
    first_snapshot_at, last_snapshot_at, replay_mode, code_version, explored_count, request_json, error_code`
	getColumns = listColumns + `, result_json`
)

// Get returns run id, or ErrNotFound.
func Get(ctx context.Context, q Queryer, id int64) (Run, error) {
	row := q.QueryRowContext(ctx, `SELECT `+getColumns+` FROM backtest_runs WHERE id = ?`, id)
	r, err := scan(row.Scan, true)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("backtestrun: get %d: %w", id, err)
	}
	return r, nil
}

// ListFilter selects the rows of List.
type ListFilter struct {
	// Status, when not empty, keeps the runs in that status.
	Status string
	// BeforeID, when above 0, keeps the runs with a smaller id (the cursor).
	BeforeID int64
	// Limit is the page size.
	Limit int
}

// List returns up to f.Limit runs, newest first, and whether more follow. It
// does not read the results.
func List(ctx context.Context, q Queryer, f ListFilter) (runs []Run, more bool, err error) {
	where, args := "1 = 1", []any{}
	if f.Status != "" {
		where += " AND status = ?"
		args = append(args, f.Status)
	}
	if f.BeforeID > 0 {
		where += " AND id < ?"
		args = append(args, f.BeforeID)
	}
	rows, err := q.QueryContext(ctx, `SELECT `+listColumns+` FROM backtest_runs WHERE `+where+` ORDER BY id DESC LIMIT ?`, append(args, f.Limit+1)...)
	if err != nil {
		return nil, false, fmt.Errorf("backtestrun: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	runs = []Run{}
	for rows.Next() {
		r, err := scan(rows.Scan, false)
		if err != nil {
			return nil, false, fmt.Errorf("backtestrun: list: %w", err)
		}
		if len(runs) == f.Limit {
			return runs, true, rows.Err()
		}
		runs = append(runs, r)
	}
	return runs, false, rows.Err()
}

// scan reads one row; withResult says the row carries result_json last.
func scan(scanFn func(dest ...any) error, withResult bool) (Run, error) {
	var (
		r                              Run
		created                        string
		started, finished, first, last sql.NullString
		errorCode, result              sql.NullString
	)
	dest := []any{&r.ID, &r.Status, &r.Owner, &created, &started, &finished, &r.FromDate, &r.ToDate,
		&first, &last, &r.ReplayMode, &r.CodeVersion, &r.ExploredCount, &r.RequestJSON, &errorCode}
	if withResult {
		dest = append(dest, &result)
	}
	if err := scanFn(dest...); err != nil {
		return Run{}, err
	}
	var err error
	if r.CreatedAt, err = sqlutil.ParseTime(created); err != nil {
		return Run{}, err
	}
	for _, f := range []struct {
		src sql.NullString
		dst **time.Time
	}{{started, &r.StartedAt}, {finished, &r.FinishedAt}, {first, &r.FirstSnapshotAt}, {last, &r.LastSnapshotAt}} {
		if *f.dst, err = sqlutil.ParseNullableTime(f.src); err != nil {
			return Run{}, err
		}
	}
	r.ErrorCode, r.ResultJSON = errorCode.String, result.String
	return r, nil
}
