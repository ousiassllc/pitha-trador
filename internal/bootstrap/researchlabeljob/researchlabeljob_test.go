package researchlabeljob

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	servicelabel "github.com/ousiassllc/pitha-trador/internal/service/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

func newJob(t *testing.T) (*Job, *sql.DB, *system.RuntimeSettingsRepository) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`); err != nil {
		t.Fatal(err)
	}
	// Bars 14:00..15:29 JST of 2026-10-13, price rising 1 yen a minute, quote price -/+ 1.
	start := time.Date(2026, 10, 13, 14, 0, 0, 0, jst)
	for i := 0; i < 90; i++ {
		p := 1000 + float64(i)
		if _, err := db.Exec(`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, spread_bps, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json)
			VALUES (1, '7203', ?, ?, ?, ?, 20, 1, 1, 1, 1, '{}')`, sqlutil.FormatTime(start.Add(time.Duration(i)*time.Minute)), p, p-1, p+1); err != nil {
			t.Fatal(err)
		}
	}
	settings := system.NewRuntimeSettingsRepository(db)
	return New(db, settings), db, settings
}

func clockAt(s string) func() time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, jst)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return t }
}

func rows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM research_horizon_labels`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestJob_SkipsBeforeTheMarketDayIsOverThenRecords(t *testing.T) {
	for now, due := range map[string]bool{
		"2026-10-14 09:00": false,
		"2026-10-14 15:59": false,
		"2026-10-14 16:00": true,
		"2026-10-14 23:30": true,
	} {
		job, db, _ := newJob(t)
		job.now = clockAt(now)
		err := job.Run(context.Background())
		if due {
			if err != nil {
				t.Errorf("%s: Run = %v, want success", now, err)
			}
			if n := rows(t, db); n != 179 { // see service/researchlabel: 62 + 32 + 85
				t.Errorf("%s: rows = %d, want 179", now, n)
			}
			continue
		}
		if !errors.Is(err, maintenance.ErrSkipped) {
			t.Errorf("%s: Run = %v, want maintenance.ErrSkipped", now, err)
		}
		if n := rows(t, db); n != 0 {
			t.Errorf("%s: %d rows written by a skipped run", now, n)
		}
	}
}

func TestJob_TheMarketDayIsJSTNotTheHostZone(t *testing.T) {
	job, db, _ := newJob(t)
	// 06:59 UTC is 15:59 JST, 07:00 UTC is 16:00 JST.
	job.now = func() time.Time { return time.Date(2026, 10, 14, 6, 59, 0, 0, time.UTC) }
	if err := job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Errorf("15:59 JST: Run = %v, want ErrSkipped", err)
	}
	job.now = func() time.Time { return time.Date(2026, 10, 14, 7, 0, 0, 0, time.UTC) }
	if err := job.Run(context.Background()); err != nil || rows(t, db) == 0 {
		t.Errorf("16:00 JST: Run = %v, rows %d, want it to record", err, rows(t, db))
	}
}

func TestJob_NeverLabelsTodayEvenAfterTheClose(t *testing.T) {
	job, db, _ := newJob(t)
	job.now = clockAt("2026-10-13 17:00") // the fixture's own day, after 16:00
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := rows(t, db); n != 0 {
		t.Errorf("%d rows labeled for today's day", n)
	}
}

func TestJob_UsesTheCostSettingsOfTheRun(t *testing.T) {
	job, db, settings := newJob(t)
	if err := settings.Set(context.Background(), barriersettings.KeyCostBps, "14", time.Now()); err != nil {
		t.Fatal(err)
	}
	job.now = clockAt("2026-10-14 16:30")
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var cost float64
	if err := db.QueryRow(`SELECT MIN(long_cost_bps) FROM research_horizon_labels`).Scan(&cost); err != nil || cost != 14 {
		t.Fatalf("long cost = %v, %v; want the Settings barrier cost 14", cost, err)
	}
}

func TestJob_AFailureIsAnErrorForTheRunnerNotAPanic(t *testing.T) {
	job, _, _ := newJob(t)
	job.now = clockAt("2026-10-14 16:30")
	job.record = func(context.Context) (r servicelabel.Result, err error) { return r, errors.New("boom") }
	err := job.Run(context.Background())
	if err == nil || errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Run = %v, want a failure the runner counts", err)
	}
	job.record = func(context.Context) (servicelabel.Result, error) { panic("bug in the recorder") }
	if err := job.Run(context.Background()); err == nil {
		t.Fatal("a panic in the recorder escaped Run")
	}
}

// A failing research task must not stop the others: the runner isolates tasks.
func TestRunner_AFailingResearchTaskDoesNotStopOtherTasks(t *testing.T) {
	job, _, _ := newJob(t)
	job.now = clockAt("2026-10-14 16:30")
	job.record = func(context.Context) (servicelabel.Result, error) { return servicelabel.Result{}, errors.New("boom") }
	other := 0
	now := time.Date(2026, 10, 14, 16, 30, 0, 0, jst)
	runner := maintenance.NewRunner(nil, maintenance.WithClock(func() time.Time { return now }))
	runner.Add(job.Task())
	runner.Add(maintenance.Task{Name: "other", Label: "other", Run: func(context.Context) error { other++; return nil }})
	runner.CatchUp(context.Background())
	if other != 1 {
		t.Fatalf("other task ran %d times, want 1", other)
	}
	if got := job.Task(); got.Name != "research_labels" || got.Label == "" {
		t.Errorf("task = %+v", got)
	}
}
