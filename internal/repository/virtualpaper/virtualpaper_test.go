package virtualpaper_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/virtualpaper"
)

func TestRecordDay_IsAtomicAndIdempotent(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	now := time.Date(2026, 1, 7, 8, 0, 0, 0, time.UTC)
	id, err := repo.InsertProfile(ctx, db, repo.NewProfile{Name: "p", ParamsJSON: "{}", CreatedAt: now, FirstDay: "2026-01-08", Actor: repo.ActorMCP})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.InsertProfile(ctx, db, repo.NewProfile{Name: "p", ParamsJSON: "{}", CreatedAt: now, FirstDay: "2026-01-08", Actor: repo.ActorMCP}); !errors.Is(err, repo.ErrNameTaken) {
		t.Errorf("a duplicate name: %v, want ErrNameTaken", err)
	}
	entry := now.Add(-time.Hour)
	good := repo.Trade{InstrumentID: 1, Symbol: "7203", Direction: "LONG", EntryAt: entry, EntryPrice: 1000, ExitAt: now, ExitPrice: 1010, ExitReason: "take_profit", Quantity: 10, Net: 1, PnLJPY: 100}
	bad := good
	bad.InstrumentID, bad.EntryPrice = 2, 0 // violates CHECK (entry_price > 0)
	day := repo.Day{ProfileID: id, Day: "2026-01-08", Status: repo.DayDone, Trades: 2}

	if err := repo.RecordDay(ctx, db, day, []repo.Trade{good, bad}, now); err == nil {
		t.Fatal("a trade that violates the schema was stored")
	}
	var trades, days int
	_ = db.QueryRow(`SELECT COUNT(*) FROM virtual_paper_trades`).Scan(&trades)
	_ = db.QueryRow(`SELECT COUNT(*) FROM virtual_paper_days`).Scan(&days)
	if trades != 0 || days != 0 {
		t.Fatalf("a failed day left %d trades and %d marks, want none (one transaction)", trades, days)
	}

	day.Trades = 1
	for range 2 { // the same day twice: no duplicate row
		if err := repo.RecordDay(ctx, db, day, []repo.Trade{good}, now); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM virtual_paper_trades`).Scan(&trades)
	if trades != 1 {
		t.Errorf("%d trades after recording the day twice, want 1", trades)
	}
	rows, err := repo.FinishedTrades(ctx, db, id, "2026-01-01", "2026-02-01", 10)
	if err != nil || len(rows) != 1 || rows[0].Net != 1 {
		t.Errorf("FinishedTrades = %+v, %v", rows, err)
	}
	if _, err := repo.FinishedTrades(ctx, db, id, "2026-01-01", "2026-02-01", 0); !errors.Is(err, repo.ErrTooManyRows) {
		t.Errorf("over the limit: %v, want ErrTooManyRows", err)
	}
	// A trade for a profile that does not exist is refused (the only foreign key).
	if err := repo.RecordDay(ctx, db, repo.Day{ProfileID: 999, Day: "2026-01-08", Status: repo.DayEmpty}, nil, now); err == nil {
		t.Error("a day mark of an unknown profile was stored")
	}
}

// ResetFailedDay deletes only a failed mark: done and empty marks (and their
// trades) and a missing mark are left alone and reported as not reset.
func TestResetFailedDay_OnlyFailedMarks(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	now := time.Date(2026, 1, 9, 8, 0, 0, 0, time.UTC)
	id, err := repo.InsertProfile(ctx, db, repo.NewProfile{Name: "p", ParamsJSON: "{}", CreatedAt: now, FirstDay: "2026-01-02", Actor: repo.ActorMCP})
	if err != nil {
		t.Fatal(err)
	}
	for day, status := range map[string]string{"2026-01-05": repo.DayDone, "2026-01-06": repo.DayEmpty, "2026-01-07": repo.DayFailed} {
		if err := repo.RecordDay(ctx, db, repo.Day{ProfileID: id, Day: day, Status: status}, nil, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, day := range []string{"2026-01-05", "2026-01-06", "2026-01-08"} {
		if reset, err := repo.ResetFailedDay(ctx, db, id, day); err != nil || reset {
			t.Errorf("ResetFailedDay(%s) = %v, %v; want false (not a failed mark)", day, reset, err)
		}
	}
	if d, err := repo.GetDay(ctx, db, id, "2026-01-05"); err != nil || d.Status != repo.DayDone {
		t.Errorf("done mark = %+v, %v; want it kept", d, err)
	}
	if reset, err := repo.ResetFailedDay(ctx, db, id, "2026-01-07"); err != nil || !reset {
		t.Fatalf("ResetFailedDay(failed) = %v, %v; want true", reset, err)
	}
	if _, err := repo.GetDay(ctx, db, id, "2026-01-07"); !errors.Is(err, repo.ErrDayNotFound) {
		t.Errorf("GetDay after the reset: %v, want ErrDayNotFound", err)
	}
}
