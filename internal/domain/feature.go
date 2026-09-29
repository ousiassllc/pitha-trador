package domain

// Feature holds the Feature Engine's computed technical/market-context
// values for a single instrument at a single point in time
// (docs/architecture/er.md §market_snapshots,
// docs/requirements/functional.md §4.1). It is embedded in Snapshot rather
// than persisted on its own: the market_snapshots table stores the raw
// market data and its derived Feature values together in one row.
//
// Fields are pointers where the underlying value cannot always be
// computed - e.g. returns immediately after startup (insufficient history)
// or order-book/trade-derived values when kabuステーションAPI does not
// report board data for the instrument/time - and must be persisted as
// NULL rather than a placeholder number (functional.md FR-FE-2).
type Feature struct {
	// 価格 (§4.1). Returns are fractions (0.01 == +1%). HighDistance5m /
	// LowDistance5m are price/high-1 (<= 0) and price/low-1 (>= 0) over
	// the trailing 5 minutes; SessionHighDistance / SessionLowDistance use
	// the session high/low kabuステーションAPI reports.
	Return1m  *float64
	Return3m  *float64
	Return5m  *float64
	Return15m *float64
	Return30m *float64

	HighDistance5m      *float64
	LowDistance5m       *float64
	SessionHighDistance *float64
	SessionLowDistance  *float64

	// VWAP and PriceVsVWAPBps are always computable once at least one
	// trade has occurred in the session, so unlike the other fields they
	// are not nullable (docs/architecture/er.md §market_snapshots).
	VWAP           float64
	PriceVsVWAPBps float64
	// VWAPSlope is the fractional change of VWAP over the trailing 5
	// minutes. VWAPCrossDirection is +1 when price crossed from below to
	// above VWAP since the previous bar, -1 for above to below, 0 for no
	// cross.
	VWAPSlope          *float64
	VWAPCrossDirection *int64

	// 出来高: Volume1m/5m are traded shares and Turnover1m/5m traded JPY
	// over the trailing window - differences of kabuステーションAPI's
	// cumulative session TradingVolume/TradingValue, never sums of them.
	Volume1m      *int64
	Volume5m      *int64
	VolumeRatio1m *float64
	VolumeRatio5m *float64
	Turnover1m    *float64
	Turnover5m    *float64

	// ボラティリティ: ATR values are in price units (JPY).
	ATR1m                    *float64
	ATR5m                    *float64
	RealizedVol5m            *float64
	RealizedVol15m           *float64
	VolatilityExpansionRatio *float64

	// 板・約定 (取得可能な場合). Best bid/ask/spread live on Snapshot.
	// BidDepth/AskDepth are the total displayed quantity across the
	// reported book levels; Microprice is the quantity-weighted mid.
	// BuyTradeRatio/SellTradeRatio are the shares of tick-rule classified
	// volume over the trailing 5 minutes and TradeFlowImbalance is
	// buy-sell over their sum.
	BidDepth           *float64
	AskDepth           *float64
	OrderbookImbalance *float64
	BuyTradeRatio      *float64
	SellTradeRatio     *float64
	TradeFlowImbalance *float64
	Microprice         *float64

	// 市場コンテキスト: MarketReturn1m/5m are the mean return of the
	// tracked market-index instruments (instruments.kind=market_index,
	// e.g. TOPIX/Nikkei225), SectorReturn5m the return of the tracked
	// sector-index instrument matching the stock's sector.
	// StockVsSectorRelativeStrength is Return5m - SectorReturn5m.
	// MarketBreadth is (advancers-decliners)/count over the stock
	// universe's latest 5-minute returns, in [-1, 1].
	MarketReturn1m                *float64
	MarketReturn5m                *float64
	SectorReturn5m                *float64
	StockVsSectorRelativeStrength *float64
	MarketBreadth                 *float64
}
