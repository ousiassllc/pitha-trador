package promotiontests

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/promotiondiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	expKey      = "risk.paper.max_trade_loss_pct"
	expStart    = "2026-10-02T15:00:00Z" // JST 10-03 00:00
	expEnd      = "2026-10-12T15:00:00Z" // 10 JST days after, and the 10 just before
	tradesByDay = 30
)

// plantTrades closes n trades on each of the 10 JST days from start, alternating mean+d / mean-d a minute apart.
func plantTrades(t *testing.T, w *sql.DB, start string, perDay int, mean, d float64) {
	t.Helper()
	at, err := time.Parse(time.RFC3339, start)
	if err != nil {
		t.Fatal(err)
	}
	for day := range 10 {
		for i := range perDay {
			pnl := mean + d
			if i%2 == 1 {
				pnl = mean - d
			}
			closed := at.AddDate(0, 0, day).Add(time.Duration(i+1) * time.Minute)
			testkit.Position(t, w, closed.Format(time.RFC3339), &pnl, 1000, 100, nil, 99, nil)
		}
	}
}

// experimentOf records a closed experiment of the setting key the way experiment_create does (and the audit row of
// its switch at its start, which the experiment's own judgement requires). It returns the id.
func experimentOf(t *testing.T, w *sql.DB, kind, name, started, ended string) int64 {
	t.Helper()
	if kind == "setting" {
		testkit.MustExec(t, w, `INSERT INTO mcp_settings_audit (at, actor, client, action, setting_key, before_value, after_value)
			VALUES (?, 'gui', '', 'set', ?, '1', '0.5')`, testkit.Stored(t, started), name)
	}
	res, err := w.ExecContext(t.Context(), `INSERT INTO tuning_experiments (created_at, actor, client, hypothesis, variable_kind, variable_name,
		before_value, after_value, audit_id, started_at, ended_at, status) VALUES (?, 'gui', '', 'h', ?, ?, '1', '0.5', NULL, ?, ?, 'closed')`,
		testkit.Stored(t, ended), kind, name, testkit.Stored(t, started), testkit.Stored(t, ended))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func settingsSetServer(t *testing.T) (*testServer, *sql.DB) {
	t.Helper()
	srv, w := newServer(t, "")
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'a', 'TSE')`)
	return srv, w
}

func expCall(srv *testServer, t *testing.T, kind, id string, extra map[string]any) response {
	t.Helper()
	return srv.post(t, tool, merge(map[string]any{"candidate": map[string]any{"kind": kind, "id": id}}, extra))
}

// A tuning experiment is a candidate: the same eleven items over its before / after comparison. The items that
// need paired rows, an AI cost or a cutoff are reported as not applicable (unknown with a reason, never fail),
// so the result is never eligible as evidence; the rest pass for a clear win. Nothing is applied or written.
func TestExperimentCandidateIsJudgedWithTheSameItems(t *testing.T) {
	t.Parallel()
	srv, w := settingsSetServer(t)
	plantTrades(t, w, "2026-09-22T15:00:00Z", tradesByDay, 100, 200)
	plantTrades(t, w, expStart, tradesByDay, 1100, 200)
	id := experimentOf(t, w, "setting", expKey, expStart, expEnd)
	before := dump(t, w)

	res := expCall(srv, t, "experiment", fmt.Sprint(id), nil).result(t)
	if res.Applied || res.EligibleAsEvidence || res.EvidenceNote == "" || res.KeyNumbers != nil || res.ExperimentNumbers == nil || res.Experiment == nil {
		t.Fatalf("result = %+v", res)
	}
	if res.Experiment.Verdict != "improved" || !res.Experiment.Judged || res.Current.Kind != "experiment_before" || res.Current.ID != fmt.Sprint(id) {
		t.Errorf("experiment = %+v, current = %+v", res.Experiment, res.Current)
	}
	if len(res.Items) != 11 {
		t.Fatalf("items = %+v", res.Items)
	}
	wantNA := map[string]string{
		"same_rows_and_period": "not_applicable_unpaired_periods", "after_ai_cost": "not_applicable_ai_cost_not_in_trade_pnl",
		"after_training_cutoff": "not_applicable_trades_not_cutoff_filtered",
	}
	for _, it := range res.Items {
		if detail, na := wantNA[it.Name]; na {
			if it.Status != "unknown" || it.Detail != detail || !it.NotApplicable {
				t.Errorf("%s = %+v, want unknown / %s / not_applicable (never fail)", it.Name, it, detail)
			}
		} else if it.Status != "pass" || it.NotApplicable {
			t.Errorf("%s = %+v, want pass", it.Name, it)
		}
	}
	n := res.ExperimentNumbers
	if n.BeforeTrades != 300 || n.AfterTrades != 300 || n.BeforeDays != 10 || n.AfterDays != 10 || n.DiffMeanJPY == nil || *n.DiffMeanJPY != 1000 ||
		n.HolmAdjustedP == nil || n.FamilySize != 1 {
		t.Errorf("numbers = %+v", n)
	}
	if res.FirstUnmet != nil {
		t.Errorf("first_unmet = %v, want none (every applicable item passed)", *res.FirstUnmet)
	}
	// the tool applied nothing: no table (settings, prompts, models, experiments, positions) changed
	after := dump(t, w)
	for table, h := range before {
		if after[table] != h {
			t.Errorf("table %s changed", table)
		}
	}
}

// A thin experiment: the items that read the interval are unknown with the experiment's own reason, the
// count and day floors fail, and first_unmet is the first applicable item that did not pass (not the N/A one).
func TestExperimentCandidateWithTooFewTrades(t *testing.T) {
	t.Parallel()
	srv, w := settingsSetServer(t)
	plantTrades(t, w, "2026-09-22T15:00:00Z", 5, 100, 200)
	plantTrades(t, w, expStart, 5, 1100, 200)
	id := experimentOf(t, w, "setting", expKey, expStart, expEnd)
	res := expCall(srv, t, "experiment", fmt.Sprint(id), map[string]any{"min_common_rows": 400}).result(t)
	if rows := item(t, res, "common_rows"); rows.Status != "fail" || rows.Actual.(float64) != 50 || rows.Required.(float64) != 400 {
		t.Errorf("common_rows = %+v", rows)
	}
	if res.FirstUnmet == nil || *res.FirstUnmet != "common_rows" || res.EligibleAsEvidence {
		t.Errorf("first_unmet = %v eligible %v", res.FirstUnmet, res.EligibleAsEvidence)
	}
	for _, name := range []string{"diff_ci_lower", "multiple_comparison", "positive_days", "not_one_day_wins", "max_drawdown"} {
		if it := item(t, res, name); it.Status != "unknown" || it.Detail != "experiment_insufficient_trades" || it.NotApplicable {
			t.Errorf("%s = %+v, want unknown / experiment_insufficient_trades", name, it)
		}
	}
	if res.Experiment.Judged || res.Experiment.Verdict != "insufficient_trades" {
		t.Errorf("experiment = %+v", res.Experiment)
	}
}

// Something else changing around the experiment withholds its judgement (its numbers are still returned), and a
// Jev model change shows in model_version_stable.
func TestExperimentCandidateConfoundedByAnotherSetting(t *testing.T) {
	t.Parallel()
	srv, w := settingsSetServer(t)
	plantTrades(t, w, "2026-09-22T15:00:00Z", tradesByDay, 100, 200)
	plantTrades(t, w, expStart, tradesByDay, 1100, 200)
	id := experimentOf(t, w, "setting", expKey, expStart, expEnd)
	testkit.MustExec(t, w, `INSERT INTO mcp_settings_audit (at, actor, client, action, setting_key, before_value, after_value)
		VALUES (?, 'gui', '', 'set', 'risk.paper.max_total_exposure_pct', '3', '4')`, testkit.Stored(t, "2026-10-06T00:00:00Z"))
	res := expCall(srv, t, "experiment", fmt.Sprint(id), nil).result(t)
	if res.Experiment.Verdict != "confounded" || res.Experiment.Confounders != 1 {
		t.Fatalf("experiment = %+v", res.Experiment)
	}
	if it := item(t, res, "diff_ci_lower"); it.Status != "unknown" || it.Detail != "experiment_confounded" {
		t.Errorf("diff_ci_lower = %+v", it)
	}
	if res.ExperimentNumbers.DiffCILowerJPY == nil || res.EligibleAsEvidence {
		t.Errorf("numbers %+v eligible %v", res.ExperimentNumbers, res.EligibleAsEvidence)
	}
	if it := item(t, res, "model_version_stable"); it.Status != "pass" {
		t.Errorf("a setting change is not a model change: %+v", it)
	}
	// a Jev model that changes inside the after period
	for i, model := range []string{"m-a", "m-b"} {
		testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction, confidence, latency_ms, model_id)
			VALUES (?, 1, '7203', ?, 'trader', 'h', '{}', 'trader-v3', '{}', 'LONG', 0.5, 5, ?)`, 100+i, testkit.Stored(t, fmt.Sprintf("2026-10-0%dT00:00:00Z", 4+i)), model)
	}
	res = expCall(srv, t, "experiment", fmt.Sprint(id), nil).result(t)
	if it := item(t, res, "model_version_stable"); it.Status != "fail" || it.Detail != "model_or_prompt_version_changed" {
		t.Errorf("model_version_stable = %+v", it)
	}
	if res.FirstUnmet == nil || *res.FirstUnmet != "diff_ci_lower" {
		t.Errorf("first_unmet = %v", res.FirstUnmet)
	}
}

// A setting candidate reads its newest experiment; with none recorded every item is unknown with a reason (not
// an error), and a stricter bps criterion cannot be compared with a yen interval.
func TestSettingCandidateReadsTheNewestExperiment(t *testing.T) {
	t.Parallel()
	srv, w := settingsSetServer(t)
	none := expCall(srv, t, "setting", expKey, nil).result(t)
	if none.Experiment != nil || none.EligibleAsEvidence || none.Applied || none.FirstUnmet == nil || *none.FirstUnmet != "same_rows_and_period" || none.EvidenceNote == "" {
		t.Fatalf("no experiment: %+v", none)
	}
	for _, it := range none.Items {
		if it.Status != "unknown" || it.Detail != "no_experiment_result" {
			t.Errorf("no experiment: %+v", it)
		}
	}
	plantTrades(t, w, "2026-09-22T15:00:00Z", tradesByDay, 100, 200)
	plantTrades(t, w, expStart, tradesByDay, 1100, 200)
	experimentOf(t, w, "prompt", "trader-v3", "2026-08-01T00:00:00Z", "2026-08-02T00:00:00Z") // another variable: not picked
	id := experimentOf(t, w, "setting", expKey, expStart, expEnd)
	res := expCall(srv, t, "setting", expKey, nil).result(t)
	if res.Experiment == nil || res.Experiment.ID != id || res.Candidate.Kind != "setting" || res.Candidate.ID != expKey {
		t.Fatalf("result = %+v", res)
	}
	if res.ExperimentNumbers.FamilySize != 2 { // both recorded experiments are the family
		t.Errorf("family_size = %d", res.ExperimentNumbers.FamilySize)
	}
	extra := expCall(srv, t, "setting", expKey, map[string]any{"family_size_extra": 3, "min_diff_ci_lower_bps": 1.5}).result(t)
	if extra.ExperimentNumbers.FamilySize != 5 || extra.ExperimentNumbers.FamilySizeExtra != 3 {
		t.Errorf("family = %+v", extra.ExperimentNumbers)
	}
	if it := item(t, extra, "diff_ci_lower"); it.Status != "unknown" || it.Detail != "criterion_in_bps_not_comparable_to_yen_per_trade" || it.Required.(float64) != 1.5 {
		t.Errorf("diff_ci_lower = %+v", it)
	}
	// the thresholds can only be stricter than the floors, as for the other kinds
	expCall(srv, t, "setting", expKey, map[string]any{"min_days": 9}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
}

// An experiment id that was never recorded is experiment_not_found; the text of the result carries no dev notes.
func TestExperimentCandidateErrorsAndTexts(t *testing.T) {
	t.Parallel()
	srv, w := settingsSetServer(t)
	expCall(srv, t, "experiment", "99", nil).wantError(t, http.StatusNotFound, paperresultsmcp.CodeExperimentNotFound)
	id := experimentOf(t, w, "setting", expKey, expStart, expEnd)
	res := expCall(srv, t, "experiment", fmt.Sprint(id), nil)
	if body := string(res.body); strings.Contains(body, "#1") || strings.Contains(body, "issue") {
		t.Errorf("the response mentions an issue: %s", body)
	}
	if got := res.result(t); got.Items[0].Name != promotiondiag.ItemSameRows {
		t.Errorf("items = %+v", got.Items)
	}
}
