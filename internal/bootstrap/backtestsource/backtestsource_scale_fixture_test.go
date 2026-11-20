package backtestsource

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

// scaleSpec is the size of a stored-history fixture: every TSE trading day of
// the windowDays calendar days from windowStart gets the instruments' 1-minute
// bars of both sessions (330 a day, as the live market-data job stores them),
// a Jev Trader decision every traderEvery bars and a Scout row every
// scoutEvery bars.
type scaleSpec struct {
	instruments int
	windowStart time.Time
	windowDays  int
	traderEvery int
	scoutEvery  int
}

// scaleCounts is what writeScaleFixture stored.
type scaleCounts struct{ snapshots, decisions, tradingDays int }

// barTimes lists the bar timestamps (UTC) of one trading day: 9:00-11:29 and
// 12:30-15:29 JST.
func barTimes(day time.Time) []time.Time {
	var out []time.Time
	for _, session := range [][2]int{{9 * 60, 11*60 + 30}, {12*60 + 30, 15*60 + 30}} {
		for m := session[0]; m < session[1]; m++ {
			out = append(out, time.Date(day.Year(), day.Month(), day.Day(), m/60, m%60, 0, 0, marketcalendar.JST).UTC())
		}
	}
	return out
}

// writeScaleFixture stores the spec's history into env's database: each
// instrument walks a seeded random price (so every run of the fixture is the
// same data), its Feature computed from the featureengine.HistoryLookbackBars
// bars before it exactly as the live job does, and a taking meta model is
// active from before the window. Each instrument-day is one transaction.
func writeScaleFixture(t *testing.T, env testEnv, spec scaleSpec) scaleCounts {
	t.Helper()
	ctx := context.Background()
	storeTakingModel(t, env, spec.windowStart.Add(-time.Hour))
	var days []time.Time
	for d := range spec.windowDays {
		if day := spec.windowStart.AddDate(0, 0, d); marketcalendar.TSE.IsTradingDay(day) {
			days = append(days, day)
		}
	}
	counts := scaleCounts{tradingDays: len(days)}
	response, err := json.Marshal(map[string]any{
		"entry_quality": domain.JevEntryQualityStrong, "continuation_probability": 0.8,
		"toxic_flow": 0.1, "liquidity_stressed": 0.1,
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	for n := range spec.instruments {
		inst := mustCreateInstrument(t, env, fmt.Sprintf("%04d", 1301+n))
		rng := rand.New(rand.NewPCG(uint64(n)+1, 1))
		price := 800 + float64(n*173%4200)
		spread := 8.0 + float64(n%5)*2
		var history []domain.Snapshot // the last HistoryLookbackBars bars, across days
		var returns []float64         // the last 5 bars' returns, for the decision direction
		for _, day := range days {
			var volume int64
			var turnover float64
			var bars []domain.Snapshot
			var decisions []domain.JevDecision
			for i, ts := range barTimes(day) {
				step := 0.0006*rng.NormFloat64() + 0.0004*math.Sin(float64(i)/40+float64(n))
				price = math.Round(price*(1+step)*10) / 10
				returns = append(returns[max(0, len(returns)-4):], step)
				volume += int64(800 + rng.IntN(4000))
				turnover += 5_000_000
				feature := featureengine.Compute(featureengine.Input{
					Timestamp: ts, Current: featureengine.Reading{Price: price, VWAP: price, Volume: volume, Turnover: turnover}, History: history,
				})
				feature.OrderFlowFeatures = policytest.Feature().OrderFlowFeatures
				snap := domain.Snapshot{
					InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts, Price: price,
					SpreadBps: &spread, Volume: volume, Turnover: turnover, Feature: feature,
				}
				bars = append(bars, snap)
				history = append(history[max(0, len(history)-featureengine.HistoryLookbackBars+1):], snap)
				if i%spec.traderEvery == 0 {
					decisions = append(decisions, scaleDecision(inst, ts, domain.JevDecisionTypeTrader, string(response), direction(returns)))
				}
				if i%spec.scoutEvery == 0 {
					decisions = append(decisions, scaleDecision(inst, ts, domain.JevDecisionTypeScout, "{}", domain.JevDirectionNone))
				}
			}
			if _, err := env.Snapshots.InsertBatch(ctx, bars); err != nil {
				t.Fatalf("insert %s bars of %s: %v", inst.Symbol, day.Format(time.DateOnly), err)
			}
			insertDecisions(t, env.Conn, decisions)
			counts.snapshots += len(bars)
			counts.decisions += len(decisions)
		}
	}
	seedPaperSettings(t, env)
	return counts
}

// direction is LONG after a net-positive run of returns and SHORT after a
// negative one, so the stored decisions trade both ways.
func direction(returns []float64) string {
	var sum float64
	for _, r := range returns {
		sum += r
	}
	if sum >= 0 {
		return domain.JevDirectionLong
	}
	return domain.JevDirectionShort
}

func scaleDecision(inst domain.Instrument, ts time.Time, kind, response, dir string) domain.JevDecision {
	confidence := 0.8
	return domain.JevDecision{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts, DecisionType: kind, StateHash: "hash", StateJSON: "{}",
		QuestionVersion: "v1", ResponseJSON: response, Direction: &dir, Confidence: &confidence, ModelID: "test-model",
	}
}

// insertDecisions stores decisions in one transaction (the repository's Insert
// is one autocommit per row, too slow for a month of history).
func insertDecisions(t *testing.T, conn *sql.DB, decisions []domain.JevDecision) {
	t.Helper()
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(context.Background(), `INSERT INTO jev_decisions (
		instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json,
		direction, confidence, latency_ms, model_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, d := range decisions {
		ts := sqlutil.FormatTime(d.Timestamp)
		if _, err := stmt.ExecContext(context.Background(), d.InstrumentID, d.Symbol, ts, d.DecisionType, d.StateHash, d.StateJSON,
			d.QuestionVersion, d.ResponseJSON, *d.Direction, *d.Confidence, d.ModelID, ts); err != nil {
			t.Fatalf("insert decision: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// seedPaperSettings stores what the Settings screen stores for the Paper funds
// (roomy limits: the replay trades ordinary-priced stocks, not the 500,000 yen
// default capital) and one closed Paper position with its orders, so the
// "nothing is written" comparison runs over non-empty tables.
func seedPaperSettings(t *testing.T, env testEnv) {
	t.Helper()
	for key, value := range map[string]string{
		papersettings.KeyInitialCapitalJPY: "30000000", papersettings.KeyMaxPositionPerSymbolPct: "2", papersettings.KeyMaxTotalExposurePct: "20",
	} {
		if err := system.NewRuntimeSettingsRepository(env.Conn).Set(context.Background(), key, value, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}
	inst := mustCreateInstrument(t, env, "9999")
	recordPaperPosition(t, env, inst, time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC), domain.PositionSideLong, domain.ExitReasonManual)
}

// guardedTables are the tables a research replay must leave as they were:
// settings (including the Paper funds), the audit of settings changes, orders,
// positions, order proposals and the virtual Paper tables.
var guardedTables = []string{
	"runtime_settings", "mcp_settings_audit", "paper_orders", "positions", "order_proposals", "order_proposal_events",
	"virtual_paper_profiles", "virtual_paper_days", "virtual_paper_trades", "backtest_runs",
}

// tableState returns each guarded table's row count and a digest of every
// row's content (rows sorted by their text, as some tables have no rowid).
func tableState(t *testing.T, conn *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range guardedTables {
		rows, err := conn.QueryContext(context.Background(), `SELECT * FROM `+table)
		if err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		cols, _ := rows.Columns()
		var lines []string
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatalf("scan %s: %v", table, err)
			}
			lines = append(lines, fmt.Sprint(values))
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate %s: %v", table, err)
		}
		_ = rows.Close()
		slices.Sort(lines)
		sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
		out[table] = fmt.Sprintf("%d rows, sha256 %s", len(lines), hex.EncodeToString(sum[:]))
	}
	return out
}

// maxRequest is the largest request the lab accepts: backtestlab.MaxVariants
// parameter sets (entry, exit and cost overrides) and
// backtestlab.MaxExtraCostPoints extra-cost rows.
func maxRequest(t *testing.T) backtestlab.Request {
	t.Helper()
	variants := []backtestlab.Variant{
		{Label: "baseline"},
		{Label: "sl0.5-tp1", Params: variant.Params{StopLossPct: new(0.5), TakeProfitPct: new(1.0)}},
		{Label: "sl1-tp2", Params: variant.Params{StopLossPct: new(1.0), TakeProfitPct: new(2.0)}},
		{Label: "tp0.3", Params: variant.Params{TakeProfitPct: new(0.3)}},
		{Label: "trail0.5", Params: variant.Params{TrailingStopPct: new(0.5)}},
		{Label: "hold5", Params: variant.Params{MaxHoldingMinutes: new(5)}},
		{Label: "hold30", Params: variant.Params{MaxHoldingMinutes: new(30)}},
		{Label: "ev10", Params: variant.Params{MinEVBps: new(10.0)}},
		{Label: "ev30", Params: variant.Params{MinEVBps: new(30.0)}},
		{Label: "slip5", Params: variant.Params{SlippageBps: new(5.0)}},
		{Label: "fee2", Params: variant.Params{FeeBps: new(2.0)}},
		{Label: "sl2-hold15", Params: variant.Params{StopLossPct: new(2.0), MaxHoldingMinutes: new(15)}},
	}
	req, err := backtestlab.Normalize(variants, []float64{0, 2, 5, 10, 20, 50})
	if err != nil {
		t.Fatalf("Normalize the maximum request: %v", err)
	}
	if len(req.Variants) != backtestlab.MaxVariants || len(req.ExtraCostBps) != backtestlab.MaxExtraCostPoints {
		t.Fatalf("request has %d variants and %d extra costs, want the lab's maximum %d and %d",
			len(req.Variants), len(req.ExtraCostBps), backtestlab.MaxVariants, backtestlab.MaxExtraCostPoints)
	}
	return req
}
