// Package evalfixture seeds synthetic Jev decisions with their one-minute bars into a
// migrated database, for the tests of the daily evaluation record (issue #1089): the
// recorder, the nightly task and the jev_evaluation_trend tool. Test support only;
// all data is made up.
package evalfixture

import (
	"database/sql"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Scenario price paths, one bar per minute from the decision's bar. With the default
// label (take profit 30, stop loss 30, cost 10 bps, 10 minutes: a take-profit at a
// gross move of +40 bps, a stop-loss at -20 bps) they give these net returns (bps) for
// a LONG / SHORT:
//
//	up    1000, 1000, then 1005   LONG +30 (TP)    SHORT -30 (SL)
//	down  1000, 1000, then 995    LONG -30 (SL)    SHORT +30 (TP)
//	mild  1000, then 1001         LONG   0         SHORT -20
//	dip   1000, then 999          LONG -20         SHORT   0
const (
	Up, Down, Mild, Dip = "up", "down", "mild", "dip"
	bars                = 13
)

// Spec is one synthetic trader decision.
type Spec struct {
	ID        int64
	At        time.Time // the decision's (and its first bar's) instant
	Instr     int       // instrument id (default 1)
	Direction string    // LONG or SHORT
	Model     string
	Version   string
	// Scenario is the price path after the decision ("" stores no bar: the decision cannot be labeled).
	Scenario string
	// R5 is the first bar's return_5m (nil: NULL).
	R5 *float64
	// Passed stores a trade signal that passed Risk.
	Passed bool
}

// Prices is the scenario's price path (bars one-minute bars).
func Prices(scenario string) []float64 {
	prices := make([]float64, bars)
	for i := range prices {
		prices[i] = 1000
	}
	tail := map[string]struct {
		from  int
		price float64
	}{Up: {2, 1005}, Down: {2, 995}, Mild: {1, 1001}, Dip: {1, 999}}[scenario]
	for i := tail.from; i < bars; i++ {
		prices[i] = tail.price
	}
	return prices
}

// Seed inserts the decisions, their bars and signals. The instruments they name are created.
func Seed(tb testing.TB, db *sql.DB, specs ...Spec) {
	tb.Helper()
	exec := func(query string, args ...any) {
		tb.Helper()
		if _, err := db.ExecContext(tb.Context(), query, args...); err != nil {
			tb.Fatalf("seed %.40s: %v", query, err)
		}
	}
	for _, s := range specs {
		instr := max(s.Instr, 1)
		exec(`INSERT OR IGNORE INTO instruments (id, symbol, name, market) VALUES (?, ?, 'n', 'TSE')`, instr, 1000+instr)
		ts := sqlutil.FormatTime(s.At)
		exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
			question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
			VALUES (?, ?, 'S', ?, 'trader', 'h', '{}', ?, '{}', ?, 0.7, 5, ?, ?)`, s.ID, instr, ts, s.Version, s.Direction, s.Model, ts)
		if s.Scenario != "" {
			for i, p := range Prices(s.Scenario) {
				var r5 any
				if i == 0 && s.R5 != nil {
					r5 = *s.R5
				}
				exec(`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, spread_bps, volume, turnover, return_5m, vwap, price_vs_vwap_bps, raw_data_json)
					VALUES (?, 'S', ?, ?, ?, ?, 20, 1, 1, ?, 1, 1, '{}')`, instr, sqlutil.FormatTime(s.At.Add(time.Duration(i)*time.Minute)), p, p, p, r5)
			}
		}
		if s.Passed {
			exec(`INSERT INTO trade_signals (instrument_id, jev_decision_id, symbol, direction, timestamp, policy_version, risk_passed)
				VALUES (?, ?, 'S', ?, ?, 'policy-v2', 1)`, instr, s.ID, s.Direction, ts)
		}
	}
}
