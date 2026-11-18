// Package jevreplay persists the replay evaluation (issue #1085): the runs
// (jev_replay_runs) and one result row per (run, live decision, arm)
// (jev_replay_results), and reads the saved inputs of the live trader decisions
// (jev_decisions.state_json) a run replays. It is separate from the judgement
// repositories on purpose: nothing that reads the live, shadow or trial decision
// tables (RAG, Calibration, the meta-model, Policy) can see a replay row, and a
// replay never writes any of them.
//
// The statements that the Paper results MCP also runs (creating a run, setting
// the stop flag, reading runs) are package functions over a transaction-or-DB
// handle; the runner's own writes are methods of Repository.
package jevreplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ErrRunNotFound is returned for a run id that does not exist.
var ErrRunNotFound = errors.New("repository: jev replay run not found")

// Querier is the read handle of the package functions; *sql.DB and *sql.Tx
// satisfy it.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const runColumns = `id, status, stop_requested, stop_reason, blocked_reason, spec_json, period_from, period_to, max_rows, max_decision_id, arms_json,
	estimate_decisions, estimate_calls, estimate_max_cost_usd, estimate_list_price_usd, estimate_token, calls_done, calls_failed, counted_cost_usd,
	created_by, created_at, started_at, finished_at, updated_at`

// InsertRun writes a new run as pending and returns its id. run.Spec, the
// period, MaxRows, MaxDecisionID, ArmsJSON and Estimate must be set; the
// database refuses a max_rows above jevreplay.MaxRows.
func InsertRun(ctx context.Context, ex sqlutil.Execer, run jevreplay.Run, now time.Time) (int64, error) {
	spec, err := json.Marshal(run.Spec)
	if err != nil {
		return 0, fmt.Errorf("repository: encode jev replay spec: %w", err)
	}
	res, err := ex.ExecContext(ctx, `
INSERT INTO jev_replay_runs (status, spec_json, period_from, period_to, max_rows, max_decision_id, arms_json,
	estimate_decisions, estimate_calls, estimate_max_cost_usd, estimate_list_price_usd, estimate_token, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		jevreplay.StatusPending, string(spec), sqlutil.FormatTime(run.PeriodFrom), sqlutil.FormatTime(run.PeriodTo), run.MaxRows, run.MaxDecisionID, run.ArmsJSON,
		run.Estimate.Decisions, run.Estimate.Calls, run.Estimate.MaxCostUSD, sqlutil.NullableFloat64(run.Estimate.ListPriceUSD), run.Estimate.Token,
		run.CreatedBy, sqlutil.FormatTime(now), sqlutil.FormatTime(now))
	if err != nil {
		return 0, fmt.Errorf("repository: insert jev replay run: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repository: read jev replay run id: %w", err)
	}
	return id, nil
}

// RequestStop sets the stop flag of an active (pending or running) run. It
// reports whether a run was changed: false for an unknown or finished run. The
// runner sees the flag before its next call and ends the run as stopped.
func RequestStop(ctx context.Context, ex sqlutil.Execer, id int64, now time.Time) (bool, error) {
	res, err := ex.ExecContext(ctx, `
UPDATE jev_replay_runs SET stop_requested = 1, updated_at = ? WHERE id = ? AND status IN ('pending', 'running')`, sqlutil.FormatTime(now), id)
	if err != nil {
		return false, fmt.Errorf("repository: request stop of jev replay run %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("repository: stop of jev replay run %d: %w", id, err)
	}
	return n > 0, nil
}

// CountActive is the number of runs that are pending or running.
func CountActive(ctx context.Context, q Querier) (int, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM jev_replay_runs WHERE status IN ('pending', 'running')`).Scan(&n); err != nil {
		return 0, fmt.Errorf("repository: count active jev replay runs: %w", err)
	}
	return n, nil
}

// GetRun returns the run with the given id, or ErrRunNotFound.
func GetRun(ctx context.Context, q Querier, id int64) (jevreplay.Run, error) {
	run, err := scanRun(q.QueryRowContext(ctx, `SELECT `+runColumns+` FROM jev_replay_runs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return jevreplay.Run{}, ErrRunNotFound
	}
	return run, err
}

// ListRuns returns up to limit runs, newest first.
func ListRuns(ctx context.Context, q Querier, limit int) ([]jevreplay.Run, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+runColumns+` FROM jev_replay_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list jev replay runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []jevreplay.Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list jev replay runs: %w", err)
	}
	return out, nil
}

func scanRun(s sqlutil.RowScanner) (jevreplay.Run, error) {
	var (
		r                                jevreplay.Run
		spec, from, to, created, updated string
		stopRequested                    int
		list                             sql.NullFloat64
		started, finished                sql.NullString
	)
	err := s.Scan(&r.ID, &r.Status, &stopRequested, &r.StopReason, &r.BlockedReason, &spec, &from, &to, &r.MaxRows, &r.MaxDecisionID, &r.ArmsJSON,
		&r.Estimate.Decisions, &r.Estimate.Calls, &r.Estimate.MaxCostUSD, &list, &r.Estimate.Token, &r.CallsDone, &r.CallsFailed, &r.CountedCostUSD,
		&r.CreatedBy, &created, &started, &finished, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return jevreplay.Run{}, err
		}
		return jevreplay.Run{}, fmt.Errorf("repository: scan jev replay run: %w", err)
	}
	r.StopRequested = stopRequested != 0
	if list.Valid {
		r.Estimate.ListPriceUSD = &list.Float64
	}
	if err := json.Unmarshal([]byte(spec), &r.Spec); err != nil {
		return jevreplay.Run{}, fmt.Errorf("repository: decode jev replay run %d spec: %w", r.ID, err)
	}
	for _, p := range []struct {
		dst *time.Time
		src string
	}{{&r.PeriodFrom, from}, {&r.PeriodTo, to}, {&r.CreatedAt, created}, {&r.UpdatedAt, updated}} {
		if *p.dst, err = sqlutil.ParseTime(p.src); err != nil {
			return jevreplay.Run{}, err
		}
	}
	if r.StartedAt, err = sqlutil.ParseNullableTime(started); err != nil {
		return jevreplay.Run{}, err
	}
	if r.FinishedAt, err = sqlutil.ParseNullableTime(finished); err != nil {
		return jevreplay.Run{}, err
	}
	return r, nil
}
