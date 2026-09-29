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

	// IsActive controls whether the instrument is included in the Fast
	// Screener's scan universe. false excludes it from scanning without
	// deleting its historical data.
	IsActive bool

	CreatedAt time.Time
	UpdatedAt time.Time
}
