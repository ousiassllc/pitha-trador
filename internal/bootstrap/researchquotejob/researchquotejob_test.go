package researchquotejob

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
	"github.com/ousiassllc/pitha-trador/internal/service/snapshotarchive"
)

// now is 2026-10-08 16:00 JST: with 30 days of retention the first day kept is 2026-09-08.
var now = time.Date(2026, 10, 8, 16, 0, 0, 0, jst)

type fixture struct {
	t        *testing.T
	job      *Job
	db       *sql.DB
	settings *system.RuntimeSettingsRepository
	dir      string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "pitha.db")
	db, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := system.NewRuntimeSettingsRepository(db)
	if err := settings.Set(context.Background(), tachibanasource.KeyResearchQuotesRetentionDays, "30", now); err != nil {
		t.Fatal(err)
	}
	job := New(db, settings, dbPath)
	job.now = func() time.Time { return now }
	job.pause = 0
	job.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() } // no real waiting
	return &fixture{t: t, job: job, db: db, settings: settings, dir: snapshotarchive.DefaultDir(dbPath)}
}

// seed inserts n one-minute rows (symbol 7203) from 09:00 JST of the date.
func (f *fixture) seed(date string, n int) {
	f.t.Helper()
	start, err := time.ParseInLocation("2006-01-02 15:04", date+" 09:00", jst)
	if err != nil {
		f.t.Fatal(err)
	}
	for i := range n {
		if _, err := f.db.Exec(`INSERT INTO research_quote_samples (timestamp, source, symbol, price, via, session_phase)
			VALUES (?, 'tachibana', '7203', 3000, 'event', 'morning')`, sqlutil.FormatTime(start.Add(time.Duration(i)*time.Minute))); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *fixture) count(where string) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM research_quote_samples WHERE ` + where).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *fixture) archived(date string) bool {
	_, err := os.Stat(filepath.Join(f.dir, "research_quote_samples", date[:4], "research_quote_samples-"+date+".csv.gz"))
	return err == nil
}

func TestRunArchivesThenDeletesOnlyDaysBeforeTheCutoff(t *testing.T) {
	f := newFixture(t)
	f.seed("2026-09-05", 4) // expired
	f.seed("2026-09-07", 3) // expired (the day before the cutoff)
	f.seed("2026-09-08", 2) // the first day kept
	f.seed("2026-10-07", 5)

	if err := f.job.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := f.count(`timestamp < '2026-09-08'`); got != 0 {
		t.Errorf("%d expired rows remain, want 0", got)
	}
	if got := f.count(`timestamp >= '2026-09-08'`); got != 7 {
		t.Errorf("%d rows on and after the cutoff remain, want all 7 untouched", got)
	}
	for date, want := range map[string]bool{"2026-09-05": true, "2026-09-07": true, "2026-09-08": false, "2026-10-07": false} {
		if f.archived(date) != want {
			t.Errorf("archive file of %s present = %v, want %v", date, !want, want)
		}
	}
	m, err := snapshotarchive.ReadTableManifest(f.dir, snapshotarchive.ResearchQuoteTable)
	if err != nil || len(m.Days) != 2 || m.Days[0].Rows != 4 || m.Days[1].Rows != 3 {
		t.Errorf("manifest = %+v, %v, want the two expired days with 4 and 3 rows", m, err)
	}
	// A second run has nothing left to do.
	if err := f.job.Run(context.Background()); err != nil {
		t.Errorf("second Run: %v", err)
	}
}

// Fail closed: a damaged archive file of one day (the existing file is never
// overwritten) blocks every delete, including the days that archive fine.
func TestRunDeletesNothingWhenAnArchiveOrItsVerificationFails(t *testing.T) {
	f := newFixture(t)
	f.seed("2026-09-05", 4)
	f.seed("2026-09-06", 3)
	bad := filepath.Join(f.dir, "research_quote_samples", "2026", "research_quote_samples-2026-09-06.csv.gz")
	if err := os.MkdirAll(filepath.Dir(bad), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("not a gzip file"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := f.job.Run(context.Background())
	if err == nil || errors.Is(err, maintenance.ErrSkipped) || errors.Is(err, maintenance.ErrMore) {
		t.Fatalf("Run = %v, want a failure", err)
	}
	if got := f.count(`1 = 1`); got != 7 {
		t.Errorf("%d rows remain, want all 7 (nothing deleted without a verified archive)", got)
	}
	if got, _ := os.ReadFile(bad); string(got) != "not a gzip file" {
		t.Error("the damaged archive file was overwritten")
	}
}

type failingArchiver struct{}

func (failingArchiver) ArchiveBefore(context.Context, time.Time) (snapshotarchive.Result, error) {
	return snapshotarchive.Result{}, errors.New("disk full")
}

func TestRunDeletesNothingWhenTheArchiverFails(t *testing.T) {
	f := newFixture(t)
	f.seed("2026-09-05", 4)
	f.job.archiver = failingArchiver{}
	if err := f.job.Run(context.Background()); err == nil {
		t.Fatal("Run succeeded with a failing archiver")
	}
	if got := f.count(`1 = 1`); got != 4 {
		t.Errorf("%d rows remain, want 4", got)
	}
}

func TestRunWritesNothingWhenTheTableIsEmpty(t *testing.T) {
	f := newFixture(t)
	if err := f.job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Run = %v, want ErrSkipped", err)
	}
	if _, err := os.Stat(f.dir); !os.IsNotExist(err) {
		t.Errorf("the archive folder was created for an empty table (stat err = %v)", err)
	}
}

// The retention is read at every run (a Settings change applies at once), and
// the job does not depend on the recording switch.
func TestRunFollowsTheRetentionSettingAndIgnoresTheRecordingSwitch(t *testing.T) {
	f := newFixture(t)
	if err := f.settings.Set(context.Background(), tachibanasource.KeyResearchQuotesEnabled, "0", now); err != nil {
		t.Fatal(err)
	}
	f.seed("2026-09-20", 2)
	f.seed("2026-10-01", 2)
	if err := f.job.Run(context.Background()); err != nil { // 30 days: 09-20 is kept
		t.Fatal(err)
	}
	if got := f.count(`1 = 1`); got != 4 {
		t.Fatalf("%d rows after a 30-day run, want 4", got)
	}
	if err := f.settings.Set(context.Background(), tachibanasource.KeyResearchQuotesRetentionDays, "10", now); err != nil {
		t.Fatal(err)
	}
	if err := f.job.Run(context.Background()); err != nil { // 10 days: first kept day is 09-28
		t.Fatal(err)
	}
	if got := f.count(`timestamp < '2026-09-28'`); got != 0 || f.count(`1 = 1`) != 2 {
		t.Errorf("after a 10-day run %d expired rows remain of %d, want 0 of 2", got, f.count(`1 = 1`))
	}
}

// A big backlog is deleted in bounded batches: a Run stops at its batch limit
// and returns ErrMore (the runner frees its lock between the calls), pausing
// between batches.
func TestRunDeletesInBoundedBatchesAndReturnsErrMore(t *testing.T) {
	f := newFixture(t)
	f.job.batchSize, f.job.maxBatch = 3, 2
	pauses := 0
	f.job.sleep = func(ctx context.Context, _ time.Duration) error { pauses++; return ctx.Err() }
	f.seed("2026-09-05", 10)
	f.seed("2026-10-07", 1)

	if err := f.job.Run(context.Background()); !errors.Is(err, maintenance.ErrMore) {
		t.Fatalf("first Run = %v, want ErrMore", err)
	}
	if got := f.count(`timestamp < '2026-09-08'`); got != 4 || pauses != 2 {
		t.Errorf("after one Run %d expired rows remain with %d pauses, want 4 and 2 (two batches of 3, a pause after each)", got, pauses)
	}
	if err := f.job.Run(context.Background()); err != nil {
		t.Fatalf("second Run = %v, want success", err)
	}
	if got := f.count(`1 = 1`); got != 1 {
		t.Errorf("%d rows remain, want only the kept day's 1", got)
	}
}

func TestRunStopsWhenTheContextEnds(t *testing.T) {
	f := newFixture(t)
	f.job.batchSize = 2
	f.seed("2026-09-05", 6)
	ctx, cancel := context.WithCancel(context.Background())
	f.job.sleep = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
	if err := f.job.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	if got := f.count(`1 = 1`); got != 4 {
		t.Errorf("%d rows remain, want 4 after one batch", got)
	}
}
