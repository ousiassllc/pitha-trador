package market

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// DailyBarRunRepository persists daily_bar_runs: one row per night of the
// daily-bar batch (docs/architecture/er/tables-market.md §daily_bar_runs).
type DailyBarRunRepository struct {
	db *sql.DB
}

// NewDailyBarRunRepository returns a DailyBarRunRepository backed by db.
func NewDailyBarRunRepository(db *sql.DB) *DailyBarRunRepository {
	return &DailyBarRunRepository{db: db}
}

// Get returns the run of runDate, or (zero, false, nil) when that night has none.
func (r *DailyBarRunRepository) Get(ctx context.Context, runDate string) (domain.DailyBarRun, bool, error) {
	var (
		run      domain.DailyBarRun
		started  string
		finished sql.NullString
	)
	err := r.db.QueryRowContext(ctx, `SELECT run_date, status, started_at, finished_at, symbols, requests,
		saved_bars, failed, duration_ms, cursor, error FROM daily_bar_runs WHERE run_date = ?`, runDate).
		Scan(&run.RunDate, &run.Status, &started, &finished, &run.Symbols, &run.Requests,
			&run.SavedBars, &run.Failed, &run.DurationMS, &run.Cursor, &run.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DailyBarRun{}, false, nil
	}
	if err != nil {
		return domain.DailyBarRun{}, false, fmt.Errorf("repository: get daily bar run %q: %w", runDate, err)
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
func (r *DailyBarRunRepository) Save(ctx context.Context, run domain.DailyBarRun) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO daily_bar_runs (run_date, status, started_at, finished_at,
		symbols, requests, saved_bars, failed, duration_ms, cursor, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (run_date) DO UPDATE SET status = excluded.status, started_at = excluded.started_at,
		finished_at = excluded.finished_at, symbols = excluded.symbols, requests = excluded.requests,
		saved_bars = excluded.saved_bars, failed = excluded.failed, duration_ms = excluded.duration_ms,
		cursor = excluded.cursor, error = excluded.error`,
		run.RunDate, run.Status, sqlutil.FormatTime(run.StartedAt), sqlutil.NullableTime(run.FinishedAt),
		run.Symbols, run.Requests, run.SavedBars, run.Failed, run.DurationMS, run.Cursor, run.Error)
	if err != nil {
		return fmt.Errorf("repository: save daily bar run %q: %w", run.RunDate, err)
	}
	return nil
}
