package researchtests

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func TestCoverageShowsMissingDays(t *testing.T) {
	path, _ := seed(t)
	srv := newTestServer(t, paperresultsmcp.Options{DBPath: path})
	d := srv.call(t, args(nil)).data(t)
	c := d.Coverage
	// 10-13 is recorded and keeps its 4 snapshots (market_snapshots purged them), 10-14 is recorded with 3,
	// 10-15 is not recorded and has 2 snapshots in market_snapshots.
	if c.Unit != "snapshots" || c.InPeriod != 9 {
		t.Errorf("coverage unit/in_period = %s/%d, want snapshots/9", c.Unit, c.InPeriod)
	}
	if c.Labeled["30m"] != 5 || c.Labeled["60m"] != 2 || c.Labeled["close"] != 1 {
		t.Errorf("labeled = %v, want 30m 5, 60m 2, close 1 (the stray row of the unrecorded day does not count)", c.Labeled)
	}
	if c.UnrecordedDays != 1 {
		t.Errorf("unrecorded_days = %d, want 1 (10-15)", c.UnrecordedDays)
	}
	if len(c.SnapshotDays) != 3 {
		t.Fatalf("snapshot_days = %+v, want the three days that have snapshots", c.SnapshotDays)
	}
	a, b, u := c.SnapshotDays[0], c.SnapshotDays[1], c.SnapshotDays[2]
	if a.Day != "2026-10-13" || !a.Recorded || a.Snapshots != 4 || a.Labeled["30m"] != 3 || a.Labeled["60m"] != 2 || a.Labeled["close"] != 1 {
		t.Errorf("day 10-13 = %+v", a)
	}
	if b.Day != "2026-10-14" || !b.Recorded || b.Snapshots != 3 || b.Labeled["30m"] != 2 || b.Labeled["60m"] != 0 {
		t.Errorf("day 10-14 = %+v", b)
	}
	if u.Day != "2026-10-15" || u.Recorded || u.Snapshots != 2 || u.Labeled["30m"] != 0 {
		t.Errorf("day 10-15 = %+v", u)
	}

	// The stray row of the unrecorded day is in no statistic: the 30m LONG numbers are the five recorded rows'.
	if l := horizon(t, d, "30m").Long; l == nil || l.Rows != 4 || !near(l.MeanGrossBps, 10) {
		t.Errorf("30m long = %+v, want the recorded rows only (a stray gross of 999 would show)", l)
	}

	// A narrower period only counts its own days.
	d = srv.call(t, map[string]any{"from": "2026-10-14", "to": "2026-10-15"}).data(t)
	if d.Coverage.InPeriod != 3 || d.Coverage.Labeled["30m"] != 2 || d.Coverage.UnrecordedDays != 0 {
		t.Errorf("one-day coverage = %+v", d.Coverage)
	}
	wantSide(t, "10-14 short", horizon(t, d, "30m").Short, 2, 1, 12.5, 0.5, 0.5, 12.5, 0.5)
	// A period holding only the unrecorded day: its snapshots are visible, nothing is summarized.
	d = srv.call(t, map[string]any{"from": "2026-10-15", "to": "2026-10-16"}).data(t)
	if h := horizon(t, d, "30m"); h.Long != nil || h.Short != nil || d.Coverage.InPeriod != 2 || d.Coverage.UnrecordedDays != 1 {
		t.Errorf("unrecorded-day period = %+v", d)
	}
	// An empty period: no sides, no error; recorded_through is a property of the record, not the period.
	d = srv.call(t, map[string]any{"from": "2026-07-01", "to": "2026-07-05"}).data(t)
	if h := horizon(t, d, "30m"); h.Long != nil || h.Short != nil || d.Coverage.InPeriod != 0 || d.Coverage.UnrecordedDays != 0 {
		t.Errorf("empty period = %+v", d)
	}
	if d.RecordedThrough == nil || *d.RecordedThrough != "2026-10-14" {
		t.Errorf("recorded_through = %v, want 2026-10-14 (10-15 is not complete)", d.RecordedThrough)
	}
}

func TestNothingRecordedYet(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	_ = w
	d := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, args(nil)).data(t)
	if d.RecordedThrough != nil || d.Coverage.InPeriod != 0 || len(d.Horizons) != 3 {
		t.Errorf("empty database = %+v", d)
	}
	if d.Notice == "" {
		t.Error("no notice on an empty answer")
	}
}
