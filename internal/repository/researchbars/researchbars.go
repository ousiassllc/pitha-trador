// Package researchbars persists the 立花 research daily-bar collection
// (issue #1082): research_daily_bar_runs (one row per night, the same shape as
// daily_bar_runs but kept apart from it) and research_daily_bar_progress (one
// row per symbol: the night it was last attempted and whether it has ever been
// fetched, which is what a resumed or later night reads). The records are
// research-only: no trading component reads them (isolation_test.go).
package researchbars

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Repository reads and writes the two research daily-bar tables.
type Repository struct{ db *sql.DB }

// New returns a Repository backed by db.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

// Get returns the research run of runDate, or (zero, false, nil) when that
// night has none.
func (r *Repository) Get(ctx context.Context, runDate string) (domain.DailyBarRun, bool, error) {
	var (
		run      domain.DailyBarRun
		started  string
		finished sql.NullString
	)
	err := r.db.QueryRowContext(ctx, `SELECT run_date, status, started_at, finished_at, symbols, requests,
		saved_bars, failed, duration_ms, cursor, error FROM research_daily_bar_runs WHERE run_date = ?`, runDate).
		Scan(&run.RunDate, &run.Status, &started, &finished, &run.Symbols, &run.Requests,
			&run.SavedBars, &run.Failed, &run.DurationMS, &run.Cursor, &run.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DailyBarRun{}, false, nil
	}
	if err != nil {
		return domain.DailyBarRun{}, false, fmt.Errorf("repository: get research daily bar run %q: %w", runDate, err)
	}
	if run.StartedAt, err = sqlutil.ParseTime(started); err != nil {
		return domain.DailyBarRun{}, false, err
	}
	if run.FinishedAt, err = sqlutil.ParseNullableTime(finished); err != nil {
		return domain.DailyBarRun{}, false, err
	}
	return run, true, nil
}

// Save inserts run or overwrites the stored run of the same RunDate.
func (r *Repository) Save(ctx context.Context, run domain.DailyBarRun) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO research_daily_bar_runs (run_date, status, started_at, finished_at,
		symbols, requests, saved_bars, failed, duration_ms, cursor, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (run_date) DO UPDATE SET status = excluded.status, started_at = excluded.started_at,
		finished_at = excluded.finished_at, symbols = excluded.symbols, requests = excluded.requests,
		saved_bars = excluded.saved_bars, failed = excluded.failed, duration_ms = excluded.duration_ms,
		cursor = excluded.cursor, error = excluded.error`,
		run.RunDate, run.Status, sqlutil.FormatTime(run.StartedAt), sqlutil.NullableTime(run.FinishedAt),
		run.Symbols, run.Requests, run.SavedBars, run.Failed, run.DurationMS, run.Cursor, run.Error)
	if err != nil {
		return fmt.Errorf("repository: save research daily bar run %q: %w", run.RunDate, err)
	}
	return nil
}

// LastNights returns, per symbol that was ever attempted, the night it was
// last attempted. A symbol whose night is the running one is not requested
// again that night (how a cut-short night resumes); the others go oldest
// first, so a symbol left over by a short night comes before one done lately.
func (r *Repository) LastNights(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT symbol, last_night FROM research_daily_bar_progress`)
	if err != nil {
		return nil, fmt.Errorf("repository: list research daily bar progress: %w", err)
	}
	defer func() { _ = rows.Close() }()
	nights := map[string]string{}
	for rows.Next() {
		var symbol, night string
		if err := rows.Scan(&symbol, &night); err != nil {
			return nil, fmt.Errorf("repository: scan research daily bar progress: %w", err)
		}
		nights[symbol] = night
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list research daily bar progress: %w", err)
	}
	return nights, nil
}

// RecordAttempt notes that symbol was requested on night: ok is whether the
// broker answered, saved how many bars that wrote. The first success sets
// first_ok_night (the backfill mark); a failure counts in failures and
// leaves first_ok_night alone.
func (r *Repository) RecordAttempt(ctx context.Context, symbol, night string, ok bool, saved int, now time.Time) error {
	okInt, failInt := 0, 1
	var firstOK sql.NullString
	if ok {
		okInt, failInt = 1, 0
		firstOK = sql.NullString{String: night, Valid: true}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO research_daily_bar_progress
		(symbol, last_night, last_ok, first_ok_night, bars_saved, failures, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (symbol) DO UPDATE SET last_night = excluded.last_night, last_ok = excluded.last_ok,
		first_ok_night = COALESCE(research_daily_bar_progress.first_ok_night, excluded.first_ok_night),
		bars_saved = research_daily_bar_progress.bars_saved + excluded.bars_saved,
		failures = research_daily_bar_progress.failures + excluded.failures,
		updated_at = excluded.updated_at`,
		symbol, night, okInt, firstOK, saved, failInt, sqlutil.FormatTime(now))
	if err != nil {
		return fmt.Errorf("repository: record research daily bar attempt of %q: %w", symbol, err)
	}
	return nil
}
