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

	// SpecialQuote, PriceLimit and Lendable record whether the instrument
	// could be entered at this bar (issue #511): Fast Screener drops a
	// special-quote / stop-high / stop-low bar and Policy Engine rejects a
	// SHORT on a non-lendable instrument (functional.md FR-FS-1/FR-POLICY-3).
	// Rows written before migration 000026 hold the zero values, i.e.
	// "no restriction known".
	SpecialQuote bool       // 特別気配（BidSign/AskSign）
	PriceLimit   PriceLimit // ストップ高/安（PriceLimitNone=該当なし）
	// Lendable is true when the instrument is a 貸借銘柄 (shortable), false
	// when it is not, and nil when unknown (legacy row, or the symbol info
	// could not be fetched): only an explicit false blocks a short.
	Lendable *bool

	// RawDataJSON is the kabuステーションAPI response for this bar, kept
	// verbatim for re-calculation/audit purposes.
	RawDataJSON string

	CreatedAt time.Time
}

// PriceLimit is how a bar's price sits against the day's price limits
// (値幅制限): stuck at the upper limit (ストップ高), the lower limit
// (ストップ安) or neither. The values are persisted verbatim to
// market_snapshots.price_limit.
type PriceLimit string

const (
	PriceLimitNone PriceLimit = ""
	PriceLimitUp   PriceLimit = "up"
	PriceLimitDown PriceLimit = "down"
)
