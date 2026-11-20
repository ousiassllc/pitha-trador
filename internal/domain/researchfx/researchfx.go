// Package researchfx holds the value types of the research-only FX 1-minute
// candle record (issue #1084, tables research_fx_candles and research_fx_days):
// GMO's BID and ASK candles of one pair merged by minute, and the per-trading-day
// marker that tells a day GMO answered for (with candles, or without: the market
// was closed) from a day that was never fetched. The record is never a trading
// input (see internal/repository/researchfx).
package researchfx

import (
	"fmt"
	"time"
)

// Status of a day marker (research_fx_days.status).
const (
	// StatusRecorded: the day had at least one candle.
	StatusRecorded = "recorded"
	// StatusEmpty: GMO answered with no candle at all (weekend or holiday: the
	// market was closed). It is not a gap.
	StatusEmpty = "empty"
)

// RolloverHour is the JST hour at which GMO's trading day (the date of its 1min
// klines request) rolls over: a trading day runs from 06:00 JST to the next
// 06:00 JST.
const RolloverHour = 6

// JST is the zone of the trading day. Japan has no DST, so a fixed zone is exact.
var JST = time.FixedZone("JST", 9*60*60)

// OHLC is one side's candle.
type OHLC struct {
	Open, High, Low, Close float64
}

// Candle is one research_fx_candles row: the minute starting at OpenTime, with
// the BID and ASK sides merged. A side GMO sent no candle for is nil.
type Candle struct {
	Symbol     string
	OpenTime   time.Time
	TradingDay string
	Bid, Ask   *OHLC
}

// Day is one research_fx_days row.
type Day struct {
	Symbol      string
	TradingDay  string
	Status      string
	CandleCount int
}

// TradingDay is the GMO trading day (JST date, YYYY-MM-DD) the instant t is in.
func TradingDay(t time.Time) string {
	return t.In(JST).Add(-RolloverHour * time.Hour).Format(time.DateOnly)
}

// DayStart is the first instant of the trading day (06:00 JST of its date).
func DayStart(day string) (time.Time, error) {
	d, err := time.ParseInLocation(time.DateOnly, day, JST)
	if err != nil {
		return time.Time{}, fmt.Errorf("researchfx: trading day %q is not YYYY-MM-DD: %w", day, err)
	}
	return d.Add(RolloverHour * time.Hour), nil
}

// DayEnd is the first instant after the trading day (06:00 JST of the next date).
func DayEnd(day string) (time.Time, error) {
	start, err := DayStart(day)
	return start.AddDate(0, 0, 1), err
}
