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

	// RawDataJSON is the broker's response for this bar (broker.Quote.Raw;
	// kabuステーションAPI's board for the kabu adapter), kept verbatim for
	// re-calculation/audit purposes.
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

// DefaultMaxSnapshotAge is how old a market_snapshots bar may be, measured
// against the wall clock during a trading session, before it counts as stale
// (issues #685, #754) in ranking-watch mode (scan.full_scan_enabled: false)
// while the Settings screen's scan.snapshot_max_age_seconds is unset
// (snapshotagesettings). A 5-15 minute horizon decision must not rest on a
// minute-old bar, so the default is tight; a bar left from the previous
// session or from before the symbol re-entered the watch list is older. A
// stale bar must not reach Jev Scout/Trader or Paper Entry. Full-scan mode
// refreshes a symbol only once per REST cycle (about 8 minutes), so it uses
// the longer scan.full_scan_max_snapshot_age_seconds instead (issue #686).
const DefaultMaxSnapshotAge = 20 * time.Second

// MarketContextMaxAge is how old an index bar may be, in ranking-watch mode,
// and still feed the market context (functional.md FR-FE-4). Index bars are
// refreshed once per 60-second cycle, so unlike a stock bar judged against
// DefaultMaxSnapshotAge they are allowed three cycles of age.
const MarketContextMaxAge = 3 * time.Minute

// IsStale reports whether s is older than maxAge at now. A bar exactly
// maxAge old is still fresh. maxAge is the ranking-watch snapshot max age
// (DefaultMaxSnapshotAge or the Settings value) in ranking-watch mode and
// the full-scan age in full-scan mode (issue #686).
func (s Snapshot) IsStale(now time.Time, maxAge time.Duration) bool {
	return now.Sub(s.Timestamp) > maxAge
}
