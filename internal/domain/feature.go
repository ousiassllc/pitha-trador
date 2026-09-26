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
	Return1m  *float64
	Return5m  *float64
	Return15m *float64

	// VWAP and PriceVsVWAPBps are always computable once at least one
	// trade has occurred in the session, so unlike the other fields they
	// are not nullable (docs/architecture/er.md §market_snapshots).
	VWAP           float64
	PriceVsVWAPBps float64

	VolumeRatio5m      *float64
	OrderbookImbalance *float64
	RealizedVol5m      *float64
	MarketReturn5m     *float64
	SectorReturn5m     *float64
}
