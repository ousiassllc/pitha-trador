package ledgertests

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Sentinels that must never appear in a response: raw bodies and an error text.
const (
	sentinelResponse = "SENTINEL_RESPONSE_JSON_BODY"
	sentinelState    = "SENTINEL_STATE_JSON_BODY"
	sentinelError    = "SENTINEL_ERROR_EXCERPT_BODY"
)

// Scenario price paths, one bar per minute from the decision's bar. With the default label (TP 30,
// SL 30, cost 10 bps, 10 minutes) they give these net returns (bps) for a LONG / SHORT:
//
//	up    LONG +30   SHORT -30
//	down  LONG -30   SHORT +30
//	mild  LONG   0   SHORT -20
//	dip   LONG -20   SHORT   0
func scenarioPrices(name string) []float64 {
	prices := make([]float64, 13)
	for i := range prices {
		prices[i] = 1000
	}
	tail := map[string]struct {
		from  int
		price float64
	}{"up": {2, 1005}, "down": {2, 995}, "mild": {1, 1001}, "dip": {1, 999}}[name]
	for i := tail.from; i < len(prices); i++ {
		prices[i] = tail.price
	}
	return prices
}

const (
	m1 = "model-1"
	m2 = "model-2"
)

var live = jev.TraderQuestionVersion

// dec is one synthetic trader decision with its bars (instrument 1; 2026-10-<day> 09:00 JST + slot*20 minutes).
type dec struct {
	id, day, slot  int
	dir            string
	model, version string
	scenario       string
	cost           any // request_cost: nil is NULL
	r5             any // return_5m of the decision's bar
}

func (d dec) at() time.Time {
	return time.Date(2026, 10, d.day, 0, 0, 0, 0, time.UTC).Add(time.Duration(d.slot) * 20 * time.Minute)
}

func stamp(t *testing.T, ts time.Time) string {
	return testkit.Stored(t, ts.UTC().Format(time.RFC3339))
}

func seedInstruments(t *testing.T, w *sql.DB) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'a', 'TSE')`)
}

// insertDecisions writes the decisions with their bars in one transaction.
func insertDecisions(t *testing.T, w *sql.DB, decs []dec) {
	t.Helper()
	tx, err := w.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, d := range decs {
		ts := stamp(t, d.at())
		if _, err := tx.ExecContext(t.Context(), `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
			question_version, response_json, direction, confidence, latency_ms, model_id, request_cost, created_at)
			VALUES (?, 1, 'S', ?, 'trader', 'h', ?, ?, ?, ?, 0.7, 5, ?, ?, ?)`,
			d.id, ts, `{"k":"`+sentinelState+`"}`, d.version, `{"k":"`+sentinelResponse+`"}`, d.dir, d.model, d.cost, ts); err != nil {
			t.Fatal(err)
		}
		for i, price := range scenarioPrices(d.scenario) {
			var r5 any
			if i == 0 {
				r5 = d.r5
			}
			if _, err := tx.ExecContext(t.Context(), `INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, volume, turnover, vwap,
				price_vs_vwap_bps, raw_data_json, return_5m) VALUES (1, 'S', ?, ?, ?, ?, 1, 1, 1, 0, '{}', ?)`,
				stamp(t, d.at().Add(time.Duration(i)*time.Minute)), price, price, price, r5); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// shadow writes one jev_shadow_decisions row.
func shadow(t *testing.T, w *sql.DB, id int, version, model, dir string, cost float64, estimate bool) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO jev_shadow_decisions (jev_decision_id, prompt_version, model_id, response_json, direction, regime, entry_quality,
		confidence, toxic_flow, liquidity_stressed, continuation_probability, latency_ms, input_tokens, request_cost, cost_is_estimate)
		VALUES (?, ?, ?, ?, ?, 'TREND', 'good', 0.5, 0, 0, 0.5, 1, 100, ?, ?)`, id, version, model, `{"k":"`+sentinelResponse+`"}`, dir, cost, estimate)
}

// trial writes one jev_trial_decisions row; an empty dir is a row without an answer.
func trial(t *testing.T, w *sql.DB, id int, arm, status, dir, served, source string, counted float64) {
	t.Helper()
	var d, regime, quality, toxic, liq, cont any
	if dir != "" {
		d, regime, quality, toxic, liq, cont = dir, "TREND", "good", 0.0, 0.0, 0.5
	}
	failure := ""
	if status == "failed" {
		failure = "timeout"
	}
	testkit.MustExec(t, w, `INSERT INTO jev_trial_decisions (jev_decision_id, arm_id, prompt_version, requested_model, served_model, status, failure_code,
		error_excerpt, direction, regime, entry_quality, toxic_flow, liquidity_stressed, continuation_probability, response_json, cost_source, counted_cost_usd)
		VALUES (?, ?, 'trader-v3', 'req-model', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, arm, served, status, failure, sentinelError, d, regime, quality, toxic, liq, cont, `{"k":"`+sentinelResponse+`"}`, source, counted)
}

// researchRun writes one research_model_runs row and returns its id.
func researchRun(t *testing.T, w *sql.DB, version, algorithm, horizon string) int {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO research_model_runs (version, algorithm, label_horizon, features_json, hyperparams_json, coefficients_json,
		train_from, train_to, predict_day, n_train, n_predicted, train_digest) VALUES (?, ?, ?, '{}', '{}', '{}', '2026-09-01', '2026-09-30', '2026-10-05', 10, 8, 'd')`,
		version, algorithm, horizon)
	var id int
	if err := w.QueryRow(`SELECT id FROM research_model_runs WHERE version = ?`, version).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func prediction(t *testing.T, w *sql.DB, run int, d dec, dir string) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO research_model_predictions (run_id, jev_decision_id, instrument_id, snapshot_at, day, pred_long_net_bps, pred_short_net_bps, direction)
		VALUES (?, ?, 1, ?, ?, 1, -1, ?)`, run, d.id, stamp(t, d.at()), fmt.Sprintf("2026-10-%02d", d.day), dir)
}

// horizonLabel writes a research_horizon_labels row at the decision's bar; a nil net is NULL (no quote on that side).
func horizonLabel(t *testing.T, w *sql.DB, d dec, horizon string, long, short any) {
	t.Helper()
	gross := func(v any) any {
		if v == nil {
			return nil
		}
		return v
	}
	testkit.MustExec(t, w, `INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at,
		entry_price, exit_price, long_gross_bps, long_cost_bps, long_net_bps, short_gross_bps, short_cost_bps, short_net_bps, label_version)
		VALUES (?, 1, 'S', ?, ?, 0, ?, ?, 1000, 1001, ?, ?, ?, ?, ?, ?, 1)`,
		d.id, stamp(t, d.at()), fmt.Sprintf("2026-10-%02d", d.day), horizon, stamp(t, d.at().Add(30*time.Minute)),
		gross(long), costOf(long), long, gross(short), costOf(short), short)
}

func costOf(net any) any {
	if net == nil {
		return nil
	}
	return 0.0
}

// base is the hand-computed live dataset: 8 live decisions on 4 days (the live version, model-1).
//
//	id day dir   scenario  LONG SHORT llm  request_cost
//	1  5   LONG  up         +30  -30   +30  0.004
//	2  5   SHORT up         +30  -30   -30  0.004
//	3  6   LONG  down       -30  +30   -30  0.004
//	4  6   SHORT down       -30  +30   +30  0.004
//	5  7   LONG  mild         0  -20     0  0.004
//	6  7   SHORT dip        -20    0     0  0.004
//	7  8   LONG  up         +30  -30   +30  0.004
//	8  8   SHORT dip        -20    0     0  NULL
//
// The live llm arm has 30/8 = 3.75 bps on the 8 rows.
func base() []dec {
	c := 0.004
	return []dec{
		{id: 1, day: 5, slot: 0, dir: "LONG", scenario: "up", cost: c, r5: 1.0},
		{id: 2, day: 5, slot: 1, dir: "SHORT", scenario: "up", cost: c, r5: 1.0},
		{id: 3, day: 6, slot: 0, dir: "LONG", scenario: "down", cost: c, r5: -1.0},
		{id: 4, day: 6, slot: 1, dir: "SHORT", scenario: "down", cost: c, r5: -1.0},
		{id: 5, day: 7, slot: 0, dir: "LONG", scenario: "mild", cost: c},
		{id: 6, day: 7, slot: 1, dir: "SHORT", scenario: "dip", cost: c, r5: 1.0},
		{id: 7, day: 8, slot: 0, dir: "LONG", scenario: "up", cost: c, r5: -1.0},
		{id: 8, day: 8, slot: 1, dir: "SHORT", scenario: "dip", cost: nil, r5: 1.0},
	}
}

// seedBase writes base() with the live model and version.
func seedBase(t *testing.T, w *sql.DB) []dec {
	t.Helper()
	seedInstruments(t, w)
	decs := base()
	for i := range decs {
		decs[i].model, decs[i].version = m1, live
	}
	insertDecisions(t, w, decs)
	return decs
}

// seedCandidates adds a shadow version, a trial arm and a research model answering base().
//
//	shadow:trader-v9  (model shadow-m, 0.01 USD each, row 3 estimated): LONG NONE SHORT SHORT LONG SHORT LONG NONE  -> +30 0 +30 +30 0 0 +30 0
//	trial:arm-a       rows 1,2,3,6,7 ok (LONG SHORT SHORT LONG SHORT); 4 failed, 5 refused, 8 absent -> +30 -30 +30 -20 -30
//	research_model:ridge:30m  rows 1-7 (SHORT SHORT NONE SHORT LONG NONE LONG)                        -> -30 -30 0 +30 0 0 +30
func seedCandidates(t *testing.T, w *sql.DB, decs []dec) {
	t.Helper()
	for i, dir := range []string{"LONG", "NONE", "SHORT", "SHORT", "LONG", "SHORT", "LONG", "NONE"} {
		shadow(t, w, decs[i].id, "trader-v9", "shadow-m", dir, 0.01, decs[i].id == 3)
	}
	trial(t, w, 1, "arm-a", "ok", "LONG", "served-1", "reported", 0.02)
	trial(t, w, 2, "arm-a", "ok", "SHORT", "served-1", "reported", 0.02)
	trial(t, w, 3, "arm-a", "ok", "SHORT", "served-1", "list_price_estimate", 0.03)
	trial(t, w, 4, "arm-a", "failed", "", "", "unknown", 0)
	trial(t, w, 5, "arm-a", "refused", "", "served-1", "unknown", 0)
	trial(t, w, 6, "arm-a", "ok", "LONG", "served-1", "unknown", 0)
	trial(t, w, 7, "arm-a", "ok", "SHORT", "served-1", "reported", 0.01)
	run := researchRun(t, w, "ridge-2026-10-05", "ridge", "30m")
	for i, dir := range []string{"SHORT", "SHORT", "NONE", "SHORT", "LONG", "NONE", "LONG"} {
		prediction(t, w, run, decs[i], dir)
	}
}
