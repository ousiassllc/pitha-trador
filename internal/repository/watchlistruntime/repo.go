// Package watchlistruntime persists watch_list_runtime_days (migration
// 000056, issue #920): the daily size of the runtime (kabu ranking-driven)
// watch list. It lives apart from internal/repository/market, whose
// watch_lists tables hold the 立花 saved lists with their symbols.
package watchlistruntime

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Repository stores one row per (JST session date, source).
type Repository struct {
	db *sql.DB
}

// New returns a Repository backed by db.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

// RecordRuntimeSize notes one in-session observation of the runtime watch list
// (the kabu ranking-driven list, issue #920): size symbols for the JST date and
// source in watch_list_runtime_days (migration 000056). It creates the day's
// row or updates it in place (latest size, running peak, one more sample).
// Counts only, never a symbol; watch_lists (the 立花 saved lists) is not touched.
func (r *Repository) RecordRuntimeSize(ctx context.Context, date, source string, size int, now time.Time) error {
	if _, err := r.db.ExecContext(ctx, `
INSERT INTO watch_list_runtime_days (session_date, source, entries, peak_entries, samples, recorded_at)
VALUES (?, ?, ?, ?, 1, ?)
ON CONFLICT (session_date, source) DO UPDATE SET entries = excluded.entries,
	peak_entries = max(peak_entries, excluded.peak_entries), samples = samples + 1, recorded_at = excluded.recorded_at`,
		date, source, size, size, sqlutil.FormatTime(now)); err != nil {
		return fmt.Errorf("repository: record runtime watch list size of %s: %w", date, err)
	}
	return nil
}
