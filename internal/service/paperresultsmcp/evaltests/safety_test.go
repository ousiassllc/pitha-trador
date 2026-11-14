package evaltests

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// dump hashes every row of every table, so any write by the tool (a label, a
// setting, a signal, a research row) changes the result.
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

// The tool is registered read-only with exactly the declared arguments, no
// setting it could write is on the MCP's write whitelist, and a call (including
// one that ends in an error) leaves every table of the database unchanged.
func TestToolIsReadOnlyAndWritesNothing(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
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
	srv.call(t, map[string]any{"horizon_minutes": 5, "prompt_version": v4}).data(t)
	srv.call(t, map[string]any{"unknown": 1}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	after := dump(t, w)
	if len(before) < 20 {
		t.Fatalf("dump covers %d tables, the check is vacuous", len(before))
	}
	for table, h := range before {
		if after[table] != h {
			t.Errorf("table %s changed during jev_evaluation", table)
		}
	}
}

// Bad arguments are invalid_argument (never partial output); a bad period is
// invalid_period like every period tool.
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
	srv.post(t, "tools/call", map[string]any{"name": tool, "arguments": map[string]any{"from": "2026-10-10", "to": "2026-10-01"}}).
		wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidPeriod)
	// The edge values are accepted.
	srv.call(t, map[string]any{"horizon_minutes": 1, "model_id": strings.Repeat("m", evaldiag.MaxModelIDLen)}).data(t)
	srv.call(t, map[string]any{"horizon_minutes": 60}).data(t)
}

// An empty period is an answer (no cohorts), not an error.
func TestEmptyPeriod(t *testing.T) {
	srv, _ := newServer(t)
	d := srv.call(t, nil).data(t)
	if d.Decisions != 0 || len(d.Cohorts) != 0 || d.Cohorts == nil || d.HorizonMinutes != 10 {
		t.Errorf("empty = %+v", d)
	}
}

func bulkDecisions(t *testing.T, w *sql.DB, n int, model func(i int) string) {
	t.Helper()
	tx, err := w.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := testkit.Stored(t, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC).Format(time.RFC3339))
	for i := range n {
		mustTx(t, tx, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
			question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
			VALUES (?, 1, 'S', ?, 'trader', 'h', '{}', 'trader-v3', '{}', 'LONG', 0.7, 5, ?, ?)`, i+1, ts, model(i), ts)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// A period holding more decisions than MaxDecisions, or more cohorts than
// MaxCohorts, is limit_exceeded rather than a partial result.
func TestLimits(t *testing.T) {
	t.Run("decisions", func(t *testing.T) {
		srv, w := newServer(t)
		testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'a', 'TSE')`)
		bulkDecisions(t, w, evaldiag.MaxDecisions, func(int) string { return "m" })
		srv.call(t, nil).data(t) // exactly at the limit is answered
		testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
			question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
			SELECT ?, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json,
			direction, confidence, latency_ms, model_id, created_at FROM jev_decisions WHERE id = 1`, evaldiag.MaxDecisions+1)
		srv.call(t, nil).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	})
	t.Run("cohorts", func(t *testing.T) {
		srv, w := newServer(t)
		testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'a', 'TSE')`)
		bulkDecisions(t, w, evaldiag.MaxCohorts, func(i int) string { return fmt.Sprintf("model-%d", i) })
		if d := srv.call(t, nil).data(t); len(d.Cohorts) != evaldiag.MaxCohorts {
			t.Fatalf("cohorts = %d", len(d.Cohorts))
		}
		testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
			question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
			SELECT 1000, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json,
			direction, confidence, latency_ms, 'model-extra', created_at FROM jev_decisions WHERE id = 1`)
		srv.call(t, nil).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	})
}
