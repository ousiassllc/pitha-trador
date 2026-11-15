package slicetests

import (
	"database/sql"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Scenario price paths, one bar per minute from the decision's bar. With the default label (TP 30,
// SL 30, cost 10 bps, 10 minutes) they give these net returns (bps) for a LONG / SHORT, the same table as
// evaltests/fixture_test.go:
//
//	up    LONG +30  SHORT -30     down  LONG -30  SHORT +30
//	mild  LONG   0  SHORT -20     dip   LONG -20  SHORT   0
func scenarioPrices(name string, n int) []float64 {
	prices := make([]float64, n)
	for i := range prices {
		prices[i] = 1000
	}
	tail := map[string]struct {
		from  int
		price float64
	}{"up": {2, 1005}, "down": {2, 995}, "mild": {1, 1001}, "dip": {1, 999}}[name]
	for i := tail.from; i < n; i++ {
		prices[i] = tail.price
	}
	return prices
}

// spec is one synthetic trader decision with its bars and the attributes the tool cuts by. A nil
// attribute is NULL; regime "" is NULL.
type spec struct {
	id, inst       int
	at             time.Time
	dir, scenario  string
	model, version string
	regime         string
	vol, conf      *float64
	spread, turn   *float64 // spread_bps and turnover_5m of the decision's own bar
	noBars         bool     // no market_snapshots at all: the decision cannot be labeled and is excluded
}

func f(v float64) *float64 { return &v }

// jst is the instant of the JST date and clock time (RFC3339 without zone is read as JST).
func jst(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s+"+09:00")
	if err != nil {
		t.Fatal(err)
	}
	return ts.UTC()
}

// seed writes the specs (decisions and 13 one-minute bars each) in one transaction, and the instruments
// with their sector ("" is NULL). The state and response bodies carry sentinels that must never come back.
func seed(t *testing.T, w *sql.DB, sectors map[int]string, specs []spec) {
	t.Helper()
	tx, err := w.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	// Prepared once per call: a statement per row is what makes a seed slow under -race.
	prepare := func(q string) func(args ...any) {
		st, err := tx.PrepareContext(t.Context(), q)
		if err != nil {
			t.Fatalf("%v: %s", err, q)
		}
		t.Cleanup(func() { _ = st.Close() })
		return func(args ...any) {
			if _, err := st.ExecContext(t.Context(), args...); err != nil {
				t.Fatalf("%v: %s", err, q)
			}
		}
	}
	insertInstrument := prepare(`INSERT INTO instruments (id, symbol, name, market, sector) VALUES (?, ?, 'n', 'TSE', ?)`)
	insertDecision := prepare(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id, created_at, market_regime, regime_volatility_bps)
		VALUES (?, ?, 'S', ?, 'trader', 'h', ?, ?, ?, ?, ?, 5, ?, ?, ?, ?)`)
	insertBar := prepare(`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, spread_bps, volume, turnover, vwap,
		price_vs_vwap_bps, raw_data_json, turnover_5m) VALUES (?, 'S', ?, ?, ?, ?, ?, 1, 1, 1, 0, ?, ?)`)
	for inst, sector := range sectors {
		var s any
		if sector != "" {
			s = sector
		}
		insertInstrument(inst, inst, s)
	}
	for _, d := range specs {
		var regime any
		if d.regime != "" {
			regime = d.regime
		}
		ts := testkit.Stored(t, d.at.Format(time.RFC3339))
		insertDecision(
			d.id, d.inst, ts, `{"k":"`+testkit.SentinelState+`"}`, d.version, `{"k":"`+testkit.SentinelResponse+`"}`, d.dir, d.conf, d.model, ts, regime, d.vol)
		for i, price := range scenarioPrices(d.scenario, 13) {
			if d.noBars {
				break
			}
			var spread, turn any
			if i == 0 {
				if d.spread != nil {
					spread = *d.spread
				}
				if d.turn != nil {
					turn = *d.turn
				}
			}
			insertBar(
				d.inst, testkit.Stored(t, d.at.Add(time.Duration(i)*time.Minute).Format(time.RFC3339)), price, price, price, spread,
				`{"k":"`+testkit.SentinelRaw+`"}`, turn)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
