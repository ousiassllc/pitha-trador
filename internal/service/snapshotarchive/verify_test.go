package snapshotarchive

import (
	"context"
	"strings"
	"testing"
)

func TestVerifyWritten_RejectsRowCountIdsRangeAndLostRows(t *testing.T) {
	dbd := dbDay{IDs: []int64{1, 2}, FirstTimestamp: "2026-06-29T09:00", LastTimestamp: "2026-06-29T09:01"}
	ok := fileInfo{Rows: 2, IDs: map[int64]struct{}{1: {}, 2: {}}, FirstTimestamp: dbd.FirstTimestamp, LastTimestamp: dbd.LastTimestamp}
	if err := verifyWritten(ok, dbd, fileInfo{}); err != nil {
		t.Fatalf("matching file rejected: %v", err)
	}
	for name, mutate := range map[string]func(*fileInfo){
		"row count": func(f *fileInfo) { f.Rows = 1 },
		"ids":       func(f *fileInfo) { f.IDs = map[int64]struct{}{1: {}, 3: {}} },
		"first":     func(f *fileInfo) { f.FirstTimestamp = "2026-06-29T08:59" },
		"last":      func(f *fileInfo) { f.LastTimestamp = "2026-06-29T09:02" },
	} {
		bad := ok
		mutate(&bad)
		if err := verifyWritten(bad, dbd, fileInfo{}); err == nil {
			t.Errorf("%s mismatch accepted", name)
		}
	}
	// The previous archive also holds row 0 (purged since): the union must keep it.
	prev := fileInfo{Rows: 2, IDs: map[int64]struct{}{0: {}, 1: {}}, FirstTimestamp: "2026-06-29T08:59", LastTimestamp: "2026-06-29T09:00"}
	if err := verifyWritten(ok, dbd, prev); err == nil {
		t.Error("a file that dropped a row of the previous archive was accepted")
	}
	union := fileInfo{Rows: 3, IDs: map[int64]struct{}{0: {}, 1: {}, 2: {}}, FirstTimestamp: "2026-06-29T08:59", LastTimestamp: "2026-06-29T09:01"}
	if err := verifyWritten(union, dbd, prev); err != nil {
		t.Errorf("the union was rejected: %v", err)
	}
}

func TestReconcile_OneArchivedDayRowCountCheck(t *testing.T) {
	a, db, inst := newArchiver(t)
	insertSnapshots(t, db, inst, utc(2026, 6, 29, 9, 0), 120)
	mustArchive(t, a)

	r, err := a.Reconcile(context.Background(), "2026-06-29")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("row count check of one archived day:\n%s", r)
	if r.Status != StatusMatch || r.ArchiveRows != 120 || r.DBRows != 120 || r.MissingFromArchive != 0 ||
		r.ArchiveFirst != r.DBFirst || r.ArchiveLast != r.DBLast || !r.ManifestMatch {
		t.Fatalf("reconcile = %+v", r)
	}

	if _, err := db.Exec(`DELETE FROM market_snapshots`); err != nil {
		t.Fatal(err)
	}
	if r, _ = a.Reconcile(context.Background(), "2026-06-29"); r.Status != StatusArchiveOnly || r.ArchiveRows != 120 {
		t.Fatalf("after purge reconcile = %+v", r)
	}
	if r, _ = a.Reconcile(context.Background(), "2026-06-28"); r.Status != StatusMismatch || r.Problem == "" {
		t.Fatalf("missing file reconcile = %+v", r)
	}
}

func TestArchiveQueries_UseTheTimestampIndex(t *testing.T) {
	_, db, _ := newArchiver(t)
	for name, q := range map[string]struct {
		sql  string
		args []any
	}{
		"page":  {"EXPLAIN QUERY PLAN " + selectPageSQL, []any{"2026-06-29", "2026-06-30", 1 << 40, "", "", 0, 10}},
		"dates": {"EXPLAIN QUERY PLAN SELECT DISTINCT substr(timestamp, 1, 10) FROM market_snapshots WHERE timestamp < ? AND id <= ? ORDER BY 1", []any{"2026-07-01", 1 << 40}},
	} {
		rows, err := db.Query(q.sql, q.args...)
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var d string
			if err := rows.Scan(&id, &parent, &unused, &d); err != nil {
				t.Fatal(err)
			}
			details = append(details, d)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		if !strings.Contains(strings.Join(details, "|"), "market_snapshots_timestamp_idx") {
			t.Errorf("%s plan does not use market_snapshots_timestamp_idx: %v", name, details)
		}
	}
}
