// Package actualstest seeds a small synthetic database for the realized-
// performance evaluation tests (issue #777): the repository reader, the
// service and the verified SQL (tools/verify-queries.sql) all check the same
// hand-computed figures. The data is invented; it holds no market data.
package actualstest

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Capital is the initial_capital the expected drawdown ratios assume.
const Capital = 100_000.0

// Trade is one seeded closed position (quantity 100, entry price 1000).
type Trade struct {
	ID       int
	Decision int // jev_decisions.id; 0: a manual entry without signal
	Side     string
	ExitPx   float64
	Fees     float64 // per fill (entry and exit)
	// Net is the realized PnL: (exit-entry)*100 (negated for SHORT) - 2*Fees.
	Net       float64
	ClosedAt  time.Time
	Reason    string
	EntrySlip *float64
	ExitSlip  *float64
}

func at(day, hour, min int) time.Time {
	return time.Date(2026, 10, day, hour, min, 0, 0, time.UTC)
}

// Trades are the closed positions in close order. Hand-computed figures
// (JPY unless noted): nets 580, -1020, 280, -1020, 180, 0 → total -1000,
// 6 trades, 3 wins; fees 100; cumulative 580, -440, -160, -1180, -1000,
// -1000 → max drawdown 1760 (580 → -1180); profit factor 1040/2040; per-trade
// returns (bps, notional 100000) 58, -102, 28, -102, 18, 0.
var Trades = []Trade{
	{ID: 1, Decision: 1, Side: "LONG", ExitPx: 1006, Fees: 10, Net: 580, ClosedAt: at(12, 0, 30), Reason: "take_profit", EntrySlip: new(5.0), ExitSlip: new(3.0)},
	{ID: 2, Decision: 2, Side: "SHORT", ExitPx: 1010, Fees: 10, Net: -1020, ClosedAt: at(12, 1, 30), Reason: "stop_loss", EntrySlip: new(4.0), ExitSlip: new(6.0)},
	{ID: 3, Decision: 3, Side: "LONG", ExitPx: 1003, Fees: 10, Net: 280, ClosedAt: at(13, 0, 50), Reason: "take_profit", EntrySlip: new(2.0), ExitSlip: new(2.0)},
	{ID: 4, Decision: 4, Side: "LONG", ExitPx: 990, Fees: 10, Net: -1020, ClosedAt: at(13, 3, 0), Reason: "stop_loss"},
	{ID: 5, Decision: 5, Side: "LONG", ExitPx: 1002, Fees: 10, Net: 180, ClosedAt: at(14, 0, 50), Reason: "max_holding", EntrySlip: new(1.0), ExitSlip: new(1.0)},
	{ID: 6, Decision: 0, Side: "LONG", ExitPx: 1000, Fees: 0, Net: 0, ClosedAt: at(14, 1, 0), Reason: "manual"},
}

// Decision is one seeded trader decision.
type Decision struct {
	ID         int
	At         time.Time
	Direction  string
	Confidence float64
	ABArm      *string
	DataAgeMs  *int
}

// Decisions are jev_decisions 1..6; 6 has no direction (NONE).
var Decisions = []Decision{
	{ID: 1, At: at(12, 0, 10), Direction: "LONG", Confidence: 0.8, ABArm: new("momentum"), DataAgeMs: new(500)},
	{ID: 2, At: at(12, 1, 10), Direction: "SHORT", Confidence: 0.6, ABArm: new("regime"), DataAgeMs: new(4000)},
	{ID: 3, At: at(13, 0, 20), Direction: "LONG", Confidence: 0.7, ABArm: new("momentum"), DataAgeMs: new(800)},
	{ID: 4, At: at(13, 2, 0), Direction: "LONG", Confidence: 0.9, DataAgeMs: new(12000)},
	{ID: 5, At: at(14, 0, 30), Direction: "LONG", Confidence: 0.5, ABArm: new("momentum")},
	{ID: 6, At: at(14, 2, 0), Direction: "NONE", Confidence: 0.5},
}

// Outcome is one seeded calibration_outcomes row.
type Outcome struct {
	Decision, Horizon, LabelVersion int
	FutureReturn                    float64
	NetBps                          *float64 // label_version 2 only
	Correct                         *bool
}

// Outcomes: label_version 2 grades decisions 1,2,4,5 (hit rate 2/4 = 0.5,
// Brier ((.8-1)²+(.6-0)²+(.9-1)²+(.5-0)²)/4 = 0.165), decision 3 is a tie
// (net 0) and 6 is NONE; label_version 1 grades 1 and 3 (hit 1/2, Brier
// ((.8-1)²+(.7-0)²)/2 = 0.265), decision 2 is a tie (return 0).
var Outcomes = []Outcome{
	{1, 10, 2, 0.12, new(12.0), new(true)},
	{2, 10, 2, -0.05, new(-5.0), new(false)},
	{3, 10, 2, 0, new(0.0), new(false)},
	{4, 10, 2, 0.08, new(8.0), new(true)},
	{5, 10, 2, -0.03, new(-3.0), new(false)},
	{6, 10, 2, 0.01, new(1.0), nil},
	{1, 5, 1, 0.10, nil, new(true)},
	{2, 5, 1, 0, nil, new(false)},
	{3, 5, 1, -0.20, nil, new(false)},
}

// Seed inserts the synthetic rows: two instruments, Decisions, one trade
// signal per decision 1..5, Trades (decision 4 included; trade 6 manual),
// one still-open position on the second instrument, Outcomes, two meta
// models (v-old rejected with an OOS window covering the 12th; v-a active)
// and their predictions (decision 1 was scored by both, v-a being newer).
func Seed(tb testing.TB, db *sql.DB) {
	tb.Helper()
	exec := func(query string, args ...any) {
		tb.Helper()
		if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
			tb.Fatalf("seed %q: %v", query, err)
		}
	}
	ts := sqlutil.FormatTime

	exec(`INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'A', 'TSE'), (2, '6758', 'B', 'TSE')`)
	for _, d := range Decisions {
		exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
			question_version, response_json, direction, confidence, latency_ms, model_id, ab_arm, data_age_ms, created_at)
			VALUES (?, 1, '7203', ?, 'trader', 'h', '{}', 'v1', '{}', ?, ?, 1, 'm', ?, ?, ?)`,
			d.ID, ts(d.At), d.Direction, d.Confidence, d.ABArm, d.DataAgeMs, ts(d.At))
		if d.ID <= 5 {
			exec(`INSERT INTO trade_signals (id, instrument_id, jev_decision_id, symbol, timestamp, direction, policy_version, risk_passed)
				VALUES (?, 1, ?, '7203', ?, ?, 'p1', 1)`, d.ID, d.ID, ts(d.At), d.Direction)
		}
	}
	for _, t := range Trades {
		var signal any
		if t.Decision > 0 {
			signal = t.Decision
		}
		entrySide, exitSide := "BUY", "SELL"
		if t.Side == "SHORT" {
			entrySide, exitSide = "SELL", "BUY"
		}
		opened := t.ClosedAt.Add(-20 * time.Minute)
		exec(`INSERT INTO paper_orders (id, instrument_id, trade_signal_id, symbol, side, order_type, quantity, status,
			submitted_at, filled_at, filled_price, fees, slippage_bps) VALUES (?, 1, ?, '7203', ?, 'MARKET', 100, 'FILLED', ?, ?, 1000, ?, ?)`,
			t.ID*10, signal, entrySide, ts(opened), ts(opened), t.Fees, t.EntrySlip)
		exec(`INSERT INTO paper_orders (id, instrument_id, symbol, side, order_type, quantity, status,
			submitted_at, filled_at, filled_price, fees, slippage_bps) VALUES (?, 1, '7203', ?, 'MARKET', 100, 'FILLED', ?, ?, ?, ?, ?)`,
			t.ID*10+1, exitSide, ts(t.ClosedAt), ts(t.ClosedAt), t.ExitPx, t.Fees, t.ExitSlip)
		exec(`INSERT INTO positions (id, instrument_id, entry_order_id, exit_order_id, symbol, side, quantity, entry_price,
			current_price, realized_pnl, opened_at, closed_at, exit_reason) VALUES (?, 1, ?, ?, '7203', ?, 100, 1000, ?, ?, ?, ?, ?)`,
			t.ID, t.ID*10, t.ID*10+1, t.Side, t.ExitPx, t.Net, ts(opened), ts(t.ClosedAt), t.Reason)
	}
	exec(`INSERT INTO paper_orders (id, instrument_id, symbol, side, order_type, quantity, status, submitted_at, filled_price)
		VALUES (900, 2, '6758', 'BUY', 'MARKET', 100, 'FILLED', ?, 500)`, ts(at(14, 4, 0)))
	exec(`INSERT INTO positions (id, instrument_id, entry_order_id, symbol, side, quantity, entry_price, current_price, opened_at)
		VALUES (900, 2, 900, '6758', 'LONG', 100, 500, 500, ?)`, ts(at(14, 4, 0)))

	for n, o := range Outcomes {
		exec(`INSERT INTO calibration_outcomes (id, jev_decision_id, horizon_minutes, future_return, max_adverse_excursion,
			max_favorable_excursion, was_direction_correct, net_return_bps, label_version) VALUES (?, ?, ?, ?, 0, 0, ?, ?, ?)`,
			n+1, o.Decision, o.Horizon, o.FutureReturn, o.Correct, o.NetBps, o.LabelVersion)
	}

	for _, m := range []struct {
		id                          int
		version, status             string
		oosStart, oosEnd, trainedAt time.Time
		oosSamples                  int
		auc, ev                     *float64
	}{
		{1, "v-old", "rejected", at(12, 0, 0), at(13, 0, 0), at(11, 0, 0), 40, new(0.48), new(-3.0)},
		{2, "v-a", "active", at(10, 0, 0), at(11, 0, 0), at(11, 6, 0), 120, new(0.62), new(7.5)},
	} {
		exec(`INSERT INTO metalabel_models (id, version, algorithm, label_version, status, trained_at, train_start, train_end,
			oos_start, oos_end, train_samples, oos_samples, feature_names_json, params_json, oos_auc, oos_expected_value_bps, created_at)
			VALUES (?, ?, 'logistic', 2, ?, ?, ?, ?, ?, ?, 300, ?, '[]', '{}', ?, ?, ?)`,
			m.id, m.version, m.status, ts(m.trainedAt), ts(m.trainedAt), ts(m.trainedAt), ts(m.oosStart), ts(m.oosEnd),
			m.oosSamples, m.auc, m.ev, ts(m.trainedAt))
	}
	predict := func(id, model, decision int, ev float64) {
		exec(`INSERT INTO metalabel_predictions (id, model_id, jev_decision_id, predicted_at, raw_probability, calibrated_probability,
			expected_value_bps, action, size_fraction, created_at) VALUES (?, ?, ?, ?, 0.6, 0.6, ?, 'take', 1, ?)`,
			id, model, decision, ts(at(12, 0, 0)), ev, ts(at(12, 0, 0)))
	}
	predict(1, 1, 1, 99) // an older model's prediction for decision 1 ...
	predict(2, 2, 1, 40) // ... superseded by v-a's (the newest)
	predict(3, 2, 2, 20)
	predict(4, 2, 3, 30)
	predict(5, 2, 5, 10)
}
