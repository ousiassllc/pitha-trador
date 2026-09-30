package featureengine_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

var t0 = time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)

// bars builds one-minute history bars ending 1 minute before t0, oldest
// first: prices[i] is the price (len-i) minutes before t0.
func bars(prices ...float64) []domain.Snapshot {
	out := make([]domain.Snapshot, len(prices))
	for i, p := range prices {
		out[i] = domain.Snapshot{Timestamp: minutesAgo(t0, len(prices)-i), Price: p}
	}
	return out
}

func wantValue(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %v", name, want)
	}
	if !approxEqual(*got, want) {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

func TestTurnoverOverWindow_IsDifferenceOfCumulativeNotSum(t *testing.T) {
	// kabu TradingValue is cumulative: 100M at t-5m ... 150M now. Summing
	// the bars (the #163 bug) would give ~600M; the real 5-minute traded
	// value is 50M.
	history := make([]domain.Snapshot, 6)
	for i := range history {
		history[i] = domain.Snapshot{Timestamp: minutesAgo(t0, 6-i), Turnover: 100e6 + float64(i)*10e6}
	}
	// bars at t-6m..t-1m carry 100M,110M,...,150M; now is 160M.
	got := featureengine.TurnoverOverWindow(t0, 160e6, history, 5*time.Minute)
	wantValue(t, "TurnoverOverWindow", got, 160e6-110e6)
}

func TestTurnoverOverWindow_NilWithoutHistoryOrAfterSessionReset(t *testing.T) {
	if got := featureengine.TurnoverOverWindow(t0, 5e6, bars(2000, 2001), 5*time.Minute); got != nil {
		t.Errorf("insufficient history: got %v, want nil", *got)
	}
	history := []domain.Snapshot{{Timestamp: minutesAgo(t0, 5), Turnover: 900e6}}
	if got := featureengine.TurnoverOverWindow(t0, 1e6, history, 5*time.Minute); got != nil {
		t.Errorf("cumulative went backwards: got %v, want nil", *got)
	}
}

func TestCompute_TurnoverAndVolumeWindowsAreCumulativeDifferences(t *testing.T) {
	history := []domain.Snapshot{
		{Timestamp: minutesAgo(t0, 5), Price: 100, Volume: 1000, Turnover: 100_000},
		{Timestamp: minutesAgo(t0, 1), Price: 100, Volume: 1800, Turnover: 180_000},
	}
	f := featureengine.Compute(featureengine.Input{
		Timestamp: t0, History: history,
		Current: featureengine.Reading{Price: 100, VWAP: 100, Volume: 2000, Turnover: 200_000},
	})
	wantValue(t, "Turnover1m", f.Turnover1m, 20_000)
	wantValue(t, "Turnover5m", f.Turnover5m, 100_000)
	if f.Volume1m == nil || *f.Volume1m != 200 || f.Volume5m == nil || *f.Volume5m != 1000 {
		t.Errorf("Volume1m/5m = %v/%v, want 200/1000", f.Volume1m, f.Volume5m)
	}
}

func TestCompute_ExtendedReturnWindows(t *testing.T) {
	history := []domain.Snapshot{
		{Timestamp: minutesAgo(t0, 30), Price: 1000},
		{Timestamp: minutesAgo(t0, 3), Price: 1100},
	}
	f := featureengine.Compute(featureengine.Input{
		Timestamp: t0, History: history, Current: featureengine.Reading{Price: 1210, VWAP: 1210},
	})
	wantValue(t, "Return3m", f.Return3m, 1210.0/1100-1)
	wantValue(t, "Return30m", f.Return30m, 1210.0/1000-1)
}

func TestCompute_HighLowAndSessionDistances(t *testing.T) {
	history := bars(100, 110, 90, 105) // t-4m .. t-1m; all inside 5m
	high, low := 120.0, 80.0
	f := featureengine.Compute(featureengine.Input{
		Timestamp: t0, History: history,
		Current: featureengine.Reading{Price: 100, VWAP: 100, SessionHigh: &high, SessionLow: &low},
	})
	wantValue(t, "HighDistance5m", f.HighDistance5m, 100.0/110-1)
	wantValue(t, "LowDistance5m", f.LowDistance5m, 100.0/90-1)
	wantValue(t, "SessionHighDistance", f.SessionHighDistance, 100.0/120-1)
	wantValue(t, "SessionLowDistance", f.SessionLowDistance, 100.0/80-1)

	none := featureengine.Compute(featureengine.Input{Timestamp: t0, Current: featureengine.Reading{Price: 100, VWAP: 100}})
	if none.SessionHighDistance != nil || none.SessionLowDistance != nil {
		t.Errorf("session distances = %v/%v, want nil without session high/low", none.SessionHighDistance, none.SessionLowDistance)
	}
}

func TestCompute_VWAPSlopeAndCross(t *testing.T) {
	history := []domain.Snapshot{
		{Timestamp: minutesAgo(t0, 5), Price: 100, Feature: domain.Feature{VWAP: 100}},
		{Timestamp: minutesAgo(t0, 1), Price: 99, Feature: domain.Feature{VWAP: 101}}, // below VWAP
	}
	f := featureengine.Compute(featureengine.Input{
		Timestamp: t0, History: history, Current: featureengine.Reading{Price: 103, VWAP: 102},
	})
	wantValue(t, "VWAPSlope", f.VWAPSlope, 102.0/100-1)
	if f.VWAPCrossDirection == nil || *f.VWAPCrossDirection != 1 {
		t.Errorf("VWAPCrossDirection = %v, want +1 (crossed above)", f.VWAPCrossDirection)
	}

	down := featureengine.Compute(featureengine.Input{
		Timestamp: t0, History: []domain.Snapshot{{Timestamp: minutesAgo(t0, 1), Price: 105, Feature: domain.Feature{VWAP: 101}}},
		Current: featureengine.Reading{Price: 100, VWAP: 101},
	})
	if down.VWAPCrossDirection == nil || *down.VWAPCrossDirection != -1 {
		t.Errorf("VWAPCrossDirection = %v, want -1", down.VWAPCrossDirection)
	}

	flat := featureengine.Compute(featureengine.Input{
		Timestamp: t0, History: []domain.Snapshot{{Timestamp: minutesAgo(t0, 1), Price: 105, Feature: domain.Feature{VWAP: 101}}},
		Current: featureengine.Reading{Price: 106, VWAP: 101},
	})
	if flat.VWAPCrossDirection == nil || *flat.VWAPCrossDirection != 0 {
		t.Errorf("VWAPCrossDirection = %v, want 0 (no cross)", flat.VWAPCrossDirection)
	}
	if none := featureengine.Compute(featureengine.Input{Timestamp: t0, Current: featureengine.Reading{Price: 1, VWAP: 1}}); none.VWAPCrossDirection != nil {
		t.Errorf("VWAPCrossDirection = %v, want nil without previous bar", *none.VWAPCrossDirection)
	}
}

func TestCompute_VolumeRatio1mAndVolatilityExpansion(t *testing.T) {
	// Steady 100 shares/minute, then 300 in the latest minute.
	var history []domain.Snapshot
	for i := 20; i >= 1; i-- {
		history = append(history, domain.Snapshot{
			Timestamp: minutesAgo(t0, i), Price: 100 + float64(i%3), Volume: int64((20 - i) * 100),
		})
	}
	f := featureengine.Compute(featureengine.Input{
		Timestamp: t0, History: history,
		Current: featureengine.Reading{Price: 101, VWAP: 100, Volume: 1900 + 300},
	})
	wantValue(t, "VolumeRatio1m", f.VolumeRatio1m, 3)

	if f.RealizedVol15m == nil || f.RealizedVol5m == nil || f.VolatilityExpansionRatio == nil {
		t.Fatalf("vols = %v/%v/%v, want all computable", f.RealizedVol5m, f.RealizedVol15m, f.VolatilityExpansionRatio)
	}
	wantValue(t, "VolatilityExpansionRatio", f.VolatilityExpansionRatio, *f.RealizedVol5m / *f.RealizedVol15m)
}

func TestCompute_ATR(t *testing.T) {
	// One bar per minute: 100, 102, 99, 105 then current 104.
	f := featureengine.Compute(featureengine.Input{
		Timestamp: t0, History: bars(100, 102, 99, 105),
		Current: featureengine.Reading{Price: 104, VWAP: 104},
	})
	// True ranges (1m buckets, newest first): |104-105|=1, |105-99|=6,
	// |99-102|=3, |102-100|=2 -> mean 3.
	wantValue(t, "ATR1m", f.ATR1m, 3)

	none := featureengine.Compute(featureengine.Input{Timestamp: t0, Current: featureengine.Reading{Price: 104, VWAP: 104}})
	if none.ATR1m != nil || none.ATR5m != nil {
		t.Errorf("ATR = %v/%v, want nil without history", none.ATR1m, none.ATR5m)
	}
}

func TestCompute_MicropriceAndDepth(t *testing.T) {
	bid, ask, bidQty, askQty := 100.0, 101.0, 300.0, 100.0
	bd, ad := 900.0, 400.0
	f := featureengine.Compute(featureengine.Input{
		Timestamp: t0,
		Current: featureengine.Reading{
			Price: 100.5, VWAP: 100.5, Bid: &bid, Ask: &ask, BidQty: &bidQty, AskQty: &askQty, BidDepth: &bd, AskDepth: &ad,
		},
	})
	// (100*100 + 101*300) / 400 = 100.75: leans to the ask side, where
	// resting quantity is thin.
	wantValue(t, "Microprice", f.Microprice, 100.75)
	wantValue(t, "BidDepth", f.BidDepth, 900)
	wantValue(t, "AskDepth", f.AskDepth, 400)

	missing := featureengine.Compute(featureengine.Input{Timestamp: t0, Current: featureengine.Reading{Price: 1, VWAP: 1}})
	if missing.Microprice != nil || missing.BidDepth != nil || missing.BuyTradeRatio != nil {
		t.Errorf("board-derived features must be nil without board data (FR-FE-2): %+v", missing)
	}
}

func TestCompute_TradeFlowTickRule(t *testing.T) {
	// Volume increments: +100 on an up-tick, +300 on a down-tick, +100 flat
	// (attributed to the previous down direction).
	history := []domain.Snapshot{
		{Timestamp: minutesAgo(t0, 5), Price: 100, Volume: 0},
		{Timestamp: minutesAgo(t0, 3), Price: 101, Volume: 100},
		{Timestamp: minutesAgo(t0, 1), Price: 100, Volume: 400},
	}
	f := featureengine.Compute(featureengine.Input{
		Timestamp: t0, History: history, Current: featureengine.Reading{Price: 100, VWAP: 100, Volume: 500},
	})
	wantValue(t, "BuyTradeRatio", f.BuyTradeRatio, 100.0/500)
	wantValue(t, "SellTradeRatio", f.SellTradeRatio, 400.0/500)
	wantValue(t, "TradeFlowImbalance", f.TradeFlowImbalance, -300.0/500)
}

func TestCompute_MarketContextPassThroughAndRelativeStrength(t *testing.T) {
	history := []domain.Snapshot{{Timestamp: minutesAgo(t0, 5), Price: 1000}}
	m1, m5, sec, breadth := 0.001, 0.002, 0.01, 0.4
	f := featureengine.Compute(featureengine.Input{
		Timestamp: t0, History: history, Current: featureengine.Reading{Price: 1030, VWAP: 1030},
		MarketReturn1m: &m1, MarketReturn5m: &m5, SectorReturn5m: &sec, MarketBreadth: &breadth,
	})
	wantValue(t, "MarketReturn1m", f.MarketReturn1m, 0.001)
	wantValue(t, "MarketReturn5m", f.MarketReturn5m, 0.002)
	wantValue(t, "SectorReturn5m", f.SectorReturn5m, 0.01)
	wantValue(t, "MarketBreadth", f.MarketBreadth, 0.4)
	wantValue(t, "StockVsSectorRelativeStrength", f.StockVsSectorRelativeStrength, 0.03-0.01)

	noSector := featureengine.Compute(featureengine.Input{
		Timestamp: t0, History: history, Current: featureengine.Reading{Price: 1030, VWAP: 1030},
	})
	if noSector.StockVsSectorRelativeStrength != nil {
		t.Errorf("StockVsSectorRelativeStrength = %v, want nil without sector return", *noSector.StockVsSectorRelativeStrength)
	}
}

func TestIndexReturn(t *testing.T) {
	idx := []domain.Snapshot{
		{Timestamp: minutesAgo(t0, 6), Price: 1000},
		{Timestamp: minutesAgo(t0, 1), Price: 1010},
		{Timestamp: t0.Add(time.Minute), Price: 5000}, // future bar: must be ignored (FR-FE-1)
	}
	wantValue(t, "IndexReturn", featureengine.IndexReturn(t0, idx, 5*time.Minute, 3*time.Minute), 1010.0/1000-1)

	if got := featureengine.IndexReturn(t0.Add(10*time.Minute), idx[:2], 5*time.Minute, 3*time.Minute); got != nil {
		t.Errorf("stale index feed: got %v, want nil", *got)
	}
	if got := featureengine.IndexReturn(t0, nil, 5*time.Minute, 3*time.Minute); got != nil {
		t.Errorf("no bars: got %v, want nil", *got)
	}
	if got := featureengine.IndexReturn(t0, idx[1:2], 5*time.Minute, 3*time.Minute); got != nil {
		t.Errorf("history shorter than window: got %v, want nil", *got)
	}
}

func TestMeanReturnAndBreadth(t *testing.T) {
	a, b := 0.01, 0.03
	wantValue(t, "MeanReturn", featureengine.MeanReturn([]*float64{&a, nil, &b}), 0.02)
	if featureengine.MeanReturn([]*float64{nil, nil}) != nil || featureengine.MeanReturn(nil) != nil {
		t.Error("MeanReturn without values must be nil")
	}

	wantValue(t, "Breadth", featureengine.Breadth([]float64{0.1, 0.2, -0.1, 0}), 1.0/4)
	if featureengine.Breadth(nil) != nil {
		t.Error("Breadth without returns must be nil")
	}
}
