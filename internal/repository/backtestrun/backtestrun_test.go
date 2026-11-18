package backtestrun_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"

	"github.com/ousiassllc/pitha-trador/db"
	"github.com/ousiassllc/pitha-trador/internal/repository/backtestrun"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

var t0 = time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func newRun(owner string) backtestrun.NewRun {
	return backtestrun.NewRun{
		Owner: owner, Client: "100.64.0.1", FromDate: "2026-10-01", ToDate: "2026-10-15", ReplayMode: "paper",
		CodeVersion: "dev", ExploredCount: 2, RequestJSON: `{"variants":[{"label":"a","params":{}}]}`,
	}
}

func TestLifecycle_QueuedRunningSucceeded(t *testing.T) {
	ctx := context.Background()
	conn := openDB(t)
	run, err := backtestrun.Insert(ctx, conn, t0, newRun("p1"))
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != 1 || run.Status != backtestrun.StatusQueued {
		t.Fatalf("inserted %+v", run)
	}
	if err := backtestrun.Start(ctx, conn, t0.Add(time.Second), run.ID); err != nil {
		t.Fatal(err)
	}
	first, last := t0.Add(-48*time.Hour), t0.Add(-time.Hour)
	if err := backtestrun.Finish(ctx, conn, t0.Add(time.Minute), run.ID, backtestrun.Outcome{
		ResultJSON: `{"variants":[]}`, FirstSnapshotAt: &first, LastSnapshotAt: &last,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := backtestrun.Get(ctx, conn, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != backtestrun.StatusSucceeded || got.ResultJSON != `{"variants":[]}` || got.ErrorCode != "" {
		t.Fatalf("got %+v", got)
	}
	if got.StartedAt == nil || !got.StartedAt.Equal(t0.Add(time.Second)) || got.FinishedAt == nil || !got.FinishedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("timestamps %v %v", got.StartedAt, got.FinishedAt)
	}
	if got.FirstSnapshotAt == nil || !got.FirstSnapshotAt.Equal(first) || got.LastSnapshotAt == nil || !got.LastSnapshotAt.Equal(last) {
		t.Fatalf("coverage %v %v", got.FirstSnapshotAt, got.LastSnapshotAt)
	}
	// An ended run is left alone by a late Finish.
	if err := backtestrun.Finish(ctx, conn, t0.Add(time.Hour), run.ID, backtestrun.Outcome{ErrorCode: backtestrun.ErrorTimedOut}); err != nil {
		t.Fatal(err)
	}
	if again, _ := backtestrun.Get(ctx, conn, run.ID); again.Status != backtestrun.StatusSucceeded {
		t.Fatalf("a finished run changed: %+v", again)
	}
}

func TestFinish_FailedFromQueuedNeedsNoStart(t *testing.T) {
	ctx := context.Background()
	conn := openDB(t)
	run, _ := backtestrun.Insert(ctx, conn, t0, newRun("p1"))
	if err := backtestrun.Finish(ctx, conn, t0, run.ID, backtestrun.Outcome{ErrorCode: backtestrun.ErrorUnavailable}); err != nil {
		t.Fatal(err)
	}
	got, _ := backtestrun.Get(ctx, conn, run.ID)
	if got.Status != backtestrun.StatusFailed || got.ErrorCode != backtestrun.ErrorUnavailable || got.ResultJSON != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestFinish_SucceededFromQueuedFillsStartedAt(t *testing.T) {
	ctx := context.Background()
	conn := openDB(t)
	run, _ := backtestrun.Insert(ctx, conn, t0, newRun("p1"))
	if err := backtestrun.Finish(ctx, conn, t0, run.ID, backtestrun.Outcome{ResultJSON: "[]"}); err != nil {
		t.Fatalf("a success that skipped the running mark must still satisfy the table's checks: %v", err)
	}
}

func TestFailOrphans_OnlyOtherOwnersUnfinishedRuns(t *testing.T) {
	ctx := context.Background()
	conn := openDB(t)
	mine, _ := backtestrun.Insert(ctx, conn, t0, newRun("p2"))
	old1, _ := backtestrun.Insert(ctx, conn, t0, newRun("p1"))
	old2, _ := backtestrun.Insert(ctx, conn, t0, newRun("p1"))
	done, _ := backtestrun.Insert(ctx, conn, t0, newRun("p1"))
	_ = backtestrun.Start(ctx, conn, t0, mine.ID)
	_ = backtestrun.Start(ctx, conn, t0, old2.ID)
	_ = backtestrun.Finish(ctx, conn, t0, done.ID, backtestrun.Outcome{ResultJSON: "[]"})

	n, err := backtestrun.FailOrphans(ctx, conn, t0.Add(time.Hour), "p2")
	if err != nil || n != 2 {
		t.Fatalf("FailOrphans = %d, %v; want 2", n, err)
	}
	want := map[int64]string{mine.ID: backtestrun.StatusRunning, old1.ID: backtestrun.StatusFailed, old2.ID: backtestrun.StatusFailed, done.ID: backtestrun.StatusSucceeded}
	for id, status := range want {
		got, _ := backtestrun.Get(ctx, conn, id)
		if got.Status != status {
			t.Errorf("run %d: status %q, want %q", id, got.Status, status)
		}
		if status == backtestrun.StatusFailed && got.ErrorCode != backtestrun.ErrorInterrupted {
			t.Errorf("run %d: error_code %q, want interrupted", id, got.ErrorCode)
		}
	}
}

func TestListAndGet(t *testing.T) {
	ctx := context.Background()
	conn := openDB(t)
	for range 5 {
		if _, err := backtestrun.Insert(ctx, conn, t0, newRun("p1")); err != nil {
			t.Fatal(err)
		}
	}
	_ = backtestrun.Finish(ctx, conn, t0, 4, backtestrun.Outcome{ErrorCode: backtestrun.ErrorInternal})

	page, more, err := backtestrun.List(ctx, conn, backtestrun.ListFilter{Limit: 2})
	if err != nil || !more || len(page) != 2 || page[0].ID != 5 || page[1].ID != 4 {
		t.Fatalf("page 1 = %+v more=%v err=%v", page, more, err)
	}
	page, more, err = backtestrun.List(ctx, conn, backtestrun.ListFilter{Limit: 2, BeforeID: 4})
	if err != nil || !more || len(page) != 2 || page[0].ID != 3 {
		t.Fatalf("page 2 = %+v more=%v err=%v", page, more, err)
	}
	page, more, err = backtestrun.List(ctx, conn, backtestrun.ListFilter{Limit: 5, Status: backtestrun.StatusFailed})
	if err != nil || more || len(page) != 1 || page[0].ID != 4 || page[0].ResultJSON != "" {
		t.Fatalf("failed filter = %+v more=%v err=%v", page, more, err)
	}
	if _, err := backtestrun.Get(ctx, conn, 99); !errors.Is(err, backtestrun.ErrNotFound) {
		t.Fatalf("Get(99) = %v, want ErrNotFound", err)
	}
}

func TestTableRefusesInconsistentRows(t *testing.T) {
	ctx := context.Background()
	conn := openDB(t)
	const prefix = `INSERT INTO backtest_runs (status, owner, client, created_at, started_at, finished_at, period_from, period_to, replay_mode, code_version, explored_count, request_json, result_json, error_code)
		VALUES `
	for name, values := range map[string]string{
		"unknown status":                `('paused', 'o', 'c', 't', NULL, NULL, '2026-10-01', '2026-10-15', 'paper', 'dev', 1, '{}', NULL, NULL)`,
		"succeeded without a result":    `('succeeded', 'o', 'c', 't', 't', 't', '2026-10-01', '2026-10-15', 'paper', 'dev', 1, '{}', NULL, NULL)`,
		"failed without an error code":  `('failed', 'o', 'c', 't', NULL, 't', '2026-10-01', '2026-10-15', 'paper', 'dev', 1, '{}', NULL, NULL)`,
		"an error text instead of code": `('failed', 'o', 'c', 't', NULL, 't', '2026-10-01', '2026-10-15', 'paper', 'dev', 1, '{}', NULL, 'open /home/x/pitha.db: denied')`,
		"running without finish mark":   `('running', 'o', 'c', 't', NULL, NULL, '2026-10-01', '2026-10-15', 'paper', 'dev', 1, '{}', NULL, NULL)`,
		"queued with a finish time":     `('queued', 'o', 'c', 't', NULL, 't', '2026-10-01', '2026-10-15', 'paper', 'dev', 1, '{}', NULL, NULL)`,
		"thirteen variants":             `('queued', 'o', 'c', 't', NULL, NULL, '2026-10-01', '2026-10-15', 'paper', 'dev', 13, '{}', NULL, NULL)`,
		"unknown replay mode":           `('queued', 'o', 'c', 't', NULL, NULL, '2026-10-01', '2026-10-15', 'shadow', 'dev', 1, '{}', NULL, NULL)`,
		"a period that is not a date":   `('queued', 'o', 'c', 't', NULL, NULL, '2026-10', '2026-10-15', 'paper', 'dev', 1, '{}', NULL, NULL)`,
	} {
		if _, err := conn.ExecContext(ctx, prefix+values); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Migration 65 creates backtest_runs and nothing else; stepping back removes it.
func TestMigration000065_UpAndDown(t *testing.T) {
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	src, err := iofs.New(db.MigrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	drv, err := migratesqlite.WithInstance(conn, &migratesqlite.Config{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
	if err != nil {
		t.Fatal(err)
	}
	exists := func() bool {
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('backtest_runs', 'backtest_runs_status_idx')`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 2
	}
	if err := m.Migrate(64); err != nil {
		t.Fatalf("migrate to 64: %v", err)
	}
	if exists() {
		t.Fatal("backtest_runs exists before migration 65")
	}
	if err := m.Migrate(65); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !exists() {
		t.Fatal("backtest_runs or its index is missing after migration 65")
	}
	if err := m.Migrate(64); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if exists() {
		t.Fatal("backtest_runs survived the down migration")
	}
}
