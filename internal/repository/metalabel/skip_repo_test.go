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
		OOSRows: 720, RequiredOOSRows: 740, DroppedIncomplete: 2, LabelV1Rows: 31, LabelV2Rows: 760,
	}
	if _, err := r.InsertRetrainSkip(ctx, want); err != nil {
		t.Fatalf("InsertRetrainSkip: %v", err)
	}
	got, err := r.ListRetrainSkips(ctx, 5)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListRetrainSkips = %+v, %v, want 1 row", got, err)
	}
	g := got[0]
	if g.UsableRows != 38 || g.RequiredRows != 130 || g.OOSRows != 720 || g.RequiredOOSRows != 740 ||
		g.DroppedIncomplete != 2 || g.LabelV1Rows != 31 || g.LabelV2Rows != 760 {
		t.Errorf("read back %+v, want %+v", g, want)
	}

	var line string
	if err := db.QueryRowContext(ctx, `SELECT 'reason_code=' || reason_code || ' usable_rows=' || usable_rows || ' required_rows=' || required_rows ||
		' oos_rows=' || oos_rows || ' required_oos_rows=' || required_oos_rows || ' dropped_incomplete=' || dropped_incomplete ||
		' label_v1_rows=' || label_v1_rows || ' label_v2_rows=' || label_v2_rows
		FROM metalabel_retrain_skips ORDER BY id DESC LIMIT 5`).Scan(&line); err != nil {
		t.Fatalf("SELECT * FROM metalabel_retrain_skips: %v", err)
	}
	t.Log(line)
}
