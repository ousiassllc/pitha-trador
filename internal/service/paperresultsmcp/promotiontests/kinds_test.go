package promotiontests

import (
	"fmt"
	"net/http"
	"testing"
)

// A baseline or a research model can be proposed too, and a baseline can be the current answer; a live_cohort
// judged other decisions, so every comparison item is unknown (different_decisions).
func TestOtherKindsOfCandidateAndCurrent(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seed(t, w)
	shadowAnswers(t, w, "trader-v9", "strong-model", 0.001, decs, follow)
	res := srv.post(t, tool, withPeriod(map[string]any{"candidate": map[string]any{"kind": "baseline", "id": "long_leg"}})).result(t)
	if n := item(t, res, "common_rows"); n.Actual.(float64) != 312 || res.Candidate.Kind != "baseline" {
		t.Errorf("baseline candidate: common_rows = %+v", n)
	}
	// the live answer is LONG on every row, so long_leg is the same as live: no difference
	if d := item(t, res, "diff_ci_lower"); d.Status != "fail" || d.Actual.(float64) != 0 {
		t.Errorf("long_leg vs live: %+v", d)
	}
	// shadow vs the long_leg baseline as the current answer
	res = srv.post(t, tool, withPeriod(map[string]any{
		"candidate": map[string]any{"kind": "shadow", "id": "trader-v9"}, "current": map[string]any{"kind": "baseline", "id": "long_leg"},
	})).result(t)
	if res.Current.Kind != "baseline" || res.Current.ID != "long_leg" || item(t, res, "diff_ci_lower").Status != "pass" {
		t.Errorf("shadow vs baseline: %+v", res)
	}
	testkitExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (9000, 1, 'S', '2026-10-05T00:00:00.000000000Z', 'trader', 'h', '{}', 'trader-old', '{}', 'LONG', 0.7, 5, 'old-model')`)
	res = srv.post(t, tool, withPeriod(map[string]any{"candidate": map[string]any{"kind": "live_cohort", "id": "old-model/trader-old"}})).result(t)
	for _, name := range []string{"same_rows_and_period", "common_rows", "common_days", "diff_ci_lower", "multiple_comparison", "positive_days", "not_one_day_wins", "max_drawdown", "after_ai_cost"} {
		if it := item(t, res, name); it.Status != "unknown" || it.Detail != "different_decisions" {
			t.Errorf("%s = %+v, want unknown/different_decisions", name, it)
		}
	}
	if res.EligibleAsEvidence || res.FirstUnmet == nil || *res.FirstUnmet != "same_rows_and_period" {
		t.Errorf("live_cohort: %+v", res)
	}
}

func TestRejectsBadArguments(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	seed(t, w)
	cand := map[string]any{"kind": "shadow", "id": "trader-v9"}
	for name, args := range map[string]map[string]any{
		"no candidate":        {},
		"setting":             {"candidate": map[string]any{"kind": "setting", "id": "x"}},
		"experiment current":  {"candidate": cand, "current": map[string]any{"kind": "experiment", "id": "7"}},
		"live as candidate":   {"candidate": map[string]any{"kind": "live", "id": "live"}},
		"cohort as current":   {"candidate": cand, "current": map[string]any{"kind": "live_cohort", "id": "m/trader-v2"}},
		"same as current":     {"candidate": map[string]any{"kind": "baseline", "id": "none"}, "current": map[string]any{"kind": "baseline", "id": "none"}},
		"unknown kind":        {"candidate": map[string]any{"kind": "oracle", "id": "x"}},
		"unknown field":       {"candidate": cand, "x": 1},
		"unknown ref field":   {"candidate": map[string]any{"kind": "shadow", "id": "x", "extra": 1}},
		"negative extra":      {"candidate": cand, "family_size_extra": -1},
		"huge extra":          {"candidate": cand, "family_size_extra": 1_000_000},
		"bad horizon":         {"candidate": cand, "horizon": "5m"},
		"bad usd_jpy":         {"candidate": cand, "usd_jpy": 0, "notional_jpy": 1},
		"fractional min rows": {"candidate": cand, "min_common_rows": 300.5},
	} {
		if r := srv.post(t, tool, withPeriod(args)); r.status != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, body %s; want 422", name, r.status, r.body)
		}
	}
}

// A research model trained on one label horizon is not evidence when it is graded on another: the checklist says
// so and is not eligible, whatever the items say. The same model at its own horizon carries no such note.
func TestResearchModelAtAnotherHorizonIsNotEvidence(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seed(t, w)
	testkitExec(t, w, `INSERT INTO research_model_runs (version, algorithm, label_horizon, features_json, hyperparams_json, coefficients_json,
		train_from, train_to, predict_day, n_train, n_predicted, train_digest) VALUES ('rv1', 'ridge', '30m', '{}', '{}', '{}', '2026-09-01', '2026-09-30', '2026-10-05', 10, 8, 'd')`)
	for _, d := range decs {
		testkitExec(t, w, `INSERT INTO research_model_predictions (run_id, jev_decision_id, instrument_id, snapshot_at, day, pred_long_net_bps, pred_short_net_bps, direction)
			VALUES ((SELECT id FROM research_model_runs WHERE version = 'rv1'), ?, 1, ?, ?, 1, -1, ?)`, d.id, stamp(t, d.at()), fmt.Sprintf("2026-10-%02d", d.day), follow(d))
	}
	cand := map[string]any{"kind": "research_model", "id": "ridge:30m"}
	res := srv.post(t, tool, withPeriod(map[string]any{"candidate": cand})).result(t) // default horizon 10m
	if res.HorizonNote == "" || res.EligibleAsEvidence {
		t.Errorf("10m horizon: note %q eligible %v, want a note and not eligible", res.HorizonNote, res.EligibleAsEvidence)
	}
	res = srv.post(t, tool, withPeriod(map[string]any{"candidate": cand, "horizon": "30m"})).result(t)
	if res.HorizonNote != "" {
		t.Errorf("30m horizon: unexpected note %q", res.HorizonNote)
	}
}
