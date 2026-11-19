package replaytests

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/replaytool"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The five tools are registered; only start and stop are annotated as writing.
func TestTools_AreRegisteredAndOnlyStartAndStopWrite(t *testing.T) {
	srv, _ := newServer(t)
	r := srv.post(t, "tools/list", nil)
	if r.status != http.StatusOK {
		t.Fatalf("tools/list: %d %s", r.status, r.body)
	}
	var env struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				Annotations map[string]any `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	readOnly := map[string]bool{}
	for _, tl := range env.Result.Tools {
		if strings.HasPrefix(tl.Name, "replay_") {
			readOnly[tl.Name], _ = tl.Annotations["readOnlyHint"].(bool)
		}
	}
	want := map[string]bool{"replay_estimate": true, "replay_start": false, "replay_stop": false, "replay_progress": true, "replay_results": true}
	if len(readOnly) != len(want) {
		t.Fatalf("replay tools = %v, want %v", readOnly, want)
	}
	for name, ro := range want {
		if got, ok := readOnly[name]; !ok || got != ro {
			t.Errorf("%s readOnlyHint = %v (listed %v), want %v", name, got, ok, ro)
		}
	}
}

func TestEstimate_CountsDecisionsTimesArmsAfterTheCutoffRuleAndWritesNothing(t *testing.T) {
	srv, w := newServer(t)
	seedDecisions(t, w)
	before := count(t, w, "jev_replay_runs") + count(t, w, "jev_replay_results") + count(t, w, "runtime_settings")

	var est replaytool.EstimateView
	srv.call(t, "replay_estimate", map[string]any{"max_rows": 10}).data(t, "replay_estimate", &est)

	if est.Decisions != 4 || est.Calls != 6 {
		t.Errorf("decisions %d calls %d, want 4 decisions and 6 calls (known arm: 2 after its cutoff; unknown arm: 4)", est.Decisions, est.Calls)
	}
	if est.MaxCostUSD <= 0 || est.ConfirmToken == "" || est.ReplayEnabled {
		t.Errorf("max cost %v token %q enabled %v, want a worst-case cost, a token and the switch off", est.MaxCostUSD, est.ConfirmToken, est.ReplayEnabled)
	}
	if est.ListPriceUSD != nil {
		t.Errorf("list price estimate %v for arms without a list price, want null", *est.ListPriceUSD)
	}
	if len(est.Arms) != 2 {
		t.Fatalf("arms = %+v", est.Arms)
	}
	known, unknown := est.Arms[0], est.Arms[1]
	if known.ArmID != "known" || known.Calls != 2 || known.ExcludedBeforeCutoff != 2 || known.LLMCutoffStatus != "verified" || known.ReferenceOnly {
		t.Errorf("known arm = %+v, want 2 calls, 2 excluded before the cutoff, verified", known)
	}
	if unknown.ArmID != "unknown" || unknown.Calls != 4 || unknown.ExcludedBeforeCutoff != 0 || unknown.LLMCutoffStatus != "unverified" ||
		unknown.LLMCutoffLabel != "未確認" || !unknown.ReferenceOnly {
		t.Errorf("unknown arm = %+v, want 4 calls, unverified (未確認) and reference-only", unknown)
	}
	if after := count(t, w, "jev_replay_runs") + count(t, w, "jev_replay_results") + count(t, w, "runtime_settings"); after != before {
		t.Errorf("an estimate changed the database (%d rows -> %d)", before, after)
	}
}

func TestEstimate_RequiresMaxRowsAndRejectsABadRequest(t *testing.T) {
	srv, w := newServer(t)
	seedDecisions(t, w)
	for _, args := range []map[string]any{
		{},                   // no max_rows
		{"max_rows": 0},      // zero rows
		{"max_rows": 100000}, // more than a run may cover
		{"max_rows": 10, "arms": []string{"nope"}}, // an arm that is not configured
		{"max_rows": 10, "decision_type": "scout"}, // only trader decisions can be replayed
		{"max_rows": 10, "run_now": true},          // an unknown parameter
	} {
		srv.call(t, "replay_estimate", args).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	}
}

func TestStart_NeedsTheSwitchTheTokenAndTheEstimate(t *testing.T) {
	srv, w := newServer(t)
	seedDecisions(t, w)
	var est replaytool.EstimateView
	srv.call(t, "replay_estimate", map[string]any{"max_rows": 10}).data(t, "replay_estimate", &est)

	// The replay switch is off (the default): no run, and the switch is not touched.
	srv.call(t, "replay_start", map[string]any{"max_rows": 10, "confirm_token": est.ConfirmToken}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
	if n := count(t, w, "jev_replay_runs"); n != 0 {
		t.Fatalf("%d runs with the switch off", n)
	}
	switchOn(t, w)

	// No token, and a token of another estimate, are refused: no start without an estimate.
	srv.call(t, "replay_start", map[string]any{"max_rows": 10}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "replay_start", map[string]any{"max_rows": 10, "confirm_token": "0123456789abcdef"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
	srv.call(t, "replay_start", map[string]any{"max_rows": 2, "confirm_token": est.ConfirmToken}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
	if n := count(t, w, "jev_replay_runs"); n != 0 {
		t.Fatalf("%d runs after refused starts", n)
	}

	var run replaytool.RunView
	srv.call(t, "replay_start", map[string]any{"max_rows": 10, "confirm_token": est.ConfirmToken}).data(t, "replay_start", &run)
	if run.RunID != 1 || run.Status != "pending" || run.Progress.CallsPlanned != 6 || run.Progress.MaxCostUSD != est.MaxCostUSD || run.Progress.CallsDone != 0 {
		t.Errorf("run = %+v, want pending with 6 planned calls and the estimate's cost", run)
	}
	if n := count(t, w, "jev_replay_runs"); n != 1 {
		t.Errorf("%d runs, want 1", n)
	}
	if n := count(t, w, "jev_replay_results"); n != 0 {
		t.Errorf("%d results after a start, want 0: the MCP sends nothing", n)
	}
	var enabled string
	if err := w.QueryRow(`SELECT value FROM runtime_settings WHERE key = 'jev.replay.enabled'`).Scan(&enabled); err != nil || enabled != "1" {
		t.Errorf("the replay switch = %q, %v: a start must not change it", enabled, err)
	}

	// One run at a time.
	srv.call(t, "replay_start", map[string]any{"max_rows": 10, "confirm_token": est.ConfirmToken}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
	if n := count(t, w, "jev_replay_runs"); n != 1 {
		t.Errorf("%d runs after a second start, want 1", n)
	}
}

func TestStart_IsRefusedWhileMCPWritesAreOff(t *testing.T) {
	srv, w := newServer(t)
	seedDecisions(t, w)
	switchOn(t, w)
	var est replaytool.EstimateView
	srv.call(t, "replay_estimate", map[string]any{"max_rows": 10}).data(t, "replay_estimate", &est)
	testkit.MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('mcp.settings_write_enabled', '0', ?)`, testkit.Stored(t, "2026-10-10T00:00:00Z"))

	srv.call(t, "replay_start", map[string]any{"max_rows": 10, "confirm_token": est.ConfirmToken}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	if n := count(t, w, "jev_replay_runs"); n != 0 {
		t.Errorf("%d runs with MCP writes off", n)
	}
}

func TestStop_SetsTheFlagOfAnActiveRunOnly(t *testing.T) {
	srv, w := newServer(t)
	seedDecisions(t, w)
	plantRun(t, w, 1, "running")
	plantRun(t, w, 2, "completed")
	// A stop is an act that only reduces spending: it works with MCP writes off.
	testkit.MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('mcp.settings_write_enabled', '0', ?)`, testkit.Stored(t, "2026-10-10T00:00:00Z"))

	var run replaytool.RunView
	srv.call(t, "replay_stop", map[string]any{"run_id": 1}).data(t, "replay_stop", &run)
	if !run.StopRequested || run.Status != "running" {
		t.Errorf("run = %+v, want the stop flag set (the runner ends it)", run)
	}
	var flag int
	if err := w.QueryRow(`SELECT stop_requested FROM jev_replay_runs WHERE id = 2`).Scan(&flag); err != nil || flag != 0 {
		t.Errorf("a finished run's flag = %d, %v, want 0", flag, err)
	}
	srv.call(t, "replay_stop", map[string]any{"run_id": 2}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
	srv.call(t, "replay_stop", map[string]any{"run_id": 99}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "replay_stop", map[string]any{}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
}

func TestProgress_ListsRunsAndOneRun(t *testing.T) {
	srv, w := newServer(t)
	seedDecisions(t, w)
	plantRun(t, w, 1, "completed")
	plantRun(t, w, 2, "running")
	testkit.MustExec(t, w, `UPDATE jev_replay_runs SET blocked_reason = 'daily_cap_overall', calls_done = 3, calls_failed = 1, counted_cost_usd = 0.00006 WHERE id = 2`)

	var list replaytool.ProgressList
	srv.call(t, "replay_progress", map[string]any{}).data(t, "replay_progress", &list)
	if len(list.Runs) != 2 || list.Runs[0].RunID != 2 || list.Runs[1].RunID != 1 {
		t.Fatalf("runs = %+v, want newest first: 2, 1", list.Runs)
	}
	var one replaytool.ProgressList
	srv.call(t, "replay_progress", map[string]any{"run_id": 2}).data(t, "replay_progress", &one)
	got := one.Runs[0]
	if got.Status != "running" || got.BlockedReason != "daily_cap_overall" || got.Progress.CallsDone != 3 || got.Progress.CallsFailed != 1 ||
		got.Progress.CallsRemaining != 3 || got.Progress.CountedCostUSD != 0.00006 {
		t.Errorf("run = %+v", got)
	}
	if len(got.Arms) != 2 || got.Arms[1].LLMCutoffLabel != "未確認" || !got.Arms[1].ReferenceOnly {
		t.Errorf("arms = %+v, want the unknown arm shown as 未確認 and reference-only", got.Arms)
	}
	srv.call(t, "replay_progress", map[string]any{"run_id": 99}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
}

// replay_results gives the trial comparison's metrics over the run's rows against
// the live decisions, marks reference-only arms, and never returns a response
// body, an error text or a key.
func TestResults_SameMetricsAsTheTrialComparisonAndNoSecrets(t *testing.T) {
	srv, w := newServer(t)
	ids := seedDecisions(t, w)
	plantRun(t, w, 1, "completed")
	// The known arm answered the two decisions after its cutoff; the unknown arm
	// answered all four, one of them failed.
	plantResult(t, w, 1, ids[2], "known", "ok", "LONG", "verified")
	plantResult(t, w, 1, ids[3], "known", "ok", "SHORT", "verified")
	for i, id := range ids {
		status := "ok"
		if i == 0 {
			status = "failed"
		}
		plantResult(t, w, 1, id, "unknown", status, "LONG", "unverified")
	}

	r := srv.call(t, "replay_results", map[string]any{"run_id": 1})
	var out replaytool.ResultsView
	r.data(t, "replay_results", &out)

	if !out.ReferenceOnly {
		t.Error("a run with an unverified arm is not marked reference-only")
	}
	cmp := out.Comparison
	if cmp.Cohort.Size != 2 || cmp.Cohort.CommonSuccessCount != 2 || len(cmp.PerArm) != 2 {
		t.Errorf("cohort %d common success %d arms %d, want the 2 decisions both arms answered, both ok, 2 arms", cmp.Cohort.Size, cmp.Cohort.CommonSuccessCount, len(cmp.PerArm))
	}
	byArm := map[string]float64{}
	for _, a := range cmp.PerArm {
		if a.PeriodRows.MissingRate == nil {
			t.Fatalf("arm %s has no missing rate", a.ArmID)
		}
		byArm[a.ArmID] = *a.PeriodRows.MissingRate
	}
	if byArm["known"] != 0 || byArm["unknown"] != 0.25 {
		t.Errorf("missing rates = %v, want known 0 and unknown 1/4", byArm)
	}
	if !cmp.CounterfactualProxy.Proxy || len(cmp.Definitions) == 0 || cmp.Fairness.Skipped.CountedInMissingRate {
		t.Errorf("proxy %v definitions %d skipped counted %v", cmp.CounterfactualProxy.Proxy, len(cmp.Definitions), cmp.Fairness.Skipped.CountedInMissingRate)
	}
	if !contains(cmp.Fairness.Caveats, "llm_cutoff_unverified") {
		t.Errorf("caveats = %v, want llm_cutoff_unverified", cmp.Fairness.Caveats)
	}
	var costKnown, costUnknown *replaytool.ArmResultCost
	for i := range out.Cost {
		switch out.Cost[i].ArmID {
		case "known":
			costKnown = &out.Cost[i]
		case "unknown":
			costUnknown = &out.Cost[i]
		}
	}
	if costKnown == nil || costUnknown == nil || costKnown.Rows != 2 || costKnown.ReferenceOnlyRows != 0 || costKnown.LLMCutoffLabel != "確認済み" ||
		costUnknown.Rows != 4 || costUnknown.ReferenceOnlyRows != 4 || costUnknown.LLMCutoffLabel != "未確認" || costUnknown.UnknownCostRows != 1 {
		t.Errorf("cost = %+v", out.Cost)
	}

	body := string(r.body)
	for _, secret := range []string{sentinelBody, sentinelError, testAPIKey, "response_json", "error_excerpt", "api_key"} {
		if strings.Contains(body, secret) {
			t.Errorf("replay_results returned %q", secret)
		}
	}
	srv.call(t, "replay_results", map[string]any{"run_id": 99}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "replay_results", map[string]any{}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
}

// The replay switch cannot be set through the MCP's settings tool.
func TestSettingsSet_CannotTurnTheReplayOn(t *testing.T) {
	srv, w := newServer(t)
	srv.call(t, "settings_set", map[string]any{"key": "jev.replay.enabled", "value": 1}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeKeyNotAllowed)
	if n := count(t, w, "runtime_settings"); n != 0 {
		t.Errorf("a refused write left %d settings rows", n)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
