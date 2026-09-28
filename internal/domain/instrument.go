package domain

import "time"

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

	// IsActive controls whether the instrument is included in the Fast
	// Screener's scan universe. false excludes it from scanning without
	// deleting its historical data.
	IsActive bool

	CreatedAt time.Time
	UpdatedAt time.Time
}
