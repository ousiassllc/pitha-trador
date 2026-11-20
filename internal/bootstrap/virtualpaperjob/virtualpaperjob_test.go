package virtualpaperjob_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/virtualpaperjob"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
	"github.com/ousiassllc/pitha-trador/internal/service/virtualpaper"
)

var jst = time.FixedZone("JST", 9*60*60)

func at(day string, h, m int) time.Time {
	d, err := time.ParseInLocation(time.DateOnly, day, jst)
	if err != nil {
		panic(err)
	}
	return d.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// source counts its reads and, when boom is set, panics or fails like a broken
// backtest source.
type source struct {
	calls int
	panic bool
	err   error
}

func (s *source) RunConfigs(context.Context, backtest.Period) ([]backtest.RunConfig, error) {
	s.calls++
	if s.panic {
		panic("source blew up")
	}
	return nil, s.err
}

func profile(t *testing.T, db *sql.DB, name string, created time.Time) {
	t.Helper()
	if _, err := virtualpaper.Create(context.Background(), db, created, "", virtualpaper.CreateArgs{Name: name}); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The default (no profile): Run is skipped, nothing is read or written, and
// through the real maintenance runner not even its last-success date is stored.
func TestRun_DefaultWithoutProfilesWritesNothing(t *testing.T) {
	db := openDB(t)
	src := &source{}
	now := at("2026-01-07", 16, 30)
	job := virtualpaperjob.NewWithSource(db, src).WithClock(func() time.Time { return now })
	if err := job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Run = %v, want ErrSkipped", err)
	}

	state := system.NewRuntimeSettingsRepository(db)
	runner := maintenance.NewRunner(state, maintenance.WithClock(func() time.Time { return now }), maintenance.WithChunkWait(func(context.Context) {}))
	runner.Add(job.Task())
	runner.CatchUp(context.Background())
	if src.calls != 0 {
		t.Errorf("the source was read %d times", src.calls)
	}
	for _, tbl := range []string{"runtime_settings", "virtual_paper_profiles", "virtual_paper_days", "virtual_paper_trades"} {
		if n := count(t, db, `SELECT COUNT(*) FROM `+tbl); n != 0 {
			t.Errorf("%s has %d rows after an idle catch-up, want 0 (a default install gains no write)", tbl, n)
		}
	}
}

// Before 16:00 JST the day is not complete: skipped, nothing read.
func TestRun_BeforeTheCloseIsSkipped(t *testing.T) {
	db := openDB(t)
	profile(t, db, "p", at("2026-01-05", 12, 0))
	src := &source{}
	job := virtualpaperjob.NewWithSource(db, src).WithClock(func() time.Time { return at("2026-01-07", 15, 59) })
	if err := job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) || src.calls != 0 {
		t.Fatalf("Run = %v (calls %d), want ErrSkipped without reading", err, src.calls)
	}
}

// A panic in the recording is an error for the runner, never a crash of the
// process that also trades; a failing source is an error too.
func TestRun_PanicAndFailureAreErrors(t *testing.T) {
	db := openDB(t)
	profile(t, db, "p", at("2026-01-05", 12, 0))
	now := func() time.Time { return at("2026-01-07", 16, 30) }

	err := virtualpaperjob.NewWithSource(db, &source{panic: true}).WithClock(now).Run(context.Background())
	if err == nil || errors.Is(err, maintenance.ErrSkipped) || errors.Is(err, maintenance.ErrMore) {
		t.Fatalf("a panicking source: %v, want a plain error", err)
	}
	err = virtualpaperjob.NewWithSource(db, &source{err: errors.New("down")}).WithClock(now).Run(context.Background())
	if err == nil || errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("a failing source: %v, want an error", err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM virtual_paper_days`); n != 0 {
		t.Errorf("%d day marks after failures, want 0", n)
	}
}

// With a backlog the job returns ErrMore between chunks (one day each) and nil
// at the end; a day without bars is marked empty. The runner drives it to the
// end and stores its last-success date only then.
func TestRun_ChunksThroughTheMaintenanceRunner(t *testing.T) {
	db := openDB(t)
	profile(t, db, "p", at("2026-01-02", 12, 0))
	src := &source{}
	now := at("2026-01-06", 16, 30) // days 3, 4, 5
	job := virtualpaperjob.NewWithSource(db, src).WithClock(func() time.Time { return now })

	if err := job.Run(context.Background()); !errors.Is(err, maintenance.ErrMore) {
		t.Fatalf("first chunk = %v, want ErrMore", err)
	}
	pauses := 0
	runner := maintenance.NewRunner(system.NewRuntimeSettingsRepository(db), maintenance.WithClock(func() time.Time { return now }),
		maintenance.WithChunkWait(func(context.Context) { pauses++ }))
	runner.Add(job.Task())
	runner.CatchUp(context.Background())
	if n := count(t, db, `SELECT COUNT(*) FROM virtual_paper_days WHERE status = 'empty'`); n != 3 {
		t.Errorf("%d empty days, want 3", n)
	}
	if pauses == 0 {
		t.Error("the runner never paused between chunks")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM runtime_settings WHERE key LIKE '%virtual_paper%'`); n != 1 {
		t.Errorf("%d last-success rows, want 1 once the backlog is done", n)
	}
}
