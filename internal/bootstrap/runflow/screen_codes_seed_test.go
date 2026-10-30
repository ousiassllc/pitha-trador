package runflow_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/labels"
)

// seedReferenceHour is the JST hour of the reference instant the seeded rows
// are placed relative to: stamp(d), the function seedEveryCode takes from
// stamper, is d before JST today at this hour. The Home screen counts the
// signals and trades of [00:00, 24:00) JST as "today", so those rows must stay
// in that window however early or late the test runs; with "d before
// time.Now()" the NONE signals (30 minutes and more old) fell into yesterday
// between 00:00 and about 01:00 JST (issue #919). Rows of today are therefore
// at most seedTodayMaxAge (< seedReferenceHour hours) old. Rows may lie after
// the wall clock (before 06:00 JST): only the "today" window is stabilised.
const (
	seedReferenceHour = 6
	seedTodayMaxAge   = 5 * time.Hour
	// seedStalledAge puts the updated_at of the proposals before JST today
	// whatever the time, so those still in received/processing always count as
	// stalled (updated_at older than 10 minutes) and the Home screen always
	// renders that item.
	seedStalledAge = 30 * time.Hour
)

// jst is Japan Standard Time (UTC+9, no daylight saving time).
var jst = time.FixedZone("JST", 9*60*60)

// stamper returns stamp: a stored timestamp (RFC 3339 with milliseconds, UTC)
// of d before the reference instant, JST today at seedReferenceHour. The day
// is taken from now once, so the seed never straddles midnight (the screens
// read their own clock afterwards; the gap is milliseconds).
func stamper(now time.Time) func(d time.Duration) string {
	j := now.In(jst)
	ref := time.Date(j.Year(), j.Month(), j.Day(), seedReferenceHour, 0, 0, 0, jst)
	return func(d time.Duration) string {
		return ref.Add(-d).UTC().Format("2006-01-02T15:04:05.000Z")
	}
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	res, err := db.Exec(query, args...)
	if err != nil {
		t.Fatalf("seed: %v\n%s", err, query)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedSecrets stores a dummy value for every required secret, so the Setup
// Guard lets every screen through.
func seedSecrets(t *testing.T, state *bootstrap.State) {
	t.Helper()
	secretsRepo, _, err := bootstrap.LoadSecrets(context.Background(), state)
	if err != nil {
		t.Fatalf("LoadSecrets: %v", err)
	}
	for _, key := range config.RequiredSecretKeys() {
		if err := secretsRepo.Set(context.Background(), key, "dummy-value"); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}
}

// seedEveryCode fills the database with one row for every code the screens
// name: each exit reason, Kill Switch reason, reject reason, order proposal
// reason, retraining skip and job queue, so a rendered screen has the chance to
// print them. Codes the tables do not name are added too (they must not show).
func seedEveryCode(t *testing.T, db *sql.DB) {
	t.Helper()
	stamp := stamper(time.Now())
	long := mustExec(t, db, `INSERT INTO instruments (symbol, name, market, kind, asset_class) VALUES ('7203', 'トヨタ自動車', 'プライム', 'stock', 'stock')`)
	etf := mustExec(t, db, `INSERT INTO instruments (symbol, name, market, kind, asset_class) VALUES ('1306', 'TOPIX連動ETF', 'プライム', 'stock', 'bond_etf')`)
	response, _ := json.Marshal(map[string]any{"direction": "LONG", "regime": "TREND", "entry_quality": "good"})
	decision := mustExec(t, db, `INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id, market_regime, ab_arm, vwap_z, data_age_ms)
		VALUES (?, '7203', ?, 'trader', 'h', '{}', 'q1', ?, 'LONG', 0.7, 100, 'm', 'trend', 'momentum', 1.2, 500)`, long, stamp(10*time.Minute), string(response))

	// reject reasons of today's NONE signals: every named head and an unknown one
	heads := append(mapKeys(labels.RejectReasons), "an_unnamed_head")
	for i, head := range heads {
		mustExec(t, db, `INSERT INTO trade_signals (instrument_id, jev_decision_id, symbol, timestamp, direction, policy_version, risk_passed, reject_reason)
			VALUES (?, ?, '7203', ?, 'NONE', 'p1', 0, ?)`, long, decision, stamp(time.Duration(30+i)*time.Minute), head)
	}
	signal := mustExec(t, db, `INSERT INTO trade_signals (instrument_id, jev_decision_id, symbol, timestamp, direction, score, entry_price_reference, policy_version, risk_passed)
		VALUES (?, ?, '7203', ?, 'LONG', 0.8, 2500, 'p1', 1)`, long, decision, stamp(2*time.Hour))

	// closed positions: every exit reason and an unnamed one, today
	for i, reason := range append(mapKeys(labels.ExitReasons), "an_unnamed_exit") {
		entry := mustExec(t, db, `INSERT INTO paper_orders (instrument_id, trade_signal_id, symbol, side, order_type, quantity, status, submitted_at, filled_at, filled_price, fees)
			VALUES (?, ?, '7203', 'BUY', 'MARKET', 100, 'FILLED', ?, ?, 2500, 55)`, long, signal, stamp(seedTodayMaxAge), stamp(seedTodayMaxAge))
		exit := mustExec(t, db, `INSERT INTO paper_orders (instrument_id, symbol, side, order_type, quantity, status, submitted_at, filled_at, filled_price, fees)
			VALUES (?, '7203', 'SELL', 'MARKET', 100, 'FILLED', ?, ?, 2510, 55)`, long, stamp(time.Hour), stamp(time.Hour))
		mustExec(t, db, `INSERT INTO positions (instrument_id, entry_order_id, exit_order_id, symbol, side, quantity, entry_price, current_price, unrealized_pnl, realized_pnl, opened_at, closed_at, exit_reason)
			VALUES (?, ?, ?, '7203', 'LONG', 100, 2500, 2510, 0, ?, ?, ?, ?)`, long, entry, exit, 1000+100*i, stamp(seedTodayMaxAge), stamp(time.Hour), reason)
	}
	open := mustExec(t, db, `INSERT INTO paper_orders (instrument_id, trade_signal_id, symbol, side, order_type, quantity, status, submitted_at, filled_at, filled_price, fees)
		VALUES (?, ?, '7203', 'BUY', 'MARKET', 100, 'FILLED', ?, ?, 2500, 55)`, long, signal, stamp(time.Hour), stamp(time.Hour))
	mustExec(t, db, `INSERT INTO positions (instrument_id, entry_order_id, symbol, side, quantity, entry_price, current_price, unrealized_pnl, opened_at)
		VALUES (?, ?, '7203', 'LONG', 100, 2500, 2520, 2000, ?)`, long, open, stamp(time.Hour))

	// Kill Switch events: every reason, lifted by each party
	for i, reason := range mapKeys(labels.KillReasons) {
		id := mustExec(t, db, `INSERT INTO kill_switch_events (triggered_at, reason, detail_json) VALUES (?, ?, '{}')`, stamp(time.Duration(i+1)*time.Minute), reason)
		by := []string{"auto", "manual"}[i%2]
		if i < 9 {
			mustExec(t, db, `INSERT INTO kill_switch_resolutions (kill_switch_event_id, resolved_at, resolved_by) VALUES (?, ?, ?)`, id, stamp(0), by)
		}
	}

	// order proposals: one per status/reason code, with an event history
	statuses := []string{"received", "processing", "waiting_session", "rejected", "submitted", "filled", "expired", "cancelled", "closed", "failed"}
	reasons := append(mapKeys(labels.ProposalReasons), "an_unnamed_reason")
	for i, reason := range reasons {
		status := statuses[i%len(statuses)]
		bucket, instrument, action := "stock", long, "open_long"
		if i%3 == 1 {
			bucket, instrument = "bond_etf", etf
		}
		if i%4 == 3 {
			action = "close"
		}
		id := mustExec(t, db, `INSERT INTO order_proposals (idempotency_key, request_hash, received_at, client, symbol, bucket, action, quantity, order_type,
			stop_loss_price, exit_plan, max_loss_jpy, rationale, sources_json, expires_at, status, status_reason, instrument_id, updated_at,
			applied_quantity, computed_max_loss_jpy, effective_stop_loss_price)
			VALUES (?, 'h', ?, 'client', '7203', ?, ?, 100, 'MARKET', 2450, '2650円で利確', 5000, 'テスト', '[]', ?, ?, ?, ?, ?, 100, 4800, 2450)`,
			fmt.Sprintf("key-%d", i), stamp(3*time.Hour), bucket, action, stamp(-time.Hour), status, reason, instrument, stamp(seedStalledAge))
		// the trade signal the proposal was judged as, rejected by the Risk Engine with the code
		signal := mustExec(t, db, `INSERT INTO trade_signals (instrument_id, symbol, timestamp, direction, policy_version, risk_passed, reject_reason)
			VALUES (?, '7203', ?, 'LONG', 'mcp-proposal-v1', 0, ?)`, instrument, stamp(2*time.Hour), reason)
		mustExec(t, db, `UPDATE order_proposals SET trade_signal_id = ? WHERE id = ?`, signal, id)
		mustExec(t, db, `INSERT INTO order_proposal_events (proposal_id, at, from_status, to_status, reason_code, actor, detail_json) VALUES (?, ?, NULL, 'received', '', 'mcp', '')`, id, stamp(3*time.Hour))
		mustExec(t, db, `INSERT INTO order_proposal_events (proposal_id, at, from_status, to_status, reason_code, actor, detail_json) VALUES (?, ?, 'received', ?, ?, ?, ?)`,
			id, stamp(2*time.Hour), status, reason, []string{"app", "gui"}[i%2], `{"requested_quantity":300,"applied_quantity":100,"limited_by":"position_size","mystery":1}`)
	}

	// meta-labeling model and the retraining skips (named and unnamed)
	mustExec(t, db, `INSERT INTO metalabel_models (version, algorithm, label_version, status, reject_reason, trained_at, train_start, train_end, oos_start, oos_end,
		train_samples, oos_samples, feature_names_json, params_json, oos_auc, oos_expected_value_bps, oos_calibration_json, created_at)
		VALUES ('v1', 'logistic_regression', 2, 'active', '', ?, ?, ?, ?, ?, 400, 100, '[]', '{}', 0.61, 3.5, '[]', ?)`,
		stamp(time.Hour), stamp(500*time.Hour), stamp(100*time.Hour), stamp(100*time.Hour), stamp(time.Hour), stamp(time.Hour))
	for _, code := range append(mapKeys(labels.RetrainSkips), "an_unnamed_skip") {
		mustExec(t, db, `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, created_at) VALUES (?, ?, 80, 130, ?)`, stamp(time.Minute), code, stamp(time.Minute))
	}

	// jobs of every queue
	for _, queue := range mapKeys(labels.JobQueues) {
		mustExec(t, db, `INSERT INTO jobs (queue, payload_json, status, scheduled_at, started_at, finished_at, last_error) VALUES (?, '{}', 'failed', ?, ?, ?, 'boom')`, queue, stamp(time.Hour), stamp(time.Hour), stamp(59*time.Minute))
	}

	// an experiment on a setting
	mustExec(t, db, `INSERT INTO tuning_experiments (created_at, actor, client, hypothesis, variable_kind, variable_name, before_value, after_value, started_at, status)
		VALUES (?, 'mcp', 'client', 'しきい値を下げると見送りが減る', 'setting', 'policy.long.min_probability', '0.7', '0.6', ?, 'running')`, stamp(48*time.Hour), stamp(48*time.Hour))
}

// mapKeys is the keys of m, in a fixed order.
func mapKeys(m map[string]string) []string { return slices.Sorted(maps.Keys(m)) }
