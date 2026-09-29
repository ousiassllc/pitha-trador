package retention

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

var fixedNow = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func newService(t *testing.T, policy Policy) (*Service, *sql.DB) {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := New(db, policy)
	s.now = func() time.Time { return fixedNow }
	return s, db
}

func daysAgo(d int) string {
	return fixedNow.AddDate(0, 0, -d).Format(time.RFC3339Nano)
}

// insertJob inserts a job; finishedDaysAgo < 0 leaves finished_at NULL (a
// live pending/running row).
func insertJob(t *testing.T, db *sql.DB, status string, finishedDaysAgo int) {
	t.Helper()
	var finishedAt any
	if finishedDaysAgo >= 0 {
		finishedAt = daysAgo(finishedDaysAgo)
	}
	if _, err := db.Exec(
		`INSERT INTO jobs (queue, payload_json, status, scheduled_at, finished_at, created_at)
		 VALUES ('market-data', '{}', ?, ?, ?, ?)`,
		status, daysAgo(100), finishedAt, daysAgo(100)); err != nil {
		t.Fatalf("insert job: %v", err)
	}
}

func jobCount(t *testing.T, db *sql.DB, status string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE status = ?`, status).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPurge_JobsHonourPerStatusRetention(t *testing.T) {
	s, db := newService(t, Policy{})

	insertJob(t, db, "succeeded", 8) // expired (> 7d)
	insertJob(t, db, "succeeded", 6) // kept
	insertJob(t, db, "failed", 20)   // kept: failed is held 30d
	insertJob(t, db, "failed", 31)   // expired (> 30d)
	insertJob(t, db, "pending", -1)  // live queue state, never purged
	insertJob(t, db, "running", -1)  // live queue state, never purged

	if err := s.Purge(context.Background()); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	for status, want := range map[string]int{"succeeded": 1, "failed": 1, "pending": 1, "running": 1} {
		if got := jobCount(t, db, status); got != want {
			t.Errorf("%s jobs = %d, want %d", status, got, want)
		}
	}
}

func TestPurge_JobsDeletesEveryBatch(t *testing.T) {
	s, db := newService(t, Policy{})
	s.batchSize = 3
	for range 10 { // 3 full batches + a partial one
		insertJob(t, db, "succeeded", 30)
	}

	if err := s.Purge(context.Background()); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if got := jobCount(t, db, "succeeded"); got != 0 {
		t.Fatalf("succeeded jobs = %d, want 0 (all batches purged)", got)
	}
}

func TestPurge_CustomPolicy(t *testing.T) {
	s, db := newService(t, Policy{SucceededJobDays: 1, FailedJobDays: 2})
	insertJob(t, db, "succeeded", 2)
	insertJob(t, db, "failed", 3)
	insertJob(t, db, "failed", 1)

	if err := s.Purge(context.Background()); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if got := jobCount(t, db, "succeeded"); got != 0 {
		t.Errorf("succeeded jobs = %d, want 0", got)
	}
	if got := jobCount(t, db, "failed"); got != 1 {
		t.Errorf("failed jobs = %d, want 1", got)
	}
}

func insertSnapshotWithVector(t *testing.T, db *sql.DB, instrumentID int64, ts time.Time) int64 {
	t.Helper()
	snap, err := repository.NewSnapshotRepository(db).Insert(context.Background(), domain.Snapshot{
		InstrumentID: instrumentID, Symbol: "7203", Timestamp: ts,
		Price: 100, Volume: 1, Turnover: 100, RawDataJSON: "{}",
		Feature: domain.Feature{VWAP: 100},
	})
	if err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	embedding := "[" + strings.TrimSuffix(strings.Repeat("0,", 14), ",") + "]"
	if _, err := db.Exec(`INSERT INTO market_snapshot_vectors (snapshot_id, embedding) VALUES (?, ?)`,
		snap.ID, embedding); err != nil {
		t.Fatalf("insert vector: %v", err)
	}
	return snap.ID
}

func count(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPurge_SnapshotsAndTheirVectors(t *testing.T) {
	s, db := newService(t, Policy{})
	s.batchSize = 2
	inst, err := repository.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	var expired []int64
	for i := range 5 { // spans 2 full batches + a partial one
		expired = append(expired, insertSnapshotWithVector(t, db, inst.ID, fixedNow.AddDate(0, 0, -91).Add(time.Duration(i)*time.Minute)))
	}
	kept := insertSnapshotWithVector(t, db, inst.ID, fixedNow.AddDate(0, 0, -89))

	if err := s.Purge(context.Background()); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	if got := count(t, db, `SELECT COUNT(*) FROM market_snapshots`); got != 1 {
		t.Errorf("market_snapshots = %d, want 1 (only the recent bar)", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM market_snapshot_vectors`); got != 1 {
		t.Errorf("market_snapshot_vectors = %d, want 1 (vectors purged with their snapshots)", got)
	}
	if got := count(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM market_snapshots WHERE id = %d`, kept)); got != 1 {
		t.Errorf("recent snapshot %d was purged", kept)
	}
	for _, id := range expired {
		if got := count(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM market_snapshot_vectors WHERE snapshot_id = %d`, id)); got != 0 {
			t.Errorf("vector for expired snapshot %d still present", id)
		}
	}
}

func TestPurge_LeavesAuditTablesAlone(t *testing.T) {
	s, db := newService(t, Policy{})
	inst, err := repository.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
			question_version, response_json, latency_ms, model_id, created_at)
		 VALUES (?, '7203', ?, 'scout', 'h', '{}', 'v1', '{}', 1, 'm', ?)`,
		inst.ID, daysAgo(400), daysAgo(400)); err != nil {
		t.Fatalf("insert decision: %v", err)
	}

	if err := s.Purge(context.Background()); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM jev_decisions`); got != 1 {
		t.Fatalf("jev_decisions = %d, want 1 (audit table is never purged)", got)
	}
}

func TestPurge_ReturnsErrorFromFailingTableAndStillRunsOthers(t *testing.T) {
	s, db := newService(t, Policy{})
	insertJob(t, db, "succeeded", 30)
	if _, err := db.Exec(`DROP TABLE market_snapshot_vectors`); err != nil {
		t.Fatal(err)
	}
	inst, err := repository.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.NewSnapshotRepository(db).Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: "7203", Timestamp: fixedNow.AddDate(0, 0, -200),
		Price: 1, Volume: 1, Turnover: 1, RawDataJSON: "{}", Feature: domain.Feature{VWAP: 1},
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.Purge(context.Background()); err == nil {
		t.Fatal("Purge should report the snapshot purge failure")
	}
	if got := jobCount(t, db, "succeeded"); got != 0 {
		t.Fatalf("succeeded jobs = %d, want 0 (job purge must run despite the snapshot failure)", got)
	}
}
