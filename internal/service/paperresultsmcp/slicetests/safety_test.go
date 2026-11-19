package slicetests

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/slicediag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// dump hashes every row of every table, so any write by the tool changes the result.
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

// The tool is registered read-only with exactly the declared arguments, and a call (including
// one that ends in an error) leaves every table unchanged and issues only SELECTs that never name a raw body column.
func TestToolIsReadOnlyAndWritesNothing(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	driver, counter := testkit.NewCountingDriver(t)
	srv := startServer(t, path, driver)
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
		if strings.Join(props, ",") != "dimensions,from,horizon_minutes,model_id,prompt_version,to" {
			t.Errorf("arguments = %v", props)
		}
	}
	if !found {
		t.Fatalf("%s is not in tools/list", tool)
	}

	before := dump(t, w)
	mark := len(counter.StatementLog())
	srv.call(t, nil).data(t)
	srv.call(t, map[string]any{"horizon_minutes": 5, "prompt_version": v4, "dimensions": []string{"regime"}}).data(t)
	srv.call(t, map[string]any{"dimensions": []string{"nope"}}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	if after := dump(t, w); len(before) < 20 {
		t.Fatalf("dump covers %d tables, the check is vacuous", len(before))
	} else {
		for table, h := range before {
			if after[table] != h {
				t.Errorf("table %s changed during %s", table, tool)
			}
		}
	}
	stmts := counter.StatementLog()[mark:]
	if len(stmts) == 0 {
		t.Fatal("no statement was recorded: the check is vacuous")
	}
	write := regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|REPLACE|CREATE|DROP|ALTER|ATTACH|VACUUM)\b`)
	for _, stmt := range stmts {
		head := strings.ToUpper(strings.TrimSpace(stmt))
		if !strings.HasPrefix(head, "SELECT") && !strings.HasPrefix(head, "WITH") && head != "BEGIN" && head != "PRAGMA QUERY_ONLY" { // the last two are the read-only connection's own setup
			t.Errorf("a non-read statement was issued: %s", testkit.FirstLine(stmt))
		}
		if write.MatchString(stmt) || strings.Contains(stmt, "response_json") || strings.Contains(stmt, "state_json") || strings.Contains(stmt, "raw_data_json") || strings.Contains(stmt, "SELECT *") {
			t.Errorf("statement writes or reads a body column: %s", testkit.FirstLine(stmt))
		}
	}
}

// Nothing stored in a body column, no runtime setting beyond the label's own keys and no credential appears in the response, which
// is also the whole of what an MCP client sees.
func TestResponseLeaksNothing(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w) // every decision has SENTINEL_STATE_JSON / SENTINEL_RESPONSE_JSON, every bar SENTINEL_RAW_DATA_JSON
	testkit.Setting(t, w, "paper.api_secret_key", testkit.SentinelRuntime)
	testkit.Setting(t, w, "kabu.password", testkit.SentinelSecret)
	for _, args := range []map[string]any{nil, {"dimensions": []string{"sector", "regime"}}} {
		body := string(srv.call(t, args).body)
		d := srv.call(t, args).data(t)
		if len(d.Cohorts) == 0 {
			t.Fatal("no cohort: the check is vacuous")
		}
		for _, s := range append(testkit.Sentinels(), testAPIKey, "response_json", "state_json", "raw_data_json", "error_excerpt") {
			if strings.Contains(body, s) {
				t.Errorf("the response contains %q", s)
			}
		}
	}
}

// Bad arguments are invalid_argument, never partial output: an unknown dimension (also a family name or a different case), a repeated one, an empty list, a
// wrong type, and everything jev_evaluation refuses; a bad period is invalid_period.
func TestRejectsBadArguments(t *testing.T) {
	srv, _ := newServer(t)
	for _, a := range []map[string]any{
		{"dimensions": []string{"nope"}}, {"dimensions": []string{"regime", "Regime"}}, {"dimensions": []string{"preregistered"}},
		{"dimensions": []string{"regime", "regime"}}, {"dimensions": []string{}}, {"dimensions": "regime"}, {"dimensions": []int{1}},
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
	// Every known dimension and the edge values are accepted.
	srv.call(t, map[string]any{"dimensions": slicediag.Names(), "horizon_minutes": 1, "model_id": strings.Repeat("m", evaldiag.MaxModelIDLen)}).data(t)
}

// An empty period is an answer (no cohorts), not an error; the response still carries the fixed texts.
func TestEmptyPeriod(t *testing.T) {
	srv, _ := newServer(t)
	d := srv.call(t, nil).data(t)
	if d.Decisions != 0 || len(d.Cohorts) != 0 || d.Cohorts == nil || d.HorizonMinutes != 10 || len(d.Dimensions) != 9 {
		t.Errorf("empty = %+v", d)
	}
	if d.Preregistration.Date != "2026-11-15" || d.Preregistration.Issue != "#1089" || strings.Join(d.Preregistration.Dimensions, ",") != "regime,time_of_day,direction,confidence_band" {
		t.Errorf("preregistration = %+v", d.Preregistration)
	}
	if d.Notice == "" || d.ExploratoryNote == "" || len(d.Caveats) == 0 || len(d.Definitions) == 0 {
		t.Errorf("texts are missing: %+v", d)
	}
}
