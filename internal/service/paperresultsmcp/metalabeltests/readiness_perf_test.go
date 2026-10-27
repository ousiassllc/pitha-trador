package metalabeltests

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/metalabeldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// bulkSeed inserts n complete, labeled trader decisions ending at end, step
// seconds apart, with a state_json of about stateBytes (the stored Jev input
// carries the board and the news context). One INSERT ... SELECT per table, so
// tens of thousands of rows take well under a second.
func bulkSeed(t *testing.T, w *sql.DB, n int, end time.Time, stepSec, stateBytes int) {
	t.Helper()
	state := `{"spread_bps":3.2,"return_1m":0.0004,"pad":"` + strings.Repeat("x", stateBytes) + `"}`
	testkit.MustExec(t, w, `WITH RECURSIVE s(i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM s WHERE i + 1 < ?)
INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version,
	response_json, direction, confidence, latency_ms, model_id, ofi_short, ofi_mid, ofi_long, microprice_dev_bps)
SELECT i + 1, 1, '7203', strftime('%Y-%m-%dT%H:%M:%S.000000000Z', ? - (? - 1 - i) * ?, 'unixepoch'), 'trader', 'h', ?, 'q1', ?,
	CASE WHEN i % 2 = 0 THEN 'LONG' ELSE 'SHORT' END, 0.7, 5, 'jev-1.13.0', 0.5, 0.5, 0.5, 0.5
FROM s`, n, end.Unix(), n, stepSec, state, validResponse)
	testkit.MustExec(t, w, `WITH RECURSIVE s(i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM s WHERE i + 1 < ?)
INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return, max_adverse_excursion,
	max_favorable_excursion, was_direction_correct, barrier_hit, net_return_bps, entry_ref_price, label_version)
SELECT i + 1, 15, 0, 0, 0, 1, 'tp', CASE WHEN i % 3 = 0 THEN -20.0 ELSE 30.0 END, 100, 2 FROM s`, n)
}

// A 30-day and a 90-day call over a realistic volume (about 100 decisions per
// trading day, 4 KB of state each) are answered inside the read budget of the
// build (readBudgets, wall clock). Without -race that is the MCP's real
// paperresultsmcp.QueryTimeout (3 s). Under -race (about ten times slower; CI
// runs only -race) it is a finite, larger budget that still catches an
// order-of-magnitude regression (race_test.go gives the measurements); the
// server's own timeout is then raised so a slow runner shows up as this
// test's budget message and not as a 504. The 30-day call is the acceptance
// criterion of #848; the 90-day call (the retraining's own window) is a
// reference. The times are logged (go test -v).
func TestReadinessFitsTheReadBudget(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	// About 100 decisions on each of the 22 trading days of a 30-day month.
	const perDay, days = 100, 90
	n := perDay * days * 22 / 30 // the 90-day volume
	bulkSeed(t, w, n, now.Add(-30*time.Minute), int(90*24*time.Hour/time.Second)/n, 4096)
	srv := newTestServerWith(t, paperresultsmcp.Options{DBPath: path, QueryTimeout: serverTimeout}, now)

	for _, c := range []struct {
		name   string
		args   map[string]any
		rows   int
		budget time.Duration
	}{
		{"30 days (default)", map[string]any{}, n / 3, readBudgets.days30},
		{"90 days (the retraining's lookback)", fullLookback, n, readBudgets.days90},
	} {
		start := time.Now()
		r := srv.call(t, "metalabel_training_readiness", c.args)
		elapsed := time.Since(start)
		if r.status != http.StatusOK {
			t.Fatalf("%s: status %d (%s) after %v; the read budget is %v", c.name, r.status, r.body, elapsed, c.budget)
		}
		var d readiness
		r.data(t, "metalabel_training_readiness", &d)
		if tol := c.rows / 20; d.Features.TrainingRows < c.rows-tol || d.Features.TrainingRows > c.rows+tol {
			t.Errorf("%s: read %d rows, want about %d (within 5%%)", c.name, d.Features.TrainingRows, c.rows)
		}
		if elapsed >= c.budget {
			t.Errorf("%s: took %v, not under the %v read budget", c.name, elapsed, c.budget)
		}
		t.Logf("%s: %d training rows in %v (budget %v)", c.name, d.Features.TrainingRows, elapsed, c.budget)
	}
}

// A 366-day jev_models call (the longest period) over a realistic volume (about
// 100 decisions per trading day for a year) answers correctly, finishes inside
// the read budget of the build (readBudgets) and reads jev_decisions through
// its timestamp index (EXPLAIN QUERY PLAN).
func TestJevModelsOver366Days(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	const n = 100 * 366 * 22 / 30
	bulkSeed(t, w, n, now.Add(-30*time.Minute), int(366*24*time.Hour/time.Second)/n, 16)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newTestServerWith(t, paperresultsmcp.Options{DBPath: path, DriverName: driverName, QueryTimeout: serverTimeout}, now)

	var d jevModels
	start := time.Now()
	r := srv.call(t, "jev_models", map[string]any{"from": "2025-10-18", "to": "2026-10-19"})
	elapsed := time.Since(start)
	r.data(t, "jev_models", &d)
	if elapsed >= readBudgets.days366 {
		t.Errorf("366 days took %v, not under the %v read budget", elapsed, readBudgets.days366)
	}
	t.Logf("jev_models over 366 days: %d decisions, %d daily rows in %v (budget %v)", d.Models[0].Decisions, len(d.Daily), elapsed, readBudgets.days366)
	if len(d.Models) != 1 || d.Models[0].Decisions < n*9/10 || len(d.Daily) < 300 {
		t.Errorf("models = %+v, daily rows = %d; want the one seeded model over about %d decisions", d.Models, len(d.Daily), n)
	}
	var total int64
	for _, day := range d.Daily {
		total += day.Decisions
	}
	if total != d.Models[0].Decisions {
		t.Errorf("daily rows add up to %d, the model row says %d", total, d.Models[0].Decisions)
	}

	explained := 0
	for _, stmt := range counter.StatementLog() {
		if !strings.Contains(stmt, "FROM jev_decisions") {
			continue
		}
		for _, p := range explainPlan(t, w, stmt) {
			if strings.Contains(p, "jev_decisions") && !strings.Contains(p, "USING INDEX") && !strings.Contains(p, "USING COVERING INDEX") {
				t.Errorf("jev_decisions read without an index in %q: %s", testkit.FirstLine(stmt), p)
			}
			if strings.Contains(stmt, "GROUP BY") && strings.Contains(p, "jev_decisions") && !strings.Contains(p, "jev_decisions_timestamp_idx (timestamp>? AND timestamp<?)") {
				t.Errorf("the period read is not a timestamp range search: %s", p)
			}
		}
		explained++
	}
	if explained != 2 {
		t.Fatalf("explained %d jev_decisions statements, want the grouped read and the latest-decision read", explained)
	}
}

// explainPlan is the EXPLAIN QUERY PLAN detail lines of stmt (every ? bound to a
// timestamp text; the plan does not depend on the values).
func explainPlan(t *testing.T, w *sql.DB, stmt string) []string {
	t.Helper()
	args := make([]any, strings.Count(stmt, "?"))
	for i := range args {
		args[i] = "2026-07-01T00:00:00.000000000Z"
	}
	rows, err := w.Query("EXPLAIN QUERY PLAN "+stmt, args...)
	if err != nil {
		t.Fatalf("EXPLAIN %s: %v", testkit.FirstLine(stmt), err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("EXPLAIN QUERY PLAN of %q:\n  %s", testkit.FirstLine(stmt), strings.Join(plan, "\n  "))
	return plan
}

// The statements of the tool use the existing indexes: no full scan of
// jev_decisions or calibration_outcomes (EXPLAIN QUERY PLAN on the statements
// the tool really ran, with the same number of arguments; the plan does not
// depend on their values).
func TestReadinessStatementsUseIndexes(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	bulkSeed(t, w, 500, now.Add(-30*time.Minute), 600, 64)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newTestServerWith(t, paperresultsmcp.Options{DBPath: path, DriverName: driverName}, now)
	srv.call(t, "metalabel_training_readiness", fullLookback)

	explained := 0
	for _, stmt := range counter.StatementLog() {
		if !strings.Contains(stmt, "jev_decisions") || !strings.Contains(stmt, "calibration_outcomes") {
			continue
		}
		args := make([]any, strings.Count(stmt, "?"))
		for i := range args {
			args[i] = "2026-07-01T00:00:00.000000000Z"
		}
		rows, err := w.Query("EXPLAIN QUERY PLAN "+stmt, args...)
		if err != nil {
			t.Fatalf("EXPLAIN %s: %v", testkit.FirstLine(stmt), err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		t.Logf("EXPLAIN QUERY PLAN of %q:\n  %s", testkit.FirstLine(stmt), strings.Join(plan, "\n  "))
		for _, p := range plan {
			if strings.HasPrefix(p, "SCAN") {
				t.Errorf("full scan in the plan of %q: %s", testkit.FirstLine(stmt), p)
			}
		}
		explained++
	}
	if explained != 3 {
		t.Fatalf("explained %d statements, want the funnel, the excluded prompt versions and the training-row read", explained)
	}
}

// More training rows than the tool rebuilds in one call (metalabeldiag.MaxReadinessRows)
// is limit_exceeded, never a partial answer; narrowing the period answers.
func TestReadinessRowLimit(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	bulkSeed(t, w, metalabeldiag.MaxReadinessRows+1, now.Add(-time.Hour), 60, 16)
	// Not a timing test: the long timeout keeps a slow (race) run from a 504.
	srv := newTestServerWith(t, paperresultsmcp.Options{DBPath: path, QueryTimeout: time.Minute}, now)

	if r := srv.call(t, "metalabel_training_readiness", fullLookback); r.status != http.StatusUnprocessableEntity || !strings.Contains(string(r.body), "limit_exceeded") {
		t.Fatalf("status %d, body %s; want 422 limit_exceeded", r.status, r.body)
	}
	var d readiness
	srv.call(t, "metalabel_training_readiness", map[string]any{"from": "2026-10-17", "to": "2026-10-19"}).data(t, "metalabel_training_readiness", &d)
	if d.Features.TrainingRows == 0 || d.Features.TrainingRows > metalabeldiag.MaxReadinessRows {
		t.Errorf("narrowed period read %d rows", d.Features.TrainingRows)
	}
}
