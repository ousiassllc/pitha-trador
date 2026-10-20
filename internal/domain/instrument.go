package domain

import "time"

// Instrument kinds (docs/architecture/er.md §instruments kind). Only
// InstrumentKindStock instruments are screened/traded; the index kinds are
// tracked purely as Feature Engine market-context inputs (functional.md
// §4.1: market_return_1m/5m, sector_return_5m).
const (
	InstrumentKindStock       = "stock"
	InstrumentKindMarketIndex = "market_index" // TOPIX / Nikkei225 等
	InstrumentKindSectorIndex = "sector_index" // 業種指数。Sector が対応する銘柄の業種と一致する
)

// Instrument asset classes (docs/architecture/er.md §instruments asset_class,
// issue #824). The asset class decides the budget bucket (Bucket*) every
// positions / paper_orders / trade_signals row is written under; it is a
// separate axis from Kind and leaves Kind's meaning unchanged.
const (
	AssetClassStock   = "stock"
	AssetClassBondETF = "bond_etf"
)

// Budget buckets (issue #824): the per-asset-class budget a row belongs to.
// There are exactly two, one per asset class and named after it.
const (
	BucketStock   = AssetClassStock
	BucketBondETF = AssetClassBondETF
)

// Instrument is a single tradable security tracked by the scanner
// (docs/architecture/er.md §instruments). It is the target-universe
// master record that market_snapshots, jev_decisions, trade_signals,
// paper_orders and positions all reference by ID.
type Instrument struct {
	ID     int64
	Symbol string // 証券コード（例: "7203"）。UNIQUE制約
	Name   string
	Market string  // 市場区分（例: "TSE Prime"）
	Sector *string // 業種。NULL可（sector_return_5m算出に利用）

	// Kind is one of InstrumentKind*. Empty is treated as
	// InstrumentKindStock by InstrumentRepository.Create.
	Kind string

	// AssetClass is one of AssetClass*. Empty is treated as AssetClassStock
	// by InstrumentRepository.Create and Upsert.
	AssetClass string

	// IsActive controls whether the instrument is included in the Fast
	// Screener's scan universe. false excludes it from scanning without
	// deleting its historical data.
	IsActive bool

	CreatedAt time.Time
	UpdatedAt time.Time
}
