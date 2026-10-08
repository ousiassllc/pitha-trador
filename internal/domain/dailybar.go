package domain

import (
	"math"
	"time"
)

// DailyBar is one 立会日 of one symbol (docs/architecture/er/tables-market.md
// §daily_bars). Open..Volume are the raw values, Adj* the same values adjusted
// by the broker's 株式分割換算係数 (they equal the raw ones until a split).
// There is no turnover: the broker's daily history does not carry it.
type DailyBar struct {
	Symbol    string
	TradeDate string // YYYY-MM-DD (JST 立会日)
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64

	AdjOpen   float64
	AdjHigh   float64
	AdjLow    float64
	AdjClose  float64
	AdjVolume float64
}

// SameAdjustment reports whether b and o carry the same split-adjusted close
// and volume: false for the same 立会日 means a split changed the factor.
func (b DailyBar) SameAdjustment(o DailyBar) bool {
	return nearlyEqual(b.AdjClose, o.AdjClose) && nearlyEqual(b.AdjVolume, o.AdjVolume)
}

func nearlyEqual(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

// DailyBarTarget is one symbol of the broker's master the nightly batch may
// fetch daily bars for.
type DailyBarTarget struct {
	Symbol string
	// Market is the 市場区分: prime, standard, growth or other.
	Market string
	// PrevClose is the master's 前日終値 (0 when unknown).
	PrevClose float64
}

// Daily bar run statuses.
const (
	DailyBarRunRunning   = "running"
	DailyBarRunSucceeded = "succeeded"
	DailyBarRunFailed    = "failed"
)

// DailyBarRun is the record of one night's daily-bar batch
// (docs/architecture/er/tables-market.md §daily_bar_runs). Succeeded means
// every symbol of the universe was attempted (Failed of them did not
// answer); Failed means the night was cut short (see Error) and Cursor is
// where the retry resumes.
type DailyBarRun struct {
	RunDate    string // YYYY-MM-DD of the 立会日 the night follows
	Status     string
	StartedAt  time.Time
	FinishedAt *time.Time
	Symbols    int // universe size
	Requests   int // 日足 requests sent
	SavedBars  int
	Failed     int // symbols whose request failed
	DurationMS int64
	Cursor     string // last symbol processed ("" before the first)
	Error      string
}
