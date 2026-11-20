// Package researchindex holds the value types of the research-only index record
// (issue #1084, tables research_index_minutes and research_index_days): the
// 1-minute values of stock indices (日経平均, TOPIX, ...) taken from what the
// 立花 quote recording (issue #1083) already stores, and
// the per-trading-day marker that tells a day with values from a day on which
// the index could not be obtained. The record is never a trading input (see
// internal/repository/researchindex).
package researchindex

import "time"

// Status of a day marker (research_index_days.status).
const (
	// StatusRecorded: the day had at least one value.
	StatusRecorded = "recorded"
	// StatusUnavailable: the 立花 quote recording had no value of the index on a
	// trading day (the recording was off, the code was not subscribed, or 立花
	// quoted nothing). It is "not obtained", not a market holiday.
	StatusUnavailable = "unavailable"
)

// Ways a value arrived (the table's CHECK values; the same as the 立花 quote recording's via).
const (
	ViaEvent = "event"
	ViaREST  = "rest"
)

// SampleLag is how long after the start of its minute a recorded value is known:
// the 立花 recorder samples at the 5th second of each minute. A reader that joins
// a value to a decision time must not use it before MinuteTime + SampleLag.
const SampleLag = 5 * time.Second

// JST is the zone of the trading day.
var JST = time.FixedZone("JST", 9*60*60)

// Minute is one research_index_minutes row: the value of the index for the minute
// starting at Time (the session minute it stands for).
type Minute struct {
	Symbol     string
	Time       time.Time
	TradingDay string
	Price      float64
	Via        string
}

// Day is one research_index_days row.
type Day struct {
	Symbol      string
	TradingDay  string
	Status      string
	MinuteCount int
}

// TradingDay is the TSE date (JST, YYYY-MM-DD) the instant t is in.
func TradingDay(t time.Time) string { return t.In(JST).Format(time.DateOnly) }

// DayStart is the first instant of the JST date day.
func DayStart(day string) (time.Time, error) { return time.ParseInLocation(time.DateOnly, day, JST) }

// labels are the 立花 codes of the well-known indices. A code that is not here is
// still recorded; it just has no name.
var labels = map[string]string{
	"101": "日経平均", "151": "TOPIX",
}

// Label is the name of the index code ("" when it is not one of the known codes).
func Label(symbol string) string { return labels[symbol] }
