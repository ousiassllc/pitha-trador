package backtest_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

func jstAt(h, m int) time.Time {
	return time.Date(2026, 9, 29, h, m, 0, 0, marketcalendar.JST) // 火曜: 立会日
}

// fillTrades replays one LONG decision at decisionAt over bars at times
// priced by prices (SpreadBps 10), under cost and the TSE sessions (or none).
func fillTrades(t *testing.T, times []time.Time, prices []float64, decisionAt time.Time, cost fillmodel.Model, sessions backtest.Sessions) []backtest.Trade {
	t.Helper()
	return fillTradesCosts(t, times, prices, decisionAt, fillmodel.Uniform(cost), sessions)
}

func fillTradesCosts(t *testing.T, times []time.Time, prices []float64, decisionAt time.Time, cost fillmodel.Costs, sessions backtest.Sessions) []backtest.Trade {
	t.Helper()
	return fillTradesDirection(t, times, prices, decisionAt, domain.JevDirectionLong, cost, sessions)
}

func fillTradesDirection(t *testing.T, times []time.Time, prices []float64, decisionAt time.Time, direction string, cost fillmodel.Costs, sessions backtest.Sessions) []backtest.Trade {
	t.Helper()
	const instrumentID = int64(1)
	bars := make([]domain.Snapshot, 0, len(times))
	for i, ts := range times {
		feature := featureengine.Compute(featureengine.Input{
			Timestamp: ts,
			Current:   featureengine.Reading{Price: prices[i], VWAP: prices[i]},
			History:   bars,
		})
		bars = append(bars, domain.Snapshot{
			InstrumentID: instrumentID, Timestamp: ts, Price: prices[i], SpreadBps: ptr(10.0), Feature: feature,
		})
	}
	thresholds := testThresholds()
	thresholds.Policy.Short = thresholds.Policy.Long
	cfg := backtest.RunConfig{
		InstrumentID: instrumentID,
		Symbol:       "7203",
		Snapshots:    bars,
		Decisions:    backtest.NewSliceDecisionSource([]domain.JevDecision{directionDecision(instrumentID, decisionAt, direction)}),
		Thresholds:   thresholds,
		Exit:         backtest.ExitRule{StopLossPct: 5, TakeProfitPct: 1.0, MaxHolding: 10 * time.Minute},
		Cost:         cost,
		Sessions:     sessions,
	}
	period := backtest.Period{Start: bars[0].Timestamp, End: bars[len(bars)-1].Timestamp.Add(time.Minute)}
	trades, err := backtest.ShadowBacktestTrades(context.Background(), cfg, period)
	if err != nil {
		t.Fatalf("ShadowBacktestTrades: %v", err)
	}
	return trades
}

func minutes(from time.Time, n int) []time.Time {
	out := make([]time.Time, n)
	for i := range out {
		out[i] = from.Add(time.Duration(i) * time.Minute)
	}
	return out
}

// TestReplay_FillsPayTheSpreadAndSlippage: the trade fills at the touch
// (half of SpreadBps 10 = 5bps) plus slippage on the tick grid, not at the
// signal bar's price, and its slippage-inclusive return is below the gross.
func TestReplay_FillsPayTheSpreadAndSlippage(t *testing.T) {
	prices := []float64{2000, 2000, 2030, 2030}
	cost := fillmodel.Model{SlippageBps: 2}
	trades := fillTrades(t, minutes(jstAt(10, 0), 4), prices, jstAt(10, 0), cost, marketcalendar.TSE)
	if len(trades) != 1 {
		t.Fatalf("len(trades) = %d, want 1", len(trades))
	}
	tr := trades[0]
	// buy: 2000 * (1+0.0005) * (1+0.0002) = 2001.4 -> ceil to the 1 yen tick
	if tr.EntryPrice != 2002 {
		t.Errorf("EntryPrice = %v, want 2002 (spread + slippage, rounded up), not the signal price 2000", tr.EntryPrice)
	}
	// sell: 2030 * (1-0.0005) * (1-0.0002) = 2028.0 -> floor
	if tr.ExitPrice >= 2030 {
		t.Errorf("ExitPrice = %v, want below the bar price 2030", tr.ExitPrice)
	}
	if !(tr.SlippageReturnPct < tr.GrossReturnPct) {
		t.Errorf("SlippageReturnPct = %v, want < GrossReturnPct = %v", tr.SlippageReturnPct, tr.GrossReturnPct)
	}
}

// TestReplay_OpeningAuctionPaysNoSpread: a 9:00 寄り fill is a single-price
// 板寄せ - no spread - while the same signal at 10:00 crosses it.
func TestReplay_OpeningAuctionPaysNoSpread(t *testing.T) {
	prices := []float64{2000, 2000, 2030, 2030}
	cost := fillmodel.Model{}

	open := fillTrades(t, minutes(jstAt(9, 0), 4), prices, jstAt(9, 0), cost, marketcalendar.TSE)
	cont := fillTrades(t, minutes(jstAt(10, 0), 4), prices, jstAt(10, 0), cost, marketcalendar.TSE)
	if len(open) != 1 || len(cont) != 1 {
		t.Fatalf("trades: open=%d continuous=%d, want 1 each", len(open), len(cont))
	}
	if open[0].EntryPrice != 2000 {
		t.Errorf("寄り EntryPrice = %v, want 2000 (no spread in the opening auction)", open[0].EntryPrice)
	}
	if cont[0].EntryPrice != 2001 {
		t.Errorf("ザラ場 EntryPrice = %v, want 2001 (5bps half spread rounded up)", cont[0].EntryPrice)
	}
}

// TestReplay_ClosingAuctionUsesAuctionSlippage: a bar in the 15:25-15:30
// クロージング・オークション exits with the auction slippage, not SlippageBps.
func TestReplay_ClosingAuctionUsesAuctionSlippage(t *testing.T) {
	prices := []float64{2000, 2030, 2030, 2030}
	cost := fillmodel.Model{SlippageBps: 0, OpeningSlippageBps: 100, ClosingSlippageBps: 100} // 1% = 20 yen
	// entry 15:24 (ザラ場), TP on the 15:25 bar (引けの板寄せ)
	trades := fillTrades(t, minutes(jstAt(15, 24), 4), prices, jstAt(15, 24), cost, marketcalendar.TSE)
	if len(trades) != 1 {
		t.Fatalf("len(trades) = %d, want 1", len(trades))
	}
	if got := trades[0].ExitPrice; got != 2009 { // 2030 * 0.99 = 2009.7 -> floor
		t.Errorf("引け ExitPrice = %v, want 2009 (auction slippage applied to the indicative price)", got)
	}
}

// TestReplay_LunchBreakNeverFills: nothing fills 11:30-12:30. An entry
// signal on a lunch-break bar is not a trade, and a take-profit reached on
// a lunch-break bar is not taken (the position holds to the last bar that
// could fill, MaxHolding here).
func TestReplay_LunchBreakNeverFills(t *testing.T) {
	cost := fillmodel.Model{}

	t.Run("entry during lunch", func(t *testing.T) {
		prices := []float64{2000, 2000, 2030, 2030}
		times := minutes(jstAt(11, 45), 4)
		if got := fillTrades(t, times, prices, times[0], cost, marketcalendar.TSE); len(got) != 0 {
			t.Fatalf("len(trades) = %d, want 0: %+v", len(got), got)
		}
		if got := fillTrades(t, times, prices, times[0], cost, nil); len(got) != 1 {
			t.Fatalf("without sessions len(trades) = %d, want 1 (control)", len(got))
		}
	})

	t.Run("take profit during lunch", func(t *testing.T) {
		times := []time.Time{jstAt(11, 28), jstAt(11, 29), jstAt(11, 30), jstAt(11, 31), jstAt(12, 30)}
		prices := []float64{2000, 2000, 2060, 2060, 2060}
		got := fillTrades(t, times, prices, times[0], cost, marketcalendar.TSE)
		if len(got) != 1 {
			t.Fatalf("len(trades) = %d, want 1", len(got))
		}
		if got[0].ExitReason != backtest.ExitReasonMaxHolding || !got[0].ExitTimestamp.Equal(jstAt(11, 29)) {
			t.Errorf("trade exit = %q at %v, want max_holding at 11:29 (the lunch-break bar cannot fill)", got[0].ExitReason, got[0].ExitTimestamp)
		}
		control := fillTrades(t, times, prices, times[0], cost, nil)
		if len(control) != 1 || control[0].ExitReason != backtest.ExitReasonTakeProfit {
			t.Fatalf("without sessions: %+v, want a take_profit exit (control)", control)
		}
	})
}

// TestReplay_NetReturnFollowsTheCostSettings: the backtest prices its fills
// with the cost settings' Costs, so raising the commission and the slippage
// lowers a trade's net return by their round trip (#779).
func TestReplay_NetReturnFollowsTheCostSettings(t *testing.T) {
	prices := []float64{2000, 2000, 2030, 2030}
	run := func(s costsettings.Settings) backtest.Trade {
		t.Helper()
		trades := fillTradesCosts(t, minutes(jstAt(10, 0), 4), prices, jstAt(10, 0), fillmodel.FromSettings(s), marketcalendar.TSE)
		if len(trades) != 1 {
			t.Fatalf("len(trades) = %d, want 1", len(trades))
		}
		return trades[0]
	}
	base := run(costsettings.Default())
	costly := costsettings.Default()
	costly.FeeBps = 5
	costly.Long.ContinuousBps = 20
	got := run(costly)

	if !(got.NetReturnPct < base.NetReturnPct) {
		t.Fatalf("NetReturnPct = %v with the costlier settings, want below %v", got.NetReturnPct, base.NetReturnPct)
	}
	if want := base.FeeReturnPct - 0.10; math.Abs(got.FeeReturnPct-want) > 1e-9 { // 5 bps on both fills = 0.10%
		t.Errorf("FeeReturnPct = %v, want %v", got.FeeReturnPct, want)
	}
	if !(got.SlippageReturnPct < base.SlippageReturnPct) {
		t.Errorf("SlippageReturnPct = %v, want below %v (slippage 20 bps vs 2)", got.SlippageReturnPct, base.SlippageReturnPct)
	}
}

// TestReplay_ShortBorrowFeeIsInTheFeeReturn: a SHORT trade pays the 貸株料, a
// LONG one does not.
func TestReplay_ShortBorrowFeeIsInTheFeeReturn(t *testing.T) {
	prices := []float64{2000, 2000, 1970, 1970}
	s := costsettings.Default()
	s.ShortBorrowBps = 7
	shortTrades := fillTradesDirection(t, minutes(jstAt(10, 0), 4), prices, jstAt(10, 0), domain.JevDirectionShort, fillmodel.FromSettings(s), marketcalendar.TSE)
	if len(shortTrades) != 1 {
		t.Fatalf("len(shortTrades) = %d, want 1", len(shortTrades))
	}
	if got, want := shortTrades[0].GrossReturnPct-shortTrades[0].FeeReturnPct, 0.07; math.Abs(got-want) > 1e-9 {
		t.Errorf("fee deduction of the SHORT trade = %v%%, want %v%% (7 bps 貸株料, no commission)", got, want)
	}
	longTrades := fillTradesDirection(t, minutes(jstAt(10, 0), 4), []float64{2000, 2000, 2030, 2030}, jstAt(10, 0), domain.JevDirectionLong, fillmodel.FromSettings(s), marketcalendar.TSE)
	if len(longTrades) != 1 || longTrades[0].FeeReturnPct != longTrades[0].GrossReturnPct {
		t.Errorf("LONG trade paid a fee with only a 貸株料 set: %+v", longTrades)
	}
}

func directionDecision(instrumentID int64, ts time.Time, direction string) domain.JevDecision {
	d := longDecision(instrumentID, ts)
	d.Direction = ptr(direction)
	return d
}
