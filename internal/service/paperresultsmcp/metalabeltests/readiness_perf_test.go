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

// A 30-day call over a realistic volume (about 100 decisions per trading day,
// 4 KB of state each) is answered inside the MCP's read budget
// (paperresultsmcp.QueryTimeout, enforced by the server: a slower read would be
// a 504 query_timeout), and the full 90-day lookback is measured with three
// times the rows. The timings are logged (go test -v). Under the race detector
// (about ten times slower, see raceEnabled) the server is given a long timeout
// and the times are only logged: the budget claim is for the normal build.
func TestReadinessFitsTheReadBudget(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	// About 100 decisions on each of the 22 trading days of a 30-day month.
	const perDay, days = 100, 90
	n := perDay * days * 22 / 30 // the 90-day volume
	bulkSeed(t, w, n, now.Add(-30*time.Minute), int(90*24*time.Hour/time.Second)/n, 4096)
	opts := paperresultsmcp.Options{DBPath: path}
	budget := paperresultsmcp.QueryTimeout
	if raceEnabled {
		opts.QueryTimeout, budget = time.Minute, time.Minute
	}
	srv := newTestServerWith(t, opts, now)

	for _, c := range []struct {
		name string
		args map[string]any
		rows int
		// strict: the call must finish within the server's own read budget (a
		// 504 query_timeout is the failure). Only the 30-day call carries it.
		strict bool
	}{
		{"30 days (default)", map[string]any{}, n / 3, true},
		{"90 days (the retraining's lookback)", fullLookback, n, false},
	} {
		start := time.Now()
		r := srv.call(t, "metalabel_training_readiness", c.args)
		elapsed := time.Since(start)
		if r.status != http.StatusOK && (c.strict || raceEnabled) {
			t.Fatalf("%s: status %d (%s) after %v; the read budget is %v", c.name, r.status, r.body, elapsed, budget)
		}
		if r.status != http.StatusOK {
			t.Logf("%s: status %d after %v (over the %v read budget; narrow the period to 30 days)", c.name, r.status, elapsed, budget)
			continue
		}
		var d readiness
		r.data(t, "metalabel_training_readiness", &d)
		if tol := c.rows / 20; d.Features.TrainingRows < c.rows-tol || d.Features.TrainingRows > c.rows+tol {
			t.Errorf("%s: read %d rows, want about %d (within 5%%)", c.name, d.Features.TrainingRows, c.rows)
		}
		t.Logf("%s: %d training rows in %v (budget %v)", c.name, d.Features.TrainingRows, elapsed, budget)
	}
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
	if explained != 2 {
		t.Fatalf("explained %d statements, want the funnel and the training-row read", explained)
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
