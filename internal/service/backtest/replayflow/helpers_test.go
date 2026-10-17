package replayflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

var base = time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)

// minuteBars builds one look-ahead-safe bar per minute from start, priced by
// prices; vwaps (nil: VWAP = price) are the bars' VWAPs.
func minuteBars(instrumentID int64, start time.Time, prices, vwaps []float64) []domain.Snapshot {
	bars := make([]domain.Snapshot, 0, len(prices))
	for i, price := range prices {
		vwap := price
		if vwaps != nil {
			vwap = vwaps[i]
		}
		ts := start.Add(time.Duration(i) * time.Minute)
		feature := featureengine.Compute(featureengine.Input{
			Timestamp: ts,
			Current:   featureengine.Reading{Price: price, VWAP: vwap},
			History:   bars,
		})
		bars = append(bars, domain.Snapshot{
			InstrumentID: instrumentID, Timestamp: ts, Price: price, SpreadBps: ptr(10.0), Feature: flowFeature(feature),
		})
	}
	return bars
}

// at is the bar time of minute i from base.
func at(i int) time.Time { return base.Add(time.Duration(i) * time.Minute) }

// replayConfig is a default-rule (live meta-labeling rule, taking model) config
// over bars with decisions, exiting by exit.
func replayConfig(instrumentID int64, symbol string, bars []domain.Snapshot, decisions []domain.JevDecision, exit backtest.ExitRule) backtest.RunConfig {
	cfg := shortThresholds()
	cfg.InstrumentID = instrumentID
	cfg.Symbol = symbol
	cfg.Snapshots = bars
	cfg.Decisions = backtest.NewSliceDecisionSource(decisions)
	cfg.Models = takingModels()
	cfg.Exit = exit
	return cfg
}

func replayTrades(t *testing.T, cfgs ...backtest.RunConfig) []backtest.Trade {
	t.Helper()
	first, last := cfgs[0].Snapshots[0].Timestamp, cfgs[0].Snapshots[len(cfgs[0].Snapshots)-1].Timestamp
	trades, err := backtest.ShadowBacktestPortfolioTrades(context.Background(), cfgs, backtest.Period{Start: first, End: last.Add(time.Minute)})
	if err != nil {
		t.Fatalf("ShadowBacktestPortfolioTrades: %v", err)
	}
	return trades
}

// maxConcurrent is the largest number of trades open at the same time (a
// position is open from its entry bar up to, not including, its exit bar's
// close: one exiting on the bar another enters on does not overlap).
func maxConcurrent(trades []backtest.Trade) int {
	most := 0
	for _, a := range trades {
		n := 0
		for _, b := range trades {
			if !b.EntryTimestamp.After(a.EntryTimestamp) && b.ExitTimestamp.After(a.EntryTimestamp) {
				n++
			}
		}
		most = max(most, n)
	}
	return most
}
