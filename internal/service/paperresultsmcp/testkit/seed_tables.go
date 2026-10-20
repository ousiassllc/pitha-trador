package testkit

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func Decision(t *testing.T, w *sql.DB, id int, kind string, conf any, ts string) {
	t.Helper()
	MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash,
		state_json, question_version, response_json, direction, confidence, latency_ms, model_id, ofi_short)
		VALUES (?, 1, '7203', ?, ?, 'h', ?, 'q1', ?, 'LONG', ?, 5, 'm', 0.123456)`,
		id, Stored(t, ts), kind, SentinelState, SentinelResponse, conf)
}

func Outcome(t *testing.T, w *sql.DB, decisionID, horizon, version int, correct, barrier, net any) {
	t.Helper()
	MustExec(t, w, `INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return,
		max_adverse_excursion, max_favorable_excursion, was_direction_correct, barrier_hit, net_return_bps,
		entry_ref_price, label_version) VALUES (?, ?, 0.7777, 0.8888, 0.9999, ?, ?, ?, 555.55, ?)`,
		decisionID, horizon, correct, barrier, net, version)
}

// SeedJev: 1 scout + 5 trader decisions in the period (D7 outside it).
// horizon 5 / label 2 and horizon 10 / label 1 outcomes; see jev_test.go.
func SeedJev(t *testing.T, w *sql.DB) {
	t.Helper()
	Decision(t, w, 1, "trader", 0.55, "2026-10-01T01:00:00Z")
	Decision(t, w, 2, "trader", 0.65, "2026-10-01T02:00:00Z")
	Decision(t, w, 3, "trader", 0.95, "2026-10-01T03:00:00Z")
	Decision(t, w, 4, "trader", nil, "2026-10-01T04:00:00Z")
	Decision(t, w, 5, "scout", 0.7, "2026-10-01T05:00:00Z")
	Decision(t, w, 6, "trader", 0.30, "2026-10-01T06:00:00Z")
	Decision(t, w, 7, "trader", 0.99, "2026-08-01T00:00:00Z")
	Outcome(t, w, 1, 10, 1, 1, nil, nil)
	Outcome(t, w, 2, 10, 1, 0, nil, nil)
	Outcome(t, w, 3, 10, 1, 1, nil, nil)
	Outcome(t, w, 4, 10, 1, 1, nil, nil)
	Outcome(t, w, 6, 10, 1, 0, nil, nil)
	Outcome(t, w, 7, 10, 1, 1, nil, nil)
	Outcome(t, w, 1, 5, 2, 1, "tp", 12.0)
	Outcome(t, w, 2, 5, 2, 0, "sl", -8.0)
	Outcome(t, w, 3, 5, 2, nil, "timeout", 0.0)
	for i, ts := range []string{"2026-10-02T00:00:00Z", "2026-10-03T00:00:00Z", "2026-08-02T00:00:00Z"} {
		MustExec(t, w, `INSERT INTO calibration_label_skips (jev_decision_id, horizon_minutes, reason, created_at)
			VALUES (?, 10, ?, ?)`, i+1, SentinelSkip, Stored(t, ts))
	}
}

// SeedMeta: models M1 (active, 10-02), M2 (rejected, 10-05, 250-rune reason)
// and M3 (outside); 3 take + 2 skip predictions in the period, 1 outside;
// retrain skips on 10-03 (87 of 130) and 10-04 (an unknown code), 1 outside.
func SeedMeta(t *testing.T, w *sql.DB) {
	t.Helper()
	model := func(id int, version, status, reason, trained string, auc, ev any) {
		MustExec(t, w, `INSERT INTO metalabel_models (id, version, algorithm, label_version, status, reject_reason,
			trained_at, train_start, train_end, oos_start, oos_end, train_samples, oos_samples,
			feature_names_json, params_json, oos_auc, oos_expected_value_bps, oos_calibration_json, created_at)
			VALUES (?, ?, 'logreg', 2, ?, ?, ?, ?, ?, ?, ?, 400, 100, ?, ?, ?, ?, ?, ?)`,
			id, version, status, reason, Stored(t, trained), Stored(t, "2026-09-01T00:00:00Z"),
			Stored(t, "2026-09-20T00:00:00Z"), Stored(t, "2026-09-20T00:00:00Z"), Stored(t, "2026-09-30T00:00:00Z"),
			SentinelParams, SentinelParams, auc, ev, SentinelParams, Stored(t, trained))
	}
	model(1, "v1", "active", "", "2026-10-02T00:00:00Z", 0.61, 3.5)
	model(2, "v2", "rejected", strings.Repeat("あ", 250), "2026-10-05T00:00:00Z", nil, nil)
	model(3, "v0", "active", "", "2026-08-01T00:00:00Z", 0.7, 1.0)
	for i, p := range []struct{ at, action string }{
		{"2026-10-02T00:00:00Z", "take"}, {"2026-10-02T01:00:00Z", "take"}, {"2026-10-02T02:00:00Z", "take"},
		{"2026-10-02T03:00:00Z", "skip"}, {"2026-10-02T04:00:00Z", "skip"}, {"2026-08-02T00:00:00Z", "take"},
	} {
		MustExec(t, w, `INSERT INTO metalabel_predictions (model_id, jev_decision_id, predicted_at, raw_probability,
			calibrated_probability, expected_value_bps, action, size_fraction, features_json, created_at)
			VALUES (1, ?, ?, 0.5, 0.5, 1.0, ?, 0.5, ?, ?)`, i+1, Stored(t, p.at), p.action, SentinelFeatures, Stored(t, p.at))
	}
	for _, k := range []struct {
		at, code     string
		usable, need int
	}{
		{"2026-10-03T18:00:00Z", "too_few_rows_before_oos", 87, 130},
		{"2026-10-04T18:00:00Z", SentinelSkipCode, 12, 130},
		{"2026-08-03T18:00:00Z", "too_few_rows_before_oos", 5, 130},
	} {
		MustExec(t, w, `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, created_at)
			VALUES (?, ?, ?, ?, ?)`, Stored(t, k.at), k.code, k.usable, k.need, Stored(t, k.at))
	}
}

// SeedRisk: 2 passed, 10 other rejects (policy_below_threshold and a NULL
// reason, plus jev_none x2, meta_skip:no_active_model x2 and
// meta_skip:ev_not_above_minimum, entry_quality_below_threshold,
// meta_model_unavailable and a meta_skip with an unknown reason), daily_loss_limit
// x2, max_daily_trades x1 and unknown x2 Risk Engine rejects in the period;
// 1 more of each kind outside.
func SeedRisk(t *testing.T, w *sql.DB) {
	t.Helper()
	signals := []struct {
		at     string
		passed int
		reason any
	}{
		{"2026-10-01T00:00:00Z", 1, nil},
		{"2026-10-01T00:01:00Z", 1, nil},
		{"2026-10-01T00:02:00Z", 0, "risk_engine_rejected:daily_loss_limit:count=3 loss=-12345"},
		{"2026-10-01T00:03:00Z", 0, "risk_engine_rejected:daily_loss_limit"},
		{"2026-10-01T00:04:00Z", 0, "risk_engine_rejected:max_daily_trades: extra detail"},
		{"2026-10-01T00:05:00Z", 0, "risk_engine_rejected:"},
		{"2026-10-01T00:06:00Z", 0, "risk_engine_rejected:  : detail"},
		{"2026-10-01T00:07:00Z", 0, "policy_below_threshold"},
		{"2026-10-01T00:08:00Z", 0, nil},
		{"2026-10-01T00:09:00Z", 0, "jev_none"},
		{"2026-10-01T00:10:00Z", 0, "jev_none"},
		{"2026-10-01T00:11:00Z", 0, "meta_skip: no_active_model (p=0.0000 ev_bps=0.00)"},
		{"2026-10-01T00:12:00Z", 0, "meta_skip: no_active_model (p=0.5123 ev_bps=-3.21)"},
		{"2026-10-01T00:13:00Z", 0, "meta_skip: ev_not_above_minimum (p=0.5500 ev_bps=1.00)"},
		{"2026-10-01T00:14:00Z", 0, "meta_skip: " + SentinelSkip + " (p=0.1 ev_bps=0.1)"},
		{"2026-10-01T00:15:00Z", 0, "entry_quality_below_threshold: entry_quality=\"poor\" < min=\"good\""},
		{"2026-10-01T00:16:00Z", 0, "meta_model_unavailable: model: " + SentinelDetail},
		{"2026-08-01T00:00:00Z", 0, "risk_engine_rejected:profit_lock"},
		{"2026-08-01T00:00:01Z", 0, "jev_none"},
	}
	for _, s := range signals {
		MustExec(t, w, `INSERT INTO trade_signals (instrument_id, symbol, timestamp, direction, policy_version,
			risk_passed, reject_reason) VALUES (1, '7203', ?, 'LONG', 'p1', ?, ?)`, Stored(t, s.at), s.passed, s.reason)
	}
}

// SeedKill: E1 (10-02, auto-resolved) and E2 (10-05, unresolved).
func SeedKill(t *testing.T, w *sql.DB) {
	t.Helper()
	MustExec(t, w, `INSERT INTO kill_switch_events (id, triggered_at, reason, detail_json) VALUES
		(1, ?, 'daily_loss_limit', ?), (2, ?, 'operator_manual', ?)`,
		Stored(t, "2026-10-02T03:00:00Z"), SentinelDetail, Stored(t, "2026-10-05T03:00:00Z"), SentinelDetail)
	MustExec(t, w, `INSERT INTO kill_switch_resolutions (kill_switch_event_id, resolved_at, resolved_by)
		VALUES (1, ?, 'auto')`, Stored(t, "2026-10-02T04:00:00Z"))
}

// SeedCollection: snapshots on JST 10-01, 10-02, 10-05, 10-06 (in 10-01..10-10)
// plus one later snapshot; jobs, daily bar runs and secrets / settings rows.
func SeedCollection(t *testing.T, w *sql.DB) {
	t.Helper()
	for _, ts := range []string{"2026-10-01T01:00:00Z", "2026-10-02T01:00:00Z", "2026-10-05T01:00:00Z",
		"2026-10-06T01:00:00Z", "2026-10-16T06:00:00Z"} {
		MustExec(t, w, `INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, volume,
			turnover, vwap, price_vs_vwap_bps, raw_data_json) VALUES (1, '7203', ?, ?, 98765.4321, 98765.4321, 100,
			1.0, 1.0, 1.0, ?)`, Stored(t, ts), SentinelPriceCol, SentinelRaw)
	}
	jobWithError := func(queue, status, finished, lastError string) {
		MustExec(t, w, `INSERT INTO jobs (queue, payload_json, status, scheduled_at, finished_at, last_error)
			VALUES (?, ?, ?, ?, ?, ?)`, queue, SentinelPayload, status, Stored(t, "2026-10-01T00:00:00Z"),
			Stored(t, finished), lastError)
	}
	job := func(queue, status, finished string) { jobWithError(queue, status, finished, SentinelLastError) }
	job("market-data", "succeeded", "2026-10-02T00:00:00Z")
	job("market-data", "succeeded", "2026-10-03T00:00:00Z")
	job("market-data", "failed", "2026-10-03T01:00:00Z") // no code token: "unknown"
	job("market-data", "succeeded", "2026-09-01T00:00:00Z")
	job("jev-trader", "succeeded", "2026-10-17T00:00:00Z")
	// Failed jobs with a broker code (JST 10-06 and 10-07), one outside the
	// period; a code is parsed from the error, the rest of the text is a sentinel.
	const kabuErr = "marketdata: kabu station api error (http 400, code %s): " + SentinelLastError
	jobWithError("market-data", "failed", "2026-10-05T16:00:00Z", fmt.Sprintf(kabuErr, "4002006"))
	jobWithError("market-data", "failed", "2026-10-06T02:00:00Z", fmt.Sprintf(kabuErr, "4002006"))
	jobWithError("jev-scout", "failed", "2026-10-06T03:00:00Z", fmt.Sprintf(kabuErr, "4001007"))
	jobWithError("jev-scout", "failed", "2026-10-06T15:30:00Z", "tachibana: p_errno=-62: "+SentinelLastError)
	jobWithError("market-data", "failed", "2026-09-01T00:00:00Z", fmt.Sprintf(kabuErr, "4001007"))
	// Terminal skips (succeeded with a "skipped:" note) and a plain success note.
	jobWithError("outcome-labeling", "succeeded", "2026-10-04T00:00:00Z", "skipped: "+SentinelLastError)
	jobWithError("outcome-labeling", "succeeded", "2026-10-04T01:00:00Z", "skipped: "+SentinelLastError)
	jobWithError("analytics", "succeeded", "2026-10-04T02:00:00Z", "deferred: "+SentinelLastError)
	for _, r := range []struct{ date, status string }{
		{"2026-10-02", "failed"}, {"2026-10-03", "succeeded"}, {"2026-10-04", "failed"}, {"2026-09-01", "failed"},
	} {
		MustExec(t, w, `INSERT INTO daily_bar_runs (run_date, status, started_at, error) VALUES (?, ?, ?, ?)`,
			r.date, r.status, Stored(t, "2026-10-04T00:00:00Z"), SentinelBarError)
	}
	MustExec(t, w, `INSERT INTO secrets (key, encrypted_value, updated_at) VALUES (?, ?, ?)`,
		SentinelSecretKey, SentinelSecret, Stored(t, "2026-10-01T00:00:00Z"))
	MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('k', ?, ?)`,
		SentinelRuntime, Stored(t, "2026-10-01T00:00:00Z"))
}

// SeedAll fills every table the tools read.
func SeedAll(t *testing.T, w *sql.DB) {
	t.Helper()
	SeedPositions(t, w)
	SeedJev(t, w)
	SeedMeta(t, w)
	SeedRisk(t, w)
	SeedKill(t, w)
	SeedCollection(t, w)
}
