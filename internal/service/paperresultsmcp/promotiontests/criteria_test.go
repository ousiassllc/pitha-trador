package promotiontests

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Every one of the five criteria can only be made stricter per call: a value looser than the documented
// floor is an invalid argument, a stricter value is accepted (the floor itself is the default every other test runs with).
func TestLooseningAnyCriterionPerCallIsRefused(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seed(t, w)
	shadowAnswers(t, w, "trader-v9", "strong-model", 0.001, decs, follow)
	cases := []struct {
		key           string
		loose         any
		stricter      any
		effectiveWant any
	}{
		{"min_common_rows", 299, 301, 301.0},
		{"min_days", 9, 11, 11.0},
		{"min_diff_ci_lower_bps", -0.001, 0.5, 0.5},
		{"min_positive_day_pct", 49.9, 60.0, 60.0},
		{"max_adjusted_p", 0.0501, 0.01, 0.01},
	}
	// a loose value is refused while the arguments are decoded: no ledger is computed for it
	stricter := map[string]any{}
	for _, c := range cases {
		srv.call(t, "trader-v9", map[string]any{c.key: c.loose}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
		stricter[c.key] = c.stricter
	}
	// every stricter value is accepted together and reflected, each with its source
	res := srv.call(t, "trader-v9", stricter).result(t)
	got := map[string]any{
		"min_common_rows": float64(res.Criteria.Effective.MinCommonRows), "min_days": float64(res.Criteria.Effective.MinDays),
		"min_diff_ci_lower_bps": res.Criteria.Effective.MinDiffCILowerBps, "min_positive_day_pct": res.Criteria.Effective.MinPositiveDayPct,
		"max_adjusted_p": res.Criteria.Effective.MaxAdjustedP,
	}
	for _, c := range cases {
		if got[c.key] != c.effectiveWant || res.Criteria.Source["research.promotion."+c.key] != "call" {
			t.Errorf("%s: effective = %v source %v, want %v from the call", c.key, got[c.key], res.Criteria.Source, c.effectiveWant)
		}
	}
}

// A stored setting looser than its floor is ignored (the floor applies, and the source says floor); a stricter
// one applies and the call may not loosen it. Damaged values are ignored too.
func TestStoredCriteriaAreTightenOnly(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seed(t, w)
	shadowAnswers(t, w, "trader-v9", "strong-model", 0.001, decs, follow)
	setting(t, w, "research.promotion.min_days", `5`)               // looser than the floor 10
	setting(t, w, "research.promotion.min_common_rows", `100`)      // looser than the floor 300
	setting(t, w, "research.promotion.max_adjusted_p", `0.2`)       // looser than the floor 0.05
	setting(t, w, "research.promotion.min_positive_day_pct", `"x"`) // damaged
	res := srv.call(t, "trader-v9", nil).result(t)
	if res.Criteria.Effective != res.Criteria.Floor {
		t.Errorf("looser stored settings were not ignored: %+v", res.Criteria)
	}
	for k, s := range res.Criteria.Source {
		if s != "floor" {
			t.Errorf("source of %s = %s", k, s)
		}
	}
	if d := item(t, res, "common_days"); d.Required.(float64) != 10 {
		t.Errorf("common_days required %v, want the floor 10", d.Required)
	}

	setting(t, w, "research.promotion.min_days", `13`) // stricter than the floor, more than the 12 days of the data
	res = srv.call(t, "trader-v9", nil).result(t)
	if d := item(t, res, "common_days"); d.Status != "fail" || d.Required.(float64) != 13 || d.Actual.(float64) != 12 || res.Criteria.Source["research.promotion.min_days"] != "setting" {
		t.Errorf("stored min_days 13: %+v, source %v", d, res.Criteria.Source)
	}
	if res.FirstUnmet == nil || *res.FirstUnmet != "common_days" {
		t.Errorf("first_unmet = %v", res.FirstUnmet)
	}
	// the call may not go below the stored 13 (though it is above the floor 10)...
	srv.call(t, "trader-v9", map[string]any{"min_days": 11}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	// ...but may equal or exceed it
	if res = srv.call(t, "trader-v9", map[string]any{"min_days": 14}).result(t); res.Criteria.Effective.MinDays != 14 || res.Criteria.Source["research.promotion.min_days"] != "call" {
		t.Errorf("min_days 14: %+v", res.Criteria)
	}
}

// Calling the tool changes nothing: every table (runtime_settings with its promotion settings, the prompt
// promotions, the shadow and trial rows, the research tables) is byte-identical afterwards, only SELECTs ran,
// no raw body column was read, no settings value besides the five criteria leaks, and applied is always false.
func TestCallWritesNothingAndAppliesNothing(t *testing.T) {
	t.Parallel()
	driverName, counter := testkit.NewCountingDriver(t)
	srv, w := newServer(t, driverName)
	decs := seed(t, w)
	shadowAnswers(t, w, "trader-v9", "strong-model", 0.001, decs, follow)
	setting(t, w, "research.promotion.min_days", `11`)
	setting(t, w, "llm.api_key", `"SENTINEL_SECRET_SETTING_VALUE"`)
	testkit.MustExec(t, w, `INSERT INTO jev_prompt_promotions (at, actor, client, from_version, to_version) VALUES ('2026-10-01T00:00:00Z', 'gui', '', 'trader-v2', 'trader-v3')`)
	testkit.MustExec(t, w, `INSERT INTO jev_trial_decisions (jev_decision_id, arm_id, prompt_version, requested_model, status, direction, regime, entry_quality, toxic_flow,
		liquidity_stressed, continuation_probability, response_json, error_excerpt, cost_source, counted_cost_usd)
		VALUES (1, 'arm-a', 'trader-v3', 'm', 'ok', 'LONG', 'TREND', 'good', 0, 0, 0.5, '{"k":"SENTINEL_TRIAL_BODY"}', 'SENTINEL_ERROR_TEXT', 'reported', 0.01)`)
	before := dump(t, w)
	if len(before) < 20 {
		t.Fatalf("dump covers %d tables, the check is vacuous", len(before))
	}
	start := len(counter.StatementLog())
	for _, extra := range []map[string]any{nil, {"horizon": "30m"}, {"min_days": 1}} { // the last one is refused: an error call writes nothing either
		r := srv.call(t, "trader-v9", extra)
		for _, leak := range []string{"SENTINEL_SECRET_SETTING_VALUE", "SENTINEL_TRIAL_BODY", "SENTINEL_ERROR_TEXT", testAPIKey, "response_json", "error_excerpt"} {
			if strings.Contains(string(r.body), leak) {
				t.Errorf("%v: the response contains %q", extra, leak)
			}
		}
		if r.status == http.StatusOK && r.result(t).Applied {
			t.Errorf("%v: applied = true", extra)
		}
	}
	srv.post(t, tool, withPeriod(map[string]any{"candidate": map[string]any{"kind": "experiment", "id": "1"}})).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
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
	for table, h := range dump(t, w) {
		if before[table] != h {
			t.Errorf("table %s changed during promotion_checklist", table)
		}
	}
	db, err := srv.DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE runtime_settings SET value = '1'`); err == nil {
		t.Error("a write succeeded on the MCP connection")
	}
}
