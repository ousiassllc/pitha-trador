package retention

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/snapshotarchive"
)

// archiveFixture is a Service with a real archiver in a temp folder over a
// database holding expired and recent snapshots (fixedNow is 2026-09-29
// 12:00, so the cutoff is 2026-07-01 12:00 and 06-30 and earlier expire).
type archiveFixture struct {
	s        *Service
	db       *sql.DB
	archiver *snapshotarchive.Archiver
	inst     int64
}

func newArchiveFixture(t *testing.T) *archiveFixture {
	t.Helper()
	s, db := newService(t, Policy{})
	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := snapshotarchive.New(db, filepath.Join(t.TempDir(), "snapshot_archive"))
	s.archiver = a
	return &archiveFixture{s: s, db: db, archiver: a, inst: inst.ID}
}

func (f *archiveFixture) insert(t *testing.T, start time.Time, n int) {
	t.Helper()
	for i := range n {
		insertSnapshotWithVector(t, f.db, f.inst, start.Add(time.Duration(i)*time.Minute))
	}
}

func (f *archiveFixture) seed(t *testing.T) {
	t.Helper()
	f.insert(t, time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC), 5) // expired day
	f.insert(t, time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC), 2) // expired day
	f.insert(t, time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC), 3)  // the cutoff's own date: kept, not archived
	f.insert(t, time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC), 4) // recent
}

func (f *archiveFixture) purgeTimestamps(t *testing.T) []string {
	t.Helper()
	rows, err := f.db.Query(`SELECT substr(timestamp, 1, 10) FROM market_snapshots ORDER BY timestamp`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// recorder wraps the archiver and notes what the database held while the
// archive step ran, i.e. whether the delete had started.
type recorder struct {
	inner        SnapshotArchiver
	db           *sql.DB
	rowsDuring   int
	calls        int
	afterArchive func()
}

func (r *recorder) ArchiveBefore(ctx context.Context, cutoff time.Time) (snapshotarchive.Result, error) {
	r.calls++
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM market_snapshots`).Scan(&r.rowsDuring)
	res, err := r.inner.ArchiveBefore(ctx, cutoff)
	if r.afterArchive != nil {
		r.afterArchive()
	}
	return res, err
}

func TestPurge_ArchivesExpiredDaysBeforeDeleting(t *testing.T) {
	f := newArchiveFixture(t)
	f.s.batchSize = 2
	f.seed(t)
	rec := &recorder{inner: f.archiver, db: f.db}
	f.s.archiver = rec

	if err := f.s.Purge(context.Background()); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	if rec.calls != 1 || rec.rowsDuring != 14 {
		t.Fatalf("archive ran %d times with %d rows present; want once, before any delete (14 rows)", rec.calls, rec.rowsDuring)
	}
	m, err := snapshotarchive.ReadManifest(f.archiver.Dir())
	if err != nil {
		t.Fatal(err)
	}
	archived := 0
	for _, d := range m.Days {
		archived += d.Rows
	}
	if len(m.Days) != 2 || m.Days[0].Date != "2026-06-29" || m.Days[0].Rows != 5 || m.Days[1].Rows != 2 {
		t.Fatalf("manifest days = %+v, want 06-29 (5 rows) and 06-30 (2 rows)", m.Days)
	}
	deleted := 14 - count(t, f.db, `SELECT COUNT(*) FROM market_snapshots`)
	if archived != deleted || deleted != 7 {
		t.Fatalf("archived %d rows, deleted %d; want both 7", archived, deleted)
	}
	got := f.purgeTimestamps(t)
	if len(got) != 7 || got[0] != "2026-07-01" || got[3] != "2026-09-28" {
		t.Fatalf("remaining rows' dates = %v, want the 3 cutoff-date rows and 4 recent ones", got)
	}
	if n := count(t, f.db, `SELECT COUNT(*) FROM market_snapshot_vectors`); n != 7 {
		t.Fatalf("vectors = %d, want 7 (purged with their snapshots)", n)
	}
	for _, date := range []string{"2026-07-01", "2026-09-28"} {
		if _, err := os.Stat(filepath.Join(f.archiver.Dir(), "market_snapshots", date[:4], "market_snapshots-"+date+".csv.gz")); !os.IsNotExist(err) {
			t.Errorf("%s must not be archived (stat err = %v)", date, err)
		}
	}
	r, err := f.archiver.Reconcile(context.Background(), "2026-06-29")
	if err != nil || r.Status != snapshotarchive.StatusArchiveOnly || r.ArchiveRows != 5 || r.MissingFromArchive != 0 {
		t.Fatalf("reconcile after purge = %+v, err %v", r, err)
	}
}

type failingArchiver struct{ err error }

func (f failingArchiver) ArchiveBefore(context.Context, time.Time) (snapshotarchive.Result, error) {
	return snapshotarchive.Result{}, f.err
}

func TestPurge_ArchiveFailureDeletesNoSnapshotsButStillPurgesJobs(t *testing.T) {
	f := newArchiveFixture(t)
	f.seed(t)
	insertJob(t, f.db, "succeeded", 30)
	boom := errors.New("disk full")
	f.s.archiver = failingArchiver{err: boom}

	err := f.s.Purge(context.Background())

	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "market snapshots") {
		t.Fatalf("Purge err = %v, want the archive failure reported for market snapshots", err)
	}
	if n := count(t, f.db, `SELECT COUNT(*) FROM market_snapshots`); n != 14 {
		t.Fatalf("market_snapshots = %d, want 14 (nothing deleted when archiving failed)", n)
	}
	if n := count(t, f.db, `SELECT COUNT(*) FROM market_snapshot_vectors`); n != 14 {
		t.Fatalf("vectors = %d, want 14", n)
	}
	if got := jobCount(t, f.db, "succeeded"); got != 0 {
		t.Fatalf("succeeded jobs = %d, want 0 (the jobs purge is independent)", got)
	}
}

func TestPurge_UnwritableArchiveFolderDeletesNothing(t *testing.T) {
	f := newArchiveFixture(t)
	f.seed(t)
	if err := os.WriteFile(f.archiver.Dir(), []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := f.s.Purge(context.Background()); err == nil {
		t.Fatal("Purge must report the archive failure")
	}
	if n := count(t, f.db, `SELECT COUNT(*) FROM market_snapshots`); n != 14 {
		t.Fatalf("market_snapshots = %d, want 14", n)
	}
}

func TestPurge_CorruptedArchiveBlocksTheDelete(t *testing.T) {
	f := newArchiveFixture(t)
	f.seed(t)
	if _, err := f.archiver.ArchiveBefore(context.Background(), f.s.now().AddDate(0, 0, -90)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.archiver.Dir(), "market_snapshots", "2026", "market_snapshots-2026-06-29.csv.gz")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw[:len(raw)-8], 0o600); err != nil { // cut off the gzip trailer
		t.Fatal(err)
	}

	if err := f.s.Purge(context.Background()); err == nil {
		t.Fatal("Purge must report the corrupt archive")
	}
	if n := count(t, f.db, `SELECT COUNT(*) FROM market_snapshots`); n != 14 {
		t.Fatalf("market_snapshots = %d, want 14 (unverified days are never deleted)", n)
	}
}

// A row inserted into an already archived date after the archive was read
// (late writer) must survive that run's delete, and be archived by the next.
func TestPurge_LateRowAfterArchiveIsNotDeletedUntilArchived(t *testing.T) {
	f := newArchiveFixture(t)
	f.seed(t)
	rec := &recorder{inner: f.archiver, db: f.db}
	rec.afterArchive = func() { f.insert(t, time.Date(2026, 6, 29, 20, 0, 0, 0, time.UTC), 1) }
	f.s.archiver = rec

	if err := f.s.Purge(context.Background()); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	got := f.purgeTimestamps(t)
	if len(got) != 8 || got[0] != "2026-06-29" {
		t.Fatalf("remaining dates = %v, want the late 06-29 row kept besides the 7 other rows", got)
	}

	rec.afterArchive = nil
	if err := f.s.Purge(context.Background()); err != nil {
		t.Fatalf("second Purge: %v", err)
	}
	if n := count(t, f.db, `SELECT COUNT(*) FROM market_snapshots`); n != 7 {
		t.Fatalf("market_snapshots = %d, want 7 after the late row was archived and deleted", n)
	}
	r, err := f.archiver.Reconcile(context.Background(), "2026-06-29")
	if err != nil || r.ArchiveRows != 6 || r.Status != snapshotarchive.StatusArchiveOnly {
		t.Fatalf("reconcile = %+v, err %v; want the archive to hold all 6 rows of the day", r, err)
	}
}

func TestPurge_WithoutArchiverKeepsTheExactCutoff(t *testing.T) {
	s, db := newService(t, Policy{})
	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	insertSnapshotWithVector(t, db, inst.ID, fixedNow.AddDate(0, 0, -90).Add(-time.Minute)) // just expired
	insertSnapshotWithVector(t, db, inst.ID, fixedNow.AddDate(0, 0, -90).Add(time.Minute))  // just inside

	if err := s.Purge(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM market_snapshots`); n != 1 {
		t.Fatalf("market_snapshots = %d, want 1", n)
	}
}

func TestNewWithSnapshotArchive_ArchivesNextToTheDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")
	s := NewWithSnapshotArchive(nil, Policy{}, dbPath)
	a, ok := s.archiver.(*snapshotarchive.Archiver)
	if !ok || a.Dir() != filepath.Join(filepath.Dir(dbPath), "snapshot_archive") {
		t.Fatalf("archiver = %#v", s.archiver)
	}
	if s.policy.SnapshotDays != DefaultSnapshotDays {
		t.Fatalf("SnapshotDays = %d, want the unchanged default %d", s.policy.SnapshotDays, DefaultSnapshotDays)
	}
}
