package snapshotarchive

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

var cutoff = utc(2026, 7, 1, 12, 0) // 2026-06-30 and earlier are expired

func mustArchive(t *testing.T, a *Archiver) Result {
	t.Helper()
	res, err := a.ArchiveBefore(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("ArchiveBefore: %v", err)
	}
	return res
}

func TestArchiveBefore_WritesEachExpiredDayWholeAndNotTheRest(t *testing.T) {
	a, db, inst := newArchiver(t)
	a.pageSize = 3 // several pages per day
	insertSnapshots(t, db, inst, utc(2026, 6, 29, 9, 0), 5)
	insertSnapshots(t, db, inst, utc(2026, 6, 30, 23, 58), 2) // ends 23:59, still 06-30
	insertSnapshots(t, db, inst, utc(2026, 7, 1, 9, 0), 4)    // the cutoff's own date: not complete
	insertSnapshots(t, db, inst, utc(2026, 8, 1, 9, 0), 3)

	res := mustArchive(t, a)

	if res.DeleteBefore != "2026-07-01" {
		t.Fatalf("DeleteBefore = %q, want 2026-07-01", res.DeleteBefore)
	}
	var got []string
	for _, d := range res.Days {
		got = append(got, d.Date+":"+strconv.Itoa(d.Rows)+":"+d.Action)
	}
	if want := []string{"2026-06-29:5:written", "2026-06-30:2:written"}; !slices.Equal(got, want) {
		t.Fatalf("days = %v, want %v", got, want)
	}
	for _, date := range []string{"2026-07-01", "2026-08-01"} {
		if _, err := os.Stat(a.filePath(date)); !os.IsNotExist(err) {
			t.Errorf("%s must not be archived yet (stat err = %v)", date, err)
		}
	}
	m, err := ReadManifest(a.Dir())
	if err != nil || len(m.Days) != 2 || m.Days[0].File != "2026/market_snapshots-2026-06-29.csv.gz" {
		t.Fatalf("manifest = %+v, err %v", m, err)
	}
	if countRows(t, db) != 14 {
		t.Fatal("archiving must not delete anything")
	}
}

func TestArchiveFile_HasEveryColumnAndExactValues(t *testing.T) {
	a, db, inst := newArchiver(t)
	insertSnapshots(t, db, inst, utc(2026, 6, 29, 9, 0), 2)
	mustArchive(t, a)

	f, err := os.Open(a.filePath("2026-06-29"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(gz).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	dbCols, err := db.Query(`SELECT * FROM market_snapshots LIMIT 1`)
	if err != nil {
		t.Fatal(err)
	}
	cols, _ := dbCols.Columns()
	if err := dbCols.Err(); err != nil {
		t.Fatal(err)
	}
	_ = dbCols.Close()
	if !slices.Equal(recs[0], cols) {
		t.Fatalf("header = %v, want every table column %v", recs[0], cols)
	}
	for _, want := range []string{"raw_data_json", "vwap", "lendable", "return_5m", "price_limit"} {
		if !slices.Contains(recs[0], want) {
			t.Errorf("header lacks %s", want)
		}
	}
	row := map[string]string{}
	for i, c := range recs[0] {
		row[c] = recs[1][i]
	}
	if row["raw_data_json"] != `{"note":"a,b \"quoted\"\nline"}` || row["bid"] != "99.5" ||
		row["lendable"] != "1" || row["return_5m"] != "" || len(recs) != 3 {
		t.Fatalf("row = %v (records %d)", row, len(recs))
	}
}

func TestArchiveFile_ModesAndNoLeftoverTempFiles(t *testing.T) {
	a, db, inst := newArchiver(t)
	insertSnapshots(t, db, inst, utc(2026, 6, 29, 9, 0), 2)
	mustArchive(t, a)

	var names []string
	_ = filepath.Walk(a.Dir(), func(p string, info os.FileInfo, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, filepath.Base(p))
		if strings.HasPrefix(filepath.Base(p), ".tmp-") {
			t.Errorf("leftover temporary file %s", p)
		}
		if runtime.GOOS != "windows" {
			want := fileMode
			if info.IsDir() {
				want = dirMode
			}
			if info.Mode().Perm() != want {
				t.Errorf("%s mode = %v, want %v", p, info.Mode().Perm(), want)
			}
		}
		return nil
	})
	if !slices.Contains(names, "manifest.json") {
		t.Fatalf("no manifest in %v", names)
	}
}

func TestArchiveBefore_IdempotentRerun(t *testing.T) {
	a, db, inst := newArchiver(t)
	insertSnapshots(t, db, inst, utc(2026, 6, 29, 9, 0), 4)
	first := mustArchive(t, a)
	a.now = func() time.Time { return fixedNow.Add(48 * time.Hour) }
	second := mustArchive(t, a)

	if second.Days[0].Action != "verified" {
		t.Fatalf("second run action = %q, want verified", second.Days[0].Action)
	}
	if first.Days[0].SHA256 != second.Days[0].SHA256 || second.Days[0].ArchivedAt != first.Days[0].ArchivedAt {
		t.Fatalf("re-run changed the archive: %+v -> %+v", first.Days[0], second.Days[0])
	}
}

func TestArchiveBefore_LateRowInArchivedDayIsReArchivedAndExcludedFromTheFirstBound(t *testing.T) {
	a, db, inst := newArchiver(t)
	insertSnapshots(t, db, inst, utc(2026, 6, 29, 9, 0), 4)
	first := mustArchive(t, a)
	late := insertSnapshots(t, db, inst, utc(2026, 6, 29, 14, 0), 1)[0]
	if late <= first.MaxID {
		t.Fatalf("late id %d must exceed the verified MaxID %d", late, first.MaxID)
	}

	second := mustArchive(t, a)

	if d := second.Days[0]; d.Action != "rewritten" || d.Rows != 5 || d.LastTimestamp[:16] != "2026-06-29T14:00" {
		t.Fatalf("late row not re-archived: %+v", d)
	}
	r, err := a.Reconcile(context.Background(), "2026-06-29")
	if err != nil || r.Status != StatusMatch || r.ArchiveRows != 5 || !r.ManifestMatch {
		t.Fatalf("reconcile = %+v, err %v", r, err)
	}
}

func TestArchiveBefore_ArchiveHoldingMoreRowsThanTheDatabaseIsKept(t *testing.T) {
	a, db, inst := newArchiver(t)
	ids := insertSnapshots(t, db, inst, utc(2026, 6, 29, 9, 0), 4)
	first := mustArchive(t, a)
	// A purge interrupted part-way through the date.
	if _, err := db.Exec(`DELETE FROM market_snapshots WHERE id IN (?, ?)`, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}

	second := mustArchive(t, a)

	if d := second.Days[0]; d.Action != "verified" || d.Rows != 4 || d.SHA256 != first.Days[0].SHA256 {
		t.Fatalf("archive must stay untouched and keep all 4 rows: %+v", d)
	}
}

// A purge interrupted part-way through a date and a late row in the same
// date: the archive becomes the union, never losing the purged rows.
func TestArchiveBefore_PurgedRowsAndLateRowBothEndUpInTheArchive(t *testing.T) {
	a, db, inst := newArchiver(t)
	ids := insertSnapshots(t, db, inst, utc(2026, 6, 29, 9, 0), 3)
	mustArchive(t, a)
	if _, err := db.Exec(`DELETE FROM market_snapshots WHERE id = ?`, ids[0]); err != nil {
		t.Fatal(err)
	}
	insertSnapshots(t, db, inst, utc(2026, 6, 29, 15, 0), 1)

	res := mustArchive(t, a)

	if d := res.Days[0]; d.Action != "rewritten" || d.Rows != 4 {
		t.Fatalf("day = %+v, want a rewritten file with 4 rows (3 original + 1 late)", d)
	}
	r, err := a.Reconcile(context.Background(), "2026-06-29")
	if err != nil || r.Status != StatusArchiveOnly || r.ArchiveRows != 4 || r.DBRows != 3 || r.MissingFromArchive != 0 {
		t.Fatalf("reconcile = %+v, err %v", r, err)
	}
}

func TestArchiveBefore_CorruptOrTruncatedArchiveFailsClosed(t *testing.T) {
	a, db, inst := newArchiver(t)
	insertSnapshots(t, db, inst, utc(2026, 6, 29, 9, 0), 200)
	mustArchive(t, a)
	path := a.filePath("2026-06-29")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw[:len(raw)/2], fileMode); err != nil {
		t.Fatal(err)
	}

	_, err = a.ArchiveBefore(context.Background(), cutoff)

	if err == nil || !strings.Contains(err.Error(), "unusable") {
		t.Fatalf("err = %v, want the corrupt archive refused", err)
	}
	if got, _ := os.ReadFile(path); len(got) != len(raw)/2 {
		t.Fatal("the corrupt file must be left as found")
	}
}

func TestArchiveBefore_UnwritableDestinationFailsClosed(t *testing.T) {
	a, db, inst := newArchiver(t)
	insertSnapshots(t, db, inst, utc(2026, 6, 29, 9, 0), 3)
	if err := os.WriteFile(a.dir, []byte("a file, not a folder"), fileMode); err != nil {
		t.Fatal(err)
	}

	res, err := a.ArchiveBefore(context.Background(), cutoff)

	if err == nil || len(res.Days) != 0 || res.DeleteBefore != "" {
		t.Fatalf("res = %+v, err = %v; want an error and no deletion bound", res, err)
	}
}
