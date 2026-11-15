package researchlabel

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/researchlabel"
)

// The default is one JST day per Run, and a Run that leaves days behind says so
// (Result.More): the backfill of the first 90 days is that many short chunks, so the
// maintenance lock is held for one day at a time.
func TestRecorder_DefaultIsOneDayPerRunAndReportsMore(t *testing.T) {
	db := openDB(t)
	for _, d := range []string{"2026-10-12", "2026-10-13", "2026-10-14"} {
		seedDay(t, db, d)
	}
	rec := NewRecorder(researchlabel.NewRepository(db), fixedCosts(Costs{Long: 19, Short: 27})).WithClock(fixedNow("2026-10-15 16:30"))
	for i, want := range []struct {
		day  string
		more bool
	}{{"2026-10-12", true}, {"2026-10-13", true}, {"2026-10-14", false}} {
		res, err := rec.Run(context.Background())
		if err != nil || strings.Join(res.Days, ",") != want.day || res.More != want.more {
			t.Fatalf("run %d = %+v, %v; want only %s, More=%v", i+1, res, err, want.day, want.more)
		}
	}
	if res, err := rec.Run(context.Background()); err != nil || len(res.Days) != 0 || res.More {
		t.Fatalf("run after the backfill = %+v, %v; want nothing and no More", res, err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_label_days`); n != 3 {
		t.Fatalf("marks = %d, want 3", n)
	}
}

// Retention: rows of days before today - N are deleted from the label table only
// (marks, day sums and market_snapshots stay), the newer rows are untouched, and a
// day outside the retention is not recorded at all. The days are trading days
// (13 Tue .. 16 Fri Oct 2026) so every day has the same number of rows.
func TestRecorder_RetentionDeletesOldRowsOfTheLabelTableOnly(t *testing.T) {
	db := openDB(t)
	days := []string{"2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16"}
	for _, d := range days {
		seedDay(t, db, d)
	}
	perDay := 2 * rowsPerInstrumentDay
	if _, err := newRecorder(db, "2026-10-17 16:30").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshots := count(t, db, `SELECT COUNT(*) FROM market_snapshots`)
	stats := count(t, db, `SELECT COUNT(*) FROM research_label_day_stats`)
	if got := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`); got != 4*perDay {
		t.Fatalf("rows before = %d, want %d", got, 4*perDay)
	}

	keep2 := func(context.Context) (int, error) { return 2, nil }
	rec := newRecorder(db, "2026-10-17 16:30").WithRetention(keep2)
	res, err := rec.Run(context.Background()) // cutoff 10-15: 10-13 and 10-14 go
	if err != nil || res.Purged != 2*perDay || res.More {
		t.Fatalf("run = %+v, %v; want %d rows purged", res, err, 2*perDay)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`); got != 2*perDay {
		t.Fatalf("rows after = %d, want %d", got, 2*perDay)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels WHERE day < '2026-10-15'`); n != 0 {
		t.Errorf("%d rows older than the retention remain", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_label_days`); n != 4 {
		t.Errorf("marks = %d, want all 4 kept (they stop a purged day being labeled again)", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_label_day_stats`); n != stats {
		t.Errorf("day sums %d -> %d: the sums of a purged day must stay", stats, n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM market_snapshots`); n != snapshots {
		t.Errorf("market_snapshots %d -> %d: the purge touched a table that is not a research table", snapshots, n)
	}
	// With the marks kept, the purged days are not recorded again.
	if again, err := rec.Run(context.Background()); err != nil || again.Purged != 0 || len(again.Days) != 0 {
		t.Fatalf("second run = %+v, %v; want nothing", again, err)
	}

	// A day outside the retention is not recorded (it would be deleted at once).
	fresh := openDB(t)
	for _, d := range days {
		seedDay(t, fresh, d)
	}
	res, err = newRecorder(fresh, "2026-10-17 16:30").WithRetention(keep2).Run(context.Background())
	if err != nil || strings.Join(res.Days, ",") != "2026-10-15,2026-10-16" {
		t.Fatalf("run = %+v, %v; want only the 2 days inside the retention", res, err)
	}
	if n := count(t, fresh, `SELECT COUNT(*) FROM research_horizon_labels WHERE day < '2026-10-15'`); n != 0 {
		t.Errorf("%d rows of days outside the retention were written", n)
	}
}

// A purge is chunked like the recording: at most maxPurge batches of PurgeBatchRows
// per Run, More while it is not finished, and the recording waits for the purge.
func TestRecorder_PurgeIsChunkedAndRecordingWaitsForIt(t *testing.T) {
	db := openDB(t)
	for _, d := range []string{"2026-10-13", "2026-10-14", "2026-10-15"} {
		seedDay(t, db, d)
	}
	if _, err := newRecorder(db, "2026-10-16 16:30").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	old := 3 * 2 * rowsPerInstrumentDay // 1074 rows: more than one batch of 1000
	if got := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`); got != old || old <= PurgeBatchRows {
		t.Fatalf("fixture has %d old rows (want %d, more than a batch of %d)", got, old, PurgeBatchRows)
	}
	seedDay(t, db, "2026-10-16")
	rec := newRecorder(db, "2026-10-19 16:30").WithMaxPurgeBatches(1).
		WithRetention(func(context.Context) (int, error) { return 3, nil }) // today 10-19: cutoff 10-16

	first, err := rec.Run(context.Background())
	if err != nil || first.Purged != PurgeBatchRows || !first.More || len(first.Days) != 0 {
		t.Fatalf("first run = %+v, %v; want one full batch purged, More, and no recording yet", first, err)
	}
	second, err := rec.Run(context.Background())
	if err != nil || second.Purged != old-PurgeBatchRows || second.More || strings.Join(second.Days, ",") != "2026-10-16" {
		t.Fatalf("second run = %+v, %v; want the rest purged and 10-16 recorded", second, err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels WHERE day < '2026-10-16'`); n != 0 {
		t.Errorf("%d old rows remain", n)
	}
}

// A retention that cannot be read is an error, not "keep nothing" or "keep all".
func TestRecorder_RetentionErrorIsReturnedAndNothingIsDeleted(t *testing.T) {
	db := openDB(t)
	seedDay(t, db, "2026-10-13")
	if _, err := newRecorder(db, "2026-10-15 16:30").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`)
	rec := newRecorder(db, "2026-10-30 16:30").WithRetention(func(context.Context) (int, error) { return 0, errors.New("settings unreadable") })
	if _, err := rec.Run(context.Background()); err == nil {
		t.Fatal("a settings failure was swallowed")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`); n != before {
		t.Fatalf("rows %d -> %d although the retention could not be read", before, n)
	}
}
