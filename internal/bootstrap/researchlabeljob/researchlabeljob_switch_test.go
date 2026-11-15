package researchlabeljob

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/backupjob"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/researchlabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/retention"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

func countOf(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The recording is opt-in: with nothing stored in runtime_settings (a default
// install) - or a stored 0, or a damaged value - the daily task writes nothing, not
// even the maintenance runner's last-success row, so the trading process gains no
// write from it. Turning the switch on is picked up by the next run, no restart.
func TestJob_IsOffByDefaultAndWritesNothingWhileOff(t *testing.T) {
	ctx := context.Background()
	tables := []string{"research_horizon_labels", "research_label_days", "research_label_day_stats", "runtime_settings", "market_snapshots"}
	snapshot := func(db *sql.DB) map[string]int {
		got := map[string]int{}
		for _, table := range tables {
			got[table] = countOf(t, db, table)
		}
		return got
	}
	for name, stored := range map[string]string{"nothing stored": "", "stored 0": "0", "damaged": `"yes"`} {
		job, db, settings := newOffJob(t)
		if stored != "" {
			if err := settings.Set(ctx, researchlabelsettings.KeyEnabled, stored, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		before := snapshot(db)
		now := time.Date(2026, 10, 14, 16, 30, 0, 0, jst)
		job.now = func() time.Time { return now }

		if err := job.Run(ctx); !errors.Is(err, maintenance.ErrSkipped) {
			t.Fatalf("%s: Run = %v, want maintenance.ErrSkipped", name, err)
		}
		// Through the real runner: a skipped task is not recorded as done either.
		runner := maintenance.NewRunner(settings, maintenance.WithClock(func() time.Time { return now }))
		runner.Add(job.Task())
		runner.CatchUp(ctx)
		if after := snapshot(db); fmt.Sprint(after) != fmt.Sprint(before) {
			t.Fatalf("%s: tables changed while the switch is off: %v -> %v", name, before, after)
		}
		if _, ok, _ := settings.Get(ctx, "system.maintenance."+TaskName+".last_success_date"); ok {
			t.Fatalf("%s: the runner stored a last-success date for a task that did nothing", name)
		}

		// Turned on, the very next run records.
		if err := settings.Set(ctx, researchlabelsettings.KeyEnabled, "1", time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := job.Run(ctx); err != nil || rows(t, db) == 0 {
			t.Fatalf("%s: after switching on, Run = %v, rows %d; want it to record", name, err, rows(t, db))
		}
	}
}

// Switching off stops the purge too: nothing is written, nothing is deleted.
func TestJob_SwitchedOffAfterRecordingKeepsTheRowsAndDeletesNothing(t *testing.T) {
	ctx := context.Background()
	job, db, settings := newJob(t)
	job.now = clockAt("2026-10-14 16:30")
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	recorded := rows(t, db)
	if err := settings.Set(ctx, researchlabelsettings.KeyEnabled, "0", time.Now()); err != nil {
		t.Fatal(err)
	}
	job.now = clockAt("2027-03-01 16:30") // far beyond the 90-day retention
	if err := job.Run(ctx); !errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Run = %v, want ErrSkipped", err)
	}
	if got := rows(t, db); got != recorded {
		t.Fatalf("rows %d -> %d while the switch is off", recorded, got)
	}
}

// The retention comes from Settings (default 90 days): rows of days older than
// today - N are deleted from the research label table and from nowhere else.
func TestJob_RetentionFromSettingsDeletesOldRowsOnlyFromTheResearchTable(t *testing.T) {
	ctx := context.Background()
	job, db, _ := newJob(t)
	job.now = clockAt("2026-10-14 16:30")
	if err := job.Run(ctx); err != nil { // records 10-13
		t.Fatal(err)
	}
	recorded := rows(t, db)
	snapshots, days := countOf(t, db, "market_snapshots"), countOf(t, db, "research_label_days")

	// Default retention 90 days: 89 days later the day is still kept, 91 days later it goes.
	job.now = clockAt("2027-01-10 16:30") // 10-14 + 88 days; today - 90 = 2026-10-12
	if err := job.Run(ctx); err != nil || rows(t, db) != recorded {
		t.Fatalf("within the default retention: Run = %v, rows %d, want %d kept", err, rows(t, db), recorded)
	}
	job.now = clockAt("2027-01-14 16:30") // today - 90 = 2026-10-16 > 10-13
	if err := job.Run(ctx); err != nil || rows(t, db) != 0 {
		t.Fatalf("beyond the default retention: Run = %v, rows %d, want 0", err, rows(t, db))
	}
	if got := countOf(t, db, "market_snapshots"); got != snapshots {
		t.Errorf("market_snapshots %d -> %d: only the research table may be purged", snapshots, got)
	}
	if got := countOf(t, db, "research_label_days"); got != days {
		t.Errorf("research_label_days %d -> %d: the marks stay", days, got)
	}

	// A shorter retention in Settings applies at the next run.
	job2, db2, settings2 := newJob(t)
	job2.now = clockAt("2026-10-14 16:30")
	if err := job2.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := settings2.Set(ctx, researchlabelsettings.KeyRetentionDays, "7", time.Now()); err != nil {
		t.Fatal(err)
	}
	job2.now = clockAt("2026-10-21 16:30") // today - 7 = 10-14 > 10-13
	if err := job2.Run(ctx); err != nil || rows(t, db2) != 0 {
		t.Fatalf("retention 7: Run = %v, rows %d, want 0", err, rows(t, db2))
	}
}

// The first run after switching on has up to 90 days to catch up. The task does
// one JST day per Run and returns maintenance.ErrMore, and the runner frees its
// lock between the days: the REAL database backup and the REAL retention purge,
// triggered while the backfill is half done (as the 16:00 end-of-day backup is),
// take the lock at once instead of waiting for the whole backfill.
func TestRunner_BackfillIsOneDayPerChunkAndBackupAndPurgeRunBetweenChunks(t *testing.T) {
	ctx := context.Background()
	job, db, settings := newJob(t)
	for _, d := range []string{"2026-10-14", "2026-10-15", "2026-10-16", "2026-10-19", "2026-10-20"} {
		seedDay(t, db, d)
	}
	backupDir := t.TempDir()
	if err := settings.Set(ctx, config.KeyBackupDir, fmt.Sprintf("%q", backupDir), time.Now()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 16, 30, 0, 0, jst)
	job.now = func() time.Time { return now }

	var (
		requestedAt time.Time // set by the gap hook just before it asks for the lock
		lockWaits   []time.Duration
		backups     int
		purges      int
		rowsAtGap   []int
	)
	acquired := func() {
		if !requestedAt.IsZero() {
			lockWaits = append(lockWaits, time.Since(requestedAt))
		}
	}
	// The purge is the real one with the real snapshot archive (issue #1072: the
	// market_snapshots are archived and verified before they are deleted). Its
	// cutoff is the real clock minus SnapshotDays, so the days are computed to put
	// the cutoff on 2026-10-14 (UTC): the purge in the first gap archives and
	// deletes 2026-10-13, which the first chunk has labeled already.
	dataDir := t.TempDir()
	cutoffDay := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	backuper := backupjob.New(db, settings, filepath.Join(dataDir, "pitha.db"))
	purger := retention.NewWithSnapshotArchive(db, retention.Policy{SnapshotDays: int(time.Since(cutoffDay) / (24 * time.Hour))}, filepath.Join(dataDir, "pitha.db"))
	var runner *maintenance.Runner
	runner = maintenance.NewRunner(settings, maintenance.WithClock(func() time.Time { return now }), maintenance.WithChunkWait(func(ctx context.Context) {
		rowsAtGap = append(rowsAtGap, rows(t, db))
		if _, ok, _ := settings.Get(ctx, "system.maintenance."+TaskName+".last_success_date"); ok {
			t.Error("research_labels was recorded as done before its backlog was finished")
		}
		requestedAt = time.Now()
		runner.RunNow(ctx, "database_backup")
		runner.RunNow(ctx, "data_retention_purge")
		requestedAt = time.Time{}
	}))
	runner.Add(maintenance.Task{Name: "database_backup", Label: "backup", Run: func(ctx context.Context) error {
		acquired()
		backups++
		return backuper.Backup(ctx)
	}})
	runner.Add(maintenance.Task{Name: "data_retention_purge", Label: "purge", Run: func(ctx context.Context) error {
		acquired()
		purges++
		if purges == 1 {
			return nil // the catch-up pass runs before the backfill (task order): nothing is labeled yet
		}
		return purger.Purge(ctx)
	}})
	runner.Add(job.Task())

	done := make(chan struct{})
	go func() { runner.CatchUp(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("CatchUp did not finish: the maintenance lock was held for the whole backfill")
	}

	const perDay = 179
	if want := []int{perDay, 2 * perDay, 3 * perDay, 4 * perDay, 5 * perDay}; fmt.Sprint(rowsAtGap) != fmt.Sprint(want) {
		t.Fatalf("rows at each gap = %v, want %v: one JST day per chunk, backup and purge run in between", rowsAtGap, want)
	}
	if got := rows(t, db); got != 6*perDay {
		t.Fatalf("rows after the backfill = %d, want %d", got, 6*perDay)
	}
	// The snapshot archive ran in a gap: 2026-10-13 is archived to the local folder and
	// deleted from market_snapshots, its labels (no foreign key) survive.
	if left := countOf(t, db, "market_snapshots WHERE timestamp < '2026-10-14'"); left != 0 {
		t.Errorf("%d market_snapshots of 2026-10-13 left after the archive and purge", left)
	}
	if entries, err := os.ReadDir(filepath.Join(dataDir, "snapshot_archive")); err != nil || len(entries) == 0 {
		t.Errorf("no snapshot archive written (%v)", err)
	}
	if backups != 1+5 || purges != 1+5 {
		t.Errorf("backups = %d, purges = %d; want the catch-up run and one fixed-time run in each of the 5 gaps", backups, purges)
	}
	for i, w := range lockWaits {
		if w > 2*time.Second {
			t.Errorf("gap %d: the backup/purge waited %v for the maintenance lock, want well under a chunk", i, w)
		}
	}
	if entries, err := os.ReadDir(backupDir); err != nil || len(entries) == 0 {
		t.Errorf("no backup written to %s (%v) while the backfill ran", backupDir, err)
	}
	if _, ok, _ := settings.Get(ctx, "system.maintenance."+TaskName+".last_success_date"); !ok {
		t.Error("research_labels was not recorded as done after the last chunk")
	}
}
