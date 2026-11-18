package evalrecord_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// A day too large to evaluate, or an evaluation that fails, is an error that writes nothing
// of that day (no partial row), and the day is tried again by the next Run.
func TestRecorder_FailureWritesNothingForTheDay(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	seed(t, w)
	for name, fail := range map[string]error{"too large": query.ErrLimitExceeded, "other": errors.New("boom")} {
		rec := newRecorder(t, w, "2026-10-05 16:30", 365).WithRows(func(context.Context, query.Range) ([]evaldiag.Row, evaldiag.Meta, error) {
			return nil, evaldiag.Meta{}, fail
		})
		if _, err := rec.Run(t.Context()); !errors.Is(err, fail) {
			t.Fatalf("%s: Run = %v, want %v", name, err, fail)
		}
		if n := count(t, w, "jev_evaluation_daily"); n != 0 {
			t.Fatalf("%s: %d rows written by a failed day", name, n)
		}
	}
	// The real evaluation succeeds afterwards on the same day.
	if res, err := newRecorder(t, w, "2026-10-05 16:30", 365).Run(t.Context()); err != nil || res.Day != "2026-10-05" || res.Rows != 2 {
		t.Fatalf("retry = %+v, %v", res, err)
	}
}

// An evaluation whose row counts disagree with its cohort counts is refused, not stored.
func TestRecorder_InconsistentEvaluationIsRefused(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	seed(t, w)
	rec := newRecorder(t, w, "2026-10-05 16:30", 365).WithRows(func(context.Context, query.Range) ([]evaldiag.Row, evaldiag.Meta, error) {
		return nil, evaldiag.Meta{HorizonMinutes: 10, Cohorts: []evaldiag.CohortCount{{ModelID: "m1", QuestionVersion: "v3", Decisions: 2, Evaluated: 2}}}, nil
	})
	if _, err := rec.Run(t.Context()); err == nil || count(t, w, "jev_evaluation_daily") != 0 {
		t.Fatalf("Run = %v, rows %d; want an error and no row", err, count(t, w, "jev_evaluation_daily"))
	}
}
