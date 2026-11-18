package evalrecord_test

import (
	"fmt"
	"testing"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Rows older than today - retention are deleted from jev_evaluation_daily, in chunks of
// PurgeBatchRows (More while the batch budget of a Run ran out), the newer rows stay, and no
// other table changes.
func TestRetention_DeletesOldRowsInChunksFromTheRecordOnly(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	seed(t, w)
	for range 3 {
		if _, err := newRecorder(t, w, "2026-10-07 16:30", 365).Run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	recent := count(t, w, "jev_evaluation_daily") // 4 rows of 10-05..10-07
	// 2500 old rows (2026-03-xx and older) of distinct cohorts.
	testkit.MustExec(t, w, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 2500)
INSERT INTO jev_evaluation_daily (day, model_id, question_version, horizon_minutes, take_profit_bps, stop_loss_bps, barrier_cost_bps,
	decisions, evaluated, entered, wins, llm_sum_bps, random_sum_bps, momentum_sum_bps, contrarian_sum_bps)
SELECT '2026-03-' || printf('%02d', 1 + i % 28), 'old-' || i, 'v1', 10, 30, 30, 10, 1, 1, 0, 0, 1, 1, 1, 1 FROM n`)
	others := map[string]int{}
	tables := []string{"jev_decisions", "market_snapshots", "trade_signals", "instruments", "runtime_settings", "calibration_outcomes", "research_model_runs", "research_model_predictions"}
	for _, tb := range tables {
		others[tb] = count(t, w, tb)
	}

	rec := newRecorder(t, w, "2026-10-07 16:30", 180).WithMaxPurgeBatches(2)
	// 180 days before 10-07 is 2026-04-10: all of March goes. Two batches of 1000 per Run.
	var purged []int
	for i := 0; i < 5; i++ {
		res, err := rec.Run(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if res.Day != "" {
			t.Fatalf("run %d recorded %s, nothing is pending", i+1, res.Day)
		}
		purged = append(purged, res.Purged)
		if !res.More {
			break
		}
	}
	if fmt.Sprint(purged) != "[2000 500]" {
		t.Fatalf("purged per run = %v, want [2000 500]", purged)
	}
	if got := count(t, w, "jev_evaluation_daily"); got != recent {
		t.Errorf("rows after the purge = %d, want the %d recent ones", got, recent)
	}
	for _, tb := range tables {
		if got := count(t, w, tb); got != others[tb] {
			t.Errorf("%s changed %d -> %d: the purge may only touch jev_evaluation_daily", tb, others[tb], got)
		}
	}
	// The boundary: a row of exactly cutoff day stays (cutoff 2026-04-10).
	r := repo.NewRepository(w)
	testkit.MustExec(t, w, `INSERT INTO jev_evaluation_daily (day, model_id, question_version, horizon_minutes, take_profit_bps, stop_loss_bps, barrier_cost_bps,
	decisions, evaluated, entered, wins, llm_sum_bps, random_sum_bps, momentum_sum_bps, contrarian_sum_bps)
	VALUES ('2026-04-10', 'edge', 'v1', 10, 30, 30, 10, 0, 0, 0, 0, 0, 0, 0, 0), ('2026-04-09', 'edge', 'v1', 10, 30, 30, 10, 0, 0, 0, 0, 0, 0, 0, 0)`)
	if n, err := r.PurgeBefore(t.Context(), "2026-04-10", evalrecord.PurgeBatchRows); err != nil || n != 1 {
		t.Fatalf("PurgeBefore(04-10) = %d, %v; want only the 04-09 row", n, err)
	}
}

// A retention shorter than the record's age deletes nothing while nothing is old.
func TestRetention_NothingOldDeletesNothing(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	seed(t, w)
	for range 3 {
		if res, err := newRecorder(t, w, "2026-10-07 16:30", 3).Run(t.Context()); err != nil || res.Purged != 0 {
			t.Fatalf("Run = %+v, %v", res, err)
		}
	}
	if n := count(t, w, "jev_evaluation_daily"); n != 4 {
		t.Errorf("rows = %d, want 4 (retention 3 days keeps 10-04 and later)", n)
	}
}
