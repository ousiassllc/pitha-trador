package trendtests

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// dump hashes every row of every table, so any write by the tool (a record, a setting, a
// label) changes the result.
func dump(t *testing.T, w *sql.DB) map[string]string {
	t.Helper()
	rows, err := w.QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	out := map[string]string{}
	for _, n := range names {
		r, err := w.QueryContext(t.Context(), `SELECT * FROM "`+n+`"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := r.Columns()
		h := sha256.New()
		for r.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := r.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			_, _ = fmt.Fprintf(h, "%v\n", vals)
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		_ = r.Close()
		out[n] = fmt.Sprintf("%x", h.Sum(nil))
	}
	return out
}

// The tool is registered read-only with exactly the declared arguments, and a call (also one
// that ends in an error) leaves every table of the database unchanged.
func TestToolIsReadOnlyAndWritesNothing(t *testing.T) {
	srv, w := newServer(t)
	seedWeek(t, w)
	recordPending(t, recorder(w, 9))
	var env struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				Annotations map[string]any `json:"annotations"`
				InputSchema struct {
					Properties           map[string]any `json:"properties"`
					AdditionalProperties bool           `json:"additionalProperties"`
				} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	r := srv.post(t, "tools/list", nil)
	if err := json.Unmarshal(r.body, &env); err != nil || r.status != http.StatusOK {
		t.Fatalf("tools/list: %d %s %v", r.status, r.body, err)
	}
	found := false
	for _, tl := range env.Result.Tools {
		if tl.Name != tool {
			continue
		}
		found = true
		if tl.Annotations["readOnlyHint"] != true || tl.Annotations["destructiveHint"] != false || tl.InputSchema.AdditionalProperties {
			t.Errorf("annotations %v, schema %+v", tl.Annotations, tl.InputSchema)
		}
		var props []string
		for p := range tl.InputSchema.Properties {
			props = append(props, p)
		}
		slices.Sort(props)
		if strings.Join(props, ",") != "from,horizon_minutes,model_id,prompt_version,to" {
			t.Errorf("arguments = %v", props)
		}
	}
	if !found {
		t.Fatalf("%s is not in tools/list", tool)
	}
	before := dump(t, w)
	srv.call(t, nil).data(t)
	srv.call(t, map[string]any{"horizon_minutes": 5, "prompt_version": "trader-v4"}).data(t)
	srv.call(t, map[string]any{"unknown": 1}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	after := dump(t, w)
	if len(before) < 20 {
		t.Fatalf("dump covers %d tables, the check is vacuous", len(before))
	}
	for table, h := range before {
		if after[table] != h {
			t.Errorf("table %s changed during %s", table, tool)
		}
	}
}

// The tool answers from the record: with decisions and bars present but no recorded day (the
// switch was off) it returns no series, and it does not label anything itself.
func TestToolReadsOnlyTheRecord(t *testing.T) {
	srv, w := newServer(t)
	seedWeek(t, w)
	d := srv.call(t, nil).data(t)
	if len(d.Series) != 0 || d.Recording.DaysRecorded != 0 {
		t.Fatalf("series %d, recorded %d without any record", len(d.Series), d.Recording.DaysRecorded)
	}
}

// Nothing of the columns and settings a tool must never expose reaches the output, however
// many runtime settings and raw bodies the database holds: only the two research settings are read.
func TestNoSecretsInTheOutput(t *testing.T) {
	srv, w := newServer(t)
	seedWeek(t, w)
	testkit.MustExec(t, w, `UPDATE jev_decisions SET state_json = ?, response_json = ?`, `{"x":"`+testkit.SentinelState+`"}`, `{"x":"`+testkit.SentinelResponse+`"}`)
	testkit.MustExec(t, w, `UPDATE market_snapshots SET raw_data_json = ?`, `{"x":"`+testkit.SentinelRaw+`"}`)
	testkit.Setting(t, w, testkit.SentinelSecretKey, `"`+testkit.SentinelSecret+`"`)
	testkit.Setting(t, w, "kabu.api_password", `"`+testkit.SentinelRuntime+`"`)
	testkit.Setting(t, w, "research.eval.enabled", "1")
	recordPending(t, recorder(w, 9))
	r := srv.call(t, nil)
	d := r.data(t)
	if len(d.Series) == 0 || !d.Recording.Enabled {
		t.Fatalf("the check is vacuous: %+v", d.Recording)
	}
	body := string(r.body)
	for _, s := range append(testkit.Sentinels(), testAPIKey) {
		if strings.Contains(body, s) {
			t.Errorf("the output contains %q", s)
		}
	}
	for _, key := range []string{"response_json", "state_json", "raw_data_json", "error_excerpt"} {
		if strings.Contains(body, key) {
			t.Errorf("the output names %q", key)
		}
	}
}

// Bad arguments are invalid_argument (never partial output); a bad period is invalid_period like
// every period tool.
func TestRejectsBadArguments(t *testing.T) {
	srv, _ := newServer(t)
	for _, a := range []map[string]any{
		{"unknown": 1}, {"horizon_minutes": 0}, {"horizon_minutes": 61}, {"horizon_minutes": "10"}, {"horizon_minutes": 2.5},
		{"prompt_version": "Trader V3"}, {"prompt_version": strings.Repeat("a", 65)}, {"prompt_version": 3},
		{"model_id": strings.Repeat("m", evaldiag.MaxModelIDLen+1)}, {"model_id": 1},
	} {
		srv.call(t, a).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	}
	srv.post(t, "tools/call", map[string]any{"name": tool, "arguments": map[string]any{"from": "2026-10-01"}}).
		wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidPeriod)
	srv.call(t, map[string]any{"horizon_minutes": 1, "model_id": strings.Repeat("m", evaldiag.MaxModelIDLen)}).data(t)
}

// insertCohorts fills jev_evaluation_daily with `cohorts` cohorts of one row per day over `days` days
// from 2026-01-01 (made-up numbers; the recorder cannot make more than 50 cohorts, the table can hold more).
const insertCohorts = `WITH RECURSIVE d(i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM d WHERE i < %d),
c(j) AS (SELECT 1 UNION ALL SELECT j + 1 FROM c WHERE j < %d)
INSERT INTO jev_evaluation_daily (day, model_id, question_version, horizon_minutes, take_profit_bps, stop_loss_bps, barrier_cost_bps,
	decisions, evaluated, entered, wins, llm_sum_bps, random_sum_bps, momentum_sum_bps, contrarian_sum_bps)
SELECT date('2026-01-01', '+' || i || ' days'), 'm' || j, 'v1', 10, 30, 30, 10, 2, 2, 1, 1, 5, 1, 1, 1 FROM d, c`

// A long period of many series is served whole (the bootstrap of every series is the cost, so
// the size here is moderate: the full 366 x 50 took 0.56 s without -race), and one row more than
// the limit (366 days x 50 cohorts) is limit_exceeded, never a partial series.
func TestRowLimit(t *testing.T) {
	srv, w := newServer(t)
	testkit.MustExec(t, w, fmt.Sprintf(insertCohorts, 281, 5)) // 5 cohorts x 282 days
	d := srv.call(t, map[string]any{"from": "2026-01-01", "to": "2026-10-10"}).data(t)
	if len(d.Series) != 5 || len(d.Series[0].Daily) != 282 || d.Series[0].Evaluated != 564 {
		t.Fatalf("series %d, days %d, evaluated %d; want 5 x 282", len(d.Series), len(d.Series[0].Daily), d.Series[0].Evaluated)
	}
	testkit.MustExec(t, w, `DELETE FROM jev_evaluation_daily`)
	// 366 days from 2025-10-10 x 51 cohorts = 18,666 rows > 18,300 (period 2025-10-10..2026-10-10 is 365 days; use 2025-10-09).
	testkit.MustExec(t, w, strings.Replace(fmt.Sprintf(insertCohorts, 365, 51), "'2026-01-01'", "'2025-10-09'", 1))
	srv.call(t, map[string]any{"from": "2025-10-09", "to": "2026-10-10"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
}
