package researchbars_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchbars"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func newRepo(t *testing.T) (*researchbars.Repository, *sql.DB) {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return researchbars.New(conn), conn
}

// first_ok_night is set by the first success and never moved or cleared by a
// later failure or success; counters accumulate; last_night follows the latest attempt.
func TestRecordAttemptKeepsTheFirstSuccessAndAccumulates(t *testing.T) {
	r, conn := newRepo(t)
	ctx, now := context.Background(), time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	steps := []struct {
		night string
		ok    bool
		saved int
	}{{"2026-10-08", false, 0}, {"2026-10-09", true, 3000}, {"2026-10-10", false, 0}, {"2026-10-11", true, 2}}
	for _, s := range steps {
		if err := r.RecordAttempt(ctx, "7203", s.night, s.ok, s.saved, now); err != nil {
			t.Fatal(err)
		}
	}
	var first *string
	var saved, failures, lastOK int
	var last string
	err := conn.QueryRowContext(ctx, `SELECT last_night, last_ok, first_ok_night, bars_saved, failures FROM research_daily_bar_progress WHERE symbol = '7203'`).
		Scan(&last, &lastOK, &first, &saved, &failures)
	if err != nil {
		t.Fatal(err)
	}
	if last != "2026-10-11" || lastOK != 1 || first == nil || *first != "2026-10-09" || saved != 3002 || failures != 2 {
		t.Fatalf("progress = last %q ok %d first %v saved %d failures %d", last, lastOK, first, saved, failures)
	}
	nights, err := r.LastNights(ctx)
	if err != nil || len(nights) != 1 || nights["7203"] != "2026-10-11" {
		t.Fatalf("LastNights = %v, %v", nights, err)
	}
}

// A run row round-trips, and saving the same night again overwrites it.
func TestRunRoundTripAndOverwrite(t *testing.T) {
	r, _ := newRepo(t)
	ctx := context.Background()
	if _, found, err := r.Get(ctx, "2026-10-08"); err != nil || found {
		t.Fatalf("Get before Save = %v, %v", found, err)
	}
	started := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	run := domain.DailyBarRun{RunDate: "2026-10-08", Status: domain.DailyBarRunRunning, StartedAt: started, Symbols: 3, Requests: 1, Cursor: "1001"}
	if err := r.Save(ctx, run); err != nil {
		t.Fatal(err)
	}
	done := started.Add(time.Minute)
	run.Status, run.FinishedAt, run.SavedBars, run.DurationMS, run.Error = domain.DailyBarRunSucceeded, &done, 7, 60000, ""
	if err := r.Save(ctx, run); err != nil {
		t.Fatal(err)
	}
	got, found, err := r.Get(ctx, "2026-10-08")
	if err != nil || !found {
		t.Fatalf("Get = %v, %v", found, err)
	}
	if got.Status != domain.DailyBarRunSucceeded || got.SavedBars != 7 || got.Cursor != "1001" || !got.StartedAt.Equal(started) || got.FinishedAt == nil || !got.FinishedAt.Equal(done) {
		t.Fatalf("run = %+v", got)
	}
}
