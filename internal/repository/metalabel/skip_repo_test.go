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
