package slicetests

import (
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// bulkDecisions writes n LONG trader decisions of one instant in one statement (a recursive CTE; one INSERT per row takes
// seconds under -race). The model of decision i is model(i), one INSERT per distinct model, each over a contiguous id run.
func bulkDecisions(t *testing.T, w *sql.DB, n int, model func(i int) string) {
	t.Helper()
	ts := testkit.Stored(t, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC).Format(time.RFC3339))
	names := map[string][]int{}
	for i := range n {
		names[model(i)] = append(names[model(i)], i+1)
	}
	if len(names) > evaldiag.MaxCohorts+1 {
		t.Fatalf("%d distinct models", len(names))
	}
	for name, ids := range names {
		lo, hi := ids[0], ids[len(ids)-1] // every model's ids are one run or one id
		if len(ids) != hi-lo+1 {
			t.Fatalf("model %s has non-contiguous ids", name)
		}
		testkit.MustExec(t, w, `WITH RECURSIVE seq(i) AS (SELECT ? UNION ALL SELECT i + 1 FROM seq WHERE i < ?)
			INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
				question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
			SELECT i, 1, 'S', ?, 'trader', 'h', '{}', 'trader-v3', '{}', 'LONG', 0.7, 5, ?, ? FROM seq`, lo, hi, ts, name, ts)
	}
}

// The period limits are jev_evaluation's: more decisions than LimitFor(horizon) or more cohorts than MaxCohorts is
// limit_exceeded, not a partial result. The 10,000-decision MaxDecisions case and the "exactly at the limit is answered" side
// are pinned by evaltests.TestLimits (seeding and evaluating 10,000 decisions takes seconds under -race); the slices pass the
// error through unchanged, which the horizon-60 limit (3,125 decisions, the smallest LimitFor allows) and the cohort limit show.
func TestLimitsAreJevEvaluationsLimits(t *testing.T) {
	clone := `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
		SELECT ?, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json,
		direction, confidence, latency_ms, ?, created_at FROM jev_decisions WHERE id = 1`
	t.Run("long horizon shrinks the decision limit", func(t *testing.T) {
		srv, w := newServer(t)
		testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'a', 'TSE')`)
		limit := evaldiag.LimitFor(60)
		if limit >= evaldiag.MaxDecisions {
			t.Fatalf("LimitFor(60) = %d", limit)
		}
		bulkDecisions(t, w, limit+1, func(int) string { return "m" })
		srv.call(t, map[string]any{"horizon_minutes": 60}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	})
	t.Run("cohorts", func(t *testing.T) {
		srv, w := newServer(t)
		testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'a', 'TSE')`)
		bulkDecisions(t, w, evaldiag.MaxCohorts, func(i int) string { return fmt.Sprintf("model-%d", i) })
		if d := srv.call(t, nil).data(t); len(d.Cohorts) != evaldiag.MaxCohorts {
			t.Fatalf("cohorts = %d", len(d.Cohorts))
		}
		testkit.MustExec(t, w, clone, 1000, "model-extra")
		srv.call(t, nil).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	})
}
