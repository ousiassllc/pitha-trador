package metalabel_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

func TestRepository_RetrainSkipsRoundTripNewestFirstWithinLimit(t *testing.T) {
	r := metalabel.NewRepository(newTestDB(t))
	ctx := context.Background()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 17, 0, 0, 0, time.UTC) }
	for _, s := range []metalabel.RetrainSkip{
		{SkippedAt: day(12), ReasonCode: metalabel.SkipTooFewRowsBeforeOOS, UsableRows: 40, RequiredRows: 130},
		{SkippedAt: day(14), ReasonCode: metalabel.SkipTooFewRowsAfterPurge, UsableRows: 128, RequiredRows: 130},
		{SkippedAt: day(13), ReasonCode: metalabel.SkipTrainingDataUnusable, UsableRows: 300, RequiredRows: 130},
	} {
		saved, err := r.InsertRetrainSkip(ctx, s)
		if err != nil || saved.ID == 0 || saved.CreatedAt.IsZero() {
			t.Fatalf("InsertRetrainSkip(%s) = %+v, %v; want the row with ID and CreatedAt set", s.ReasonCode, saved, err)
		}
	}

	got, err := r.ListRetrainSkips(ctx, 2)
	if err != nil {
		t.Fatalf("ListRetrainSkips: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListRetrainSkips returned %d rows, want 2 (the limit)", len(got))
	}
	if got[0].ReasonCode != metalabel.SkipTooFewRowsAfterPurge || got[0].UsableRows != 128 || got[0].RequiredRows != 130 || !got[0].SkippedAt.Equal(day(14)) {
		t.Errorf("newest skip = %+v, want too_few_rows_after_purge 128/130 on day 14", got[0])
	}
	if got[1].ReasonCode != metalabel.SkipTrainingDataUnusable {
		t.Errorf("second skip = %+v, want the day-13 skip", got[1])
	}
}

func TestRepository_RetrainSkipRejectsAnEmptyReasonCode(t *testing.T) {
	r := metalabel.NewRepository(newTestDB(t))
	if _, err := r.InsertRetrainSkip(context.Background(), metalabel.RetrainSkip{SkippedAt: time.Now(), RequiredRows: 130}); err == nil {
		t.Fatal("InsertRetrainSkip with an empty reason code succeeded, want a CHECK failure")
	}
}

// Issue #862: a skip keeps the breakdown of what the history held (rows dropped
// as incomplete, labeled decisions per label_version) and both sides of the
// split, and reads it back. The SELECT is the one the issue asks to run on a
// real DB, here on the test DB.
func TestRepository_RetrainSkipKeepsTheRowBreakdown(t *testing.T) {
	db := newTestDB(t)
	r := metalabel.NewRepository(db)
	ctx := context.Background()
	at := time.Date(2026, 10, 26, 9, 8, 0, 0, time.UTC)
	want := metalabel.RetrainSkip{
		SkippedAt: at, ReasonCode: metalabel.SkipTooFewRowsBeforeOOS, UsableRows: 38, RequiredRows: 130,
		OOSRows: new(720), RequiredOOSRows: new(740), DroppedIncomplete: new(2), LabelV1Rows: new(31), LabelV2Rows: new(760),
	}
	if _, err := r.InsertRetrainSkip(ctx, want); err != nil {
		t.Fatalf("InsertRetrainSkip: %v", err)
	}
	got, err := r.ListRetrainSkips(ctx, 5)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListRetrainSkips = %+v, %v, want 1 row", got, err)
	}
	g := got[0]
	if g.UsableRows != 38 || g.RequiredRows != 130 || !hasInts(g, 720, 740, 2, 31, 760) {
		t.Errorf("read back %+v, want %+v", g, want)
	}

	var line string
	if err := db.QueryRowContext(ctx, `SELECT 'reason_code=' || reason_code || ' usable_rows=' || usable_rows || ' required_rows=' || required_rows ||
		' oos_rows=' || IFNULL(oos_rows, 'NULL') || ' required_oos_rows=' || IFNULL(required_oos_rows, 'NULL') ||
		' dropped_incomplete=' || IFNULL(dropped_incomplete, 'NULL') ||
		' label_v1_rows=' || IFNULL(label_v1_rows, 'NULL') || ' label_v2_rows=' || IFNULL(label_v2_rows, 'NULL')
		FROM metalabel_retrain_skips ORDER BY id DESC LIMIT 5`).Scan(&line); err != nil {
		t.Fatalf("SELECT * FROM metalabel_retrain_skips: %v", err)
	}
	t.Log(line)
}

// hasInts reports whether the five breakdown counts of s are all recorded and
// equal to oos, requiredOOS, dropped, v1, v2.
func hasInts(s metalabel.RetrainSkip, oos, requiredOOS, dropped, v1, v2 int) bool {
	for i, p := range []*int{s.OOSRows, s.RequiredOOSRows, s.DroppedIncomplete, s.LabelV1Rows, s.LabelV2Rows} {
		if p == nil || *p != []int{oos, requiredOOS, dropped, v1, v2}[i] {
			return false
		}
	}
	return true
}

// The five breakdown counts are NULL when they were not recorded and the real
// count when they were: a night that counted 0 stores 0, which reads back as 0,
// never as "not recorded", and a skip written without them reads back nil.
func TestRepository_RetrainSkipBreakdownDistinguishesZeroFromNotRecorded(t *testing.T) {
	db := newTestDB(t)
	r := metalabel.NewRepository(db)
	ctx := context.Background()
	at := time.Date(2026, 10, 26, 9, 8, 0, 0, time.UTC)
	base := metalabel.RetrainSkip{ReasonCode: metalabel.SkipTooFewRowsBeforeOOS, RequiredRows: 130}
	zero := base
	zero.OOSRows, zero.RequiredOOSRows, zero.DroppedIncomplete, zero.LabelV1Rows, zero.LabelV2Rows = new(0), new(0), new(0), new(0), new(0)
	pos := base
	pos.OOSRows, pos.RequiredOOSRows, pos.DroppedIncomplete, pos.LabelV1Rows, pos.LabelV2Rows = new(3), new(740), new(2), new(31), new(760)
	for i, s := range []metalabel.RetrainSkip{base, zero, pos} {
		s.SkippedAt = at.Add(time.Duration(i) * time.Hour)
		if _, err := r.InsertRetrainSkip(ctx, s); err != nil {
			t.Fatalf("InsertRetrainSkip %d: %v", i, err)
		}
	}
	got, err := r.ListRetrainSkips(ctx, 5) // newest first: positive, zero, not recorded
	if err != nil || len(got) != 3 {
		t.Fatalf("ListRetrainSkips = %+v, %v, want 3 rows", got, err)
	}
	if !hasInts(got[0], 3, 740, 2, 31, 760) {
		t.Errorf("positive counts read back as %+v", got[0])
	}
	if !hasInts(got[1], 0, 0, 0, 0, 0) {
		t.Errorf("zero counts read back as %+v, want recorded 0 (not nil)", got[1])
	}
	n := got[2]
	if n.OOSRows != nil || n.RequiredOOSRows != nil || n.DroppedIncomplete != nil || n.LabelV1Rows != nil || n.LabelV2Rows != nil {
		t.Errorf("a skip written without the counts read back as %+v, want all nil", n)
	}
	var nulls int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM metalabel_retrain_skips WHERE dropped_incomplete IS NULL AND label_v1_rows IS NULL
		AND label_v2_rows IS NULL AND oos_rows IS NULL AND required_oos_rows IS NULL`).Scan(&nulls); err != nil || nulls != 1 {
		t.Errorf("rows with all five NULL = %d, %v, want 1 (only the one written without them)", nulls, err)
	}
}
