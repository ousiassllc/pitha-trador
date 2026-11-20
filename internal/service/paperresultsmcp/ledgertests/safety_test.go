package ledgertests

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
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/ledgerdiag"
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
			vals, ptrs := make([]any, len(cols)), make([]any, len(cols))
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

// The tool is registered read-only with exactly the declared arguments.
func TestToolIsRegisteredReadOnly(t *testing.T) {
	t.Parallel()
	srv, _ := newServer(t, "")
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
		if got := strings.Join(props, ","); got != "alignment,candidates,from,horizon,model_id,notional_jpy,to,usd_jpy" {
			t.Errorf("arguments = %s", got)
		}
	}
	if !found {
		t.Fatalf("%s is not in tools/list", tool)
	}
}

// A call (including one that ends in an error) writes nothing: every table is unchanged, the
// statements issued are SELECTs only, none reads a body column of the raw tables, and a write on the
// connection fails.
func TestCallWritesNothingAndReadsNoRawBodies(t *testing.T) {
	t.Parallel()
	driverName, counter := testkit.NewCountingDriver(t)
	srv, w := newServer(t, driverName)
	seedCandidates(t, w, seedBase(t, w))
	before := dump(t, w)
	if len(before) < 20 {
		t.Fatalf("dump covers %d tables, the check is vacuous", len(before))
	}
	start := len(counter.StatementLog())
	for _, args := range []map[string]any{nil, {"horizon": "30m"}, {"alignment": "pairwise", "usd_jpy": 150.0, "notional_jpy": 1e6}} {
		srv.call(t, args).raw(t)
	}
	srv.call(t, map[string]any{"unknown": 1}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	stmts := counter.StatementLog()[start:]
	if len(stmts) == 0 {
		t.Fatal("no statement was logged: the check is vacuous")
	}
	for _, stmt := range stmts {
		s := strings.ToUpper(strings.TrimSpace(stmt))
		if !strings.HasPrefix(s, "SELECT") && !strings.HasPrefix(s, "PRAGMA") && !strings.HasPrefix(s, "BEGIN") && !strings.HasPrefix(s, "ROLLBACK") {
			t.Errorf("a non-read statement ran: %s", testkit.FirstLine(stmt))
		}
		for _, body := range []string{"response_json", "state_json", "raw_data_json", "error_excerpt"} {
			if strings.Contains(stmt, body) {
				t.Errorf("a statement reads %s: %s", body, testkit.FirstLine(stmt))
			}
		}
	}
	db, err := srv.DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('x', '1', 't')`); err == nil {
		t.Error("a write succeeded on the MCP connection")
	}
	after := dump(t, w)
	for table, h := range before {
		if after[table] != h {
			t.Errorf("table %s changed during candidate_ledger", table)
		}
	}
}

// The ledger decides nothing: no key of the whole response names a winner, a ranking, a vote, a
// majority or a recommendation, and the candidates come in a fixed kind/id order, never by value.
func TestNoWinnerRankVoteOrRecommendationAnywhere(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	seedCandidates(t, w, seedBase(t, w))
	data := srv.call(t, map[string]any{"usd_jpy": 150.0, "notional_jpy": 1e6}).raw(t)
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				for _, bad := range []string{"winner", "best", "rank", "vote", "majority", "recommend", "verdict", "adopt", "score"} {
					if strings.Contains(strings.ToLower(k), bad) {
						t.Errorf("key %q at %s names a decision (%s)", k, path, bad)
					}
				}
				walk(path+"."+k, child)
			}
		case []any:
			for i, child := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), child)
			}
		}
	}
	walk("data", data)
	var kinds []int
	var ids []string
	for _, c := range data["candidates"].([]any) {
		m := c.(map[string]any)
		ids = append(ids, m["id"].(string))
		kinds = append(kinds, slices.Index([]string{"live_cohort", "shadow", "trial", "baseline", "research_model"}, m["kind"].(string)))
	}
	if !slices.IsSorted(kinds) {
		t.Errorf("candidates are not in kind order: %v", ids)
	}
}

// Raw bodies, error text and the API key never reach the response.
func TestNoSecretsOrRawBodiesInTheResponse(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	seedCandidates(t, w, seedBase(t, w))
	testkit.Setting(t, w, "llm.api_key", `"SENTINEL_SECRET_SETTING_VALUE"`)
	for _, args := range []map[string]any{nil, {"alignment": "pairwise"}, {"horizon": "30m"}} {
		body := string(srv.call(t, args).body)
		for _, leak := range []string{sentinelResponse, sentinelState, sentinelError, "SENTINEL_SECRET_SETTING_VALUE", testAPIKey, "response_json", "state_json", "error_excerpt", "raw_data_json"} {
			if strings.Contains(body, leak) {
				t.Errorf("%v: the response contains %q", args, leak)
			}
		}
	}
}

func TestRejectsBadArguments(t *testing.T) {
	t.Parallel()
	srv, _ := newServer(t, "")
	tooMany := make([]string, ledgerdiag.MaxCandidates+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("shadow:v%d", i)
	}
	for name, args := range map[string]map[string]any{
		"unknown field":      {"x": 1},
		"horizon":            {"horizon": "5m"},
		"alignment":          {"alignment": "union"},
		"candidate kind":     {"candidates": []string{"experiment:1"}},
		"baseline name":      {"candidates": []string{"baseline:oracle"}},
		"research model id":  {"candidates": []string{"research_model:ridge"}},
		"live as candidate":  {"candidates": []string{"live"}},
		"duplicate":          {"candidates": []string{idShadow, idShadow}},
		"too many listed":    {"candidates": tooMany},
		"usd_jpy zero":       {"usd_jpy": 0, "notional_jpy": 1e6},
		"notional negative":  {"usd_jpy": 150, "notional_jpy": -1},
		"model_id too long":  {"model_id": strings.Repeat("m", 101)},
		"cohort without ver": {"candidates": []string{"live_cohort:model-1"}},
	} {
		if r := srv.call(t, args); r.status != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, body %s; want 422", name, r.status, r.body)
		}
	}
	srv.post(t, "tools/call", map[string]any{"name": tool, "arguments": map[string]any{"from": "2026-10-09", "to": "2026-10-05"}}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidPeriod)
}

// More candidates than MaxCandidates found in the period is limit_exceeded, not a partial table.
func TestTooManyCandidatesIsLimitExceeded(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seedBase(t, w)
	for i := range ledgerdiag.MaxCandidates { // 6 baselines + MaxCandidates shadow versions
		shadow(t, w, decs[0].id, fmt.Sprintf("trader-s%03d", i), "m", "LONG", 0.001, false)
	}
	srv.call(t, nil).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	if r := srv.call(t, map[string]any{"candidates": []string{"shadow:trader-s000"}}); r.status != http.StatusOK {
		t.Errorf("an explicit short list must work: %d %s", r.status, r.body)
	}
}
