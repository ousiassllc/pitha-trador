package featureengine

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Reading is the current-cycle raw market data point for one instrument,
// as reported by internal/service/marketdata. It is a separate type
// (rather than importing marketdata.Board directly) so this package keeps
// depending only on internal/domain and internal/repository (doc.go).
// Callers translate marketdata.Board into a Reading.
type Reading struct {
	Price float64
	VWAP  float64
	// Volume and Turnover are kabuステーションAPI's cumulative session
	// TradingVolume/TradingValue, not per-bar values: windowed volume and
	// turnover are always differences of these (volumeDelta,
	// TurnoverOverWindow), never sums.
	Volume   int64
	Turnover float64

	// SessionHigh and SessionLow are the session's high/low price
	// (kabuステーションAPI HighPrice/LowPrice); nil when not reported.
	SessionHigh *float64
	SessionLow  *float64

	// Bid, Ask, BidQty and AskQty use the conventional meaning: Bid/BidQty
	// is the best BUY quote and Ask/AskQty the best SELL quote. This is the
	// reverse of kabuステーションAPI's BidPrice/AskPrice naming, which
	// callers swap when translating a marketdata.Board. They are nil
	// whenever kabuステーションAPI does not report board data for this
	// instrument/time, in which case every feature derived from them
	// (OrderbookImbalance, Microprice and the snapshot-level SpreadBps
	// built in engine.go) is nil rather than a placeholder number (FR-FE-2).
	// A crossed book (Bid > Ask) is treated as invalid too: SpreadBps and
	// Microprice are nil (OrderbookImbalance uses quantities only).
	Bid    *float64
	Ask    *float64
	BidQty *float64
	AskQty *float64

	// BidDepth and AskDepth are the total quantity across every book
	// level the API reported on the Bid (buy) / Ask (sell) side; nil when
	// no level was reported.
	BidDepth *float64
	AskDepth *float64
}

// HistoryLookbackBars is how many prior market_snapshots bars the live
// market-data job passes as Input.History for each new bar. Compute's
// longest window is 30 minutes (Return30m); at the 60s full-scan cadence
// 35 bars covers that with margin. internal/service/backtest's
// VerifyNoLookahead recomputes every bar from the same bounded history,
// since VolumeRatio5m's baseline averages over every history bar
// supplied and so depends on exactly how many there were.
const HistoryLookbackBars = 35

// Input bundles everything Compute needs to derive one instrument's
// Feature values for the bar at Timestamp.
type Input struct {
	Timestamp time.Time
	Current   Reading

	// History is this instrument's prior market_snapshots rows, in any
	// order. Bars timestamped after Timestamp are ignored so a caller
	// that accidentally passes "future" bars (e.g. a backtest replaying a
	// full day at once) cannot leak look-ahead information (FR-FE-1).
	History []domain.Snapshot

	// Market context (functional.md §4.1), computed by the caller from
	// the tracked index instruments' and the stock universe's own
	// snapshots (see MarketContext/IndexReturn) and passed through
	// unchanged. nil when unavailable.
	MarketReturn1m *float64
	MarketReturn5m *float64
	SectorReturn5m *float64
	MarketBreadth  *float64
}

// Compute derives one instrument's Feature values for the bar at
// in.Timestamp (functional.md §4.1). Every return/ratio/volatility value
// uses only in.Current and in.History bars timestamped at or before
// in.Timestamp (FR-FE-1); orderbook-derived values are nil whenever
// in.Current lacks the needed board quantities (FR-FE-2).
func Compute(in Input) domain.Feature {
	series := buildSeries(in)
	at, cur := in.Timestamp, in.Current

	ret5m := returnOverWindow(series, at, cur.Price, 5*time.Minute)
	realized5m := realizedVol(series, at, 5)
	realized15m := realizedVol(series, at, 15)
	high5m, low5m := trailingRange(series, at, 5*time.Minute)
	buyRatio, sellRatio, flowImbalance := tradeFlow(series, at, 5*time.Minute)

	return domain.Feature{
		Return1m:  returnOverWindow(series, at, cur.Price, time.Minute),
		Return3m:  returnOverWindow(series, at, cur.Price, 3*time.Minute),
		Return5m:  ret5m,
		Return15m: returnOverWindow(series, at, cur.Price, 15*time.Minute),
		Return30m: returnOverWindow(series, at, cur.Price, 30*time.Minute),

		HighDistance5m:      distanceTo(cur.Price, high5m),
		LowDistance5m:       distanceTo(cur.Price, low5m),
		SessionHighDistance: distanceTo(cur.Price, cur.SessionHigh),
		SessionLowDistance:  distanceTo(cur.Price, cur.SessionLow),

		VWAP:               cur.VWAP,
		PriceVsVWAPBps:     priceVsVWAPBps(cur.Price, cur.VWAP),
		VWAPSlope:          vwapSlope(series, at, cur.VWAP, 5*time.Minute),
		VWAPCrossDirection: vwapCrossDirection(series, at, cur.Price, cur.VWAP),

		Volume1m:      volumeOverWindow(series, at, cur.Volume, time.Minute),
		Volume5m:      volumeOverWindow(series, at, cur.Volume, 5*time.Minute),
		VolumeRatio1m: volumeRatio(series, at, cur.Volume, time.Minute),
		VolumeRatio5m: volumeRatio(series, at, cur.Volume, 5*time.Minute),
		Turnover1m:    turnoverOverWindow(series, at, cur.Turnover, time.Minute),
		Turnover5m:    turnoverOverWindow(series, at, cur.Turnover, 5*time.Minute),

		ATR1m:                    atr(series, at, time.Minute, atr1mBars),
		ATR5m:                    atr(series, at, 5*time.Minute, atr5mBars),
		RealizedVol5m:            realized5m,
		RealizedVol15m:           realized15m,
		VolatilityExpansionRatio: ratioOrNil(realized5m, realized15m),

		BidDepth:           cur.BidDepth,
		AskDepth:           cur.AskDepth,
		OrderbookImbalance: orderbookImbalance(cur),
		BuyTradeRatio:      buyRatio,
		SellTradeRatio:     sellRatio,
		TradeFlowImbalance: flowImbalance,
		Microprice:         microprice(cur),

		MarketReturn1m:                in.MarketReturn1m,
		MarketReturn5m:                in.MarketReturn5m,
		SectorReturn5m:                in.SectorReturn5m,
		StockVsSectorRelativeStrength: difference(ret5m, in.SectorReturn5m),
		MarketBreadth:                 in.MarketBreadth,
	}
}

// ReturnOverWindow computes (currentPrice / referencePrice) - 1, where
// referencePrice is the Price of the most recent history bar timestamped
// at or before at.Add(-window), or nil if no such bar exists. It is
// exported so callers can derive market/sector index returns using the
// exact same look-ahead-safe logic Compute uses for a stock's own
// returns.
func ReturnOverWindow(at time.Time, currentPrice float64, history []domain.Snapshot, window time.Duration) *float64 {
	series := buildSeries(Input{Timestamp: at, Current: Reading{Price: currentPrice}, History: history})
	return returnOverWindow(series, at, currentPrice, window)
}

// TurnoverOverWindow is the traded value (JPY) over the trailing window
// ending at at: currentTurnover minus the cumulative session turnover
// recorded by the latest history bar at or before at.Add(-window). It is
// the single definition of turnover_1m/5m (functional.md §4.1) shared by
// Compute and any caller that only holds persisted snapshots. nil when no
// history bar reaches back that far (insufficient history) or the
// cumulative value went backwards (e.g. a new session started).
func TurnoverOverWindow(at time.Time, currentTurnover float64, history []domain.Snapshot, window time.Duration) *float64 {
	series := buildSeries(Input{Timestamp: at, Current: Reading{Turnover: currentTurnover}, History: history})
	return turnoverOverWindow(series, at, currentTurnover, window)
}

func priceVsVWAPBps(price, vwap float64) float64 {
	if vwap == 0 {
		return 0
	}
	return (price - vwap) / vwap * 10000
}

// orderbookImbalance is (bidQty-askQty)/(bidQty+askQty), or nil whenever
// either quantity is missing (FR-FE-2) or both are zero.
func orderbookImbalance(r Reading) *float64 {
	if r.BidQty == nil || r.AskQty == nil {
		return nil
	}
	total := *r.BidQty + *r.AskQty
	if total == 0 {
		return nil
	}
	v := (*r.BidQty - *r.AskQty) / total
	return &v
}

// microprice is the quantity-weighted mid (bid*askQty + ask*bidQty) /
// (bidQty+askQty): it leans toward the side with less resting quantity.
// nil whenever a quote or quantity is missing (FR-FE-2), the book is
// crossed (Bid > Ask, same invalid-book rule as spreadBps) or both
// quantities are zero.
func microprice(r Reading) *float64 {
	if r.Bid == nil || r.Ask == nil || r.BidQty == nil || r.AskQty == nil || *r.Bid > *r.Ask {
		return nil
	}
	total := *r.BidQty + *r.AskQty
	if total == 0 {
		return nil
	}
	v := (*r.Bid**r.AskQty + *r.Ask**r.BidQty) / total
	return &v
}
