package domain

import "time"

// Snapshot mirrors a single market_snapshots row: the raw market data
// captured for one Instrument at one 1-minute-bar Timestamp, plus the
// Feature Engine's computed Feature values for that same bar
// (docs/architecture/er.md §market_snapshots).
type Snapshot struct {
	ID           int64
	InstrumentID int64
	Symbol       string // 非正規化（クエリ簡略化用）
	Timestamp    time.Time

	Price float64

	// Bid, Ask and SpreadBps are NULL可: kabuステーションAPIから板情報が
	// 取得できない銘柄・時間帯ではnilを保存する（functional.md FR-FE-2）。
	Bid       *float64
	Ask       *float64
	SpreadBps *float64

	Volume   int64
	Turnover float64

	Feature Feature

	// RawDataJSON is the kabuステーションAPI response for this bar, kept
	// verbatim for re-calculation/audit purposes.
	RawDataJSON string

	CreatedAt time.Time
}
