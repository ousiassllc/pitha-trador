package gmo

import (
	"time"
)

// Interval is a candle width. GMO offers more; only these two are supported
// (issue #907).
type Interval string

const (
	// Interval1Min is the 1-minute candle. GMO serves it only for trading
	// days from MinuteCandlesSince onwards, one trading day per request.
	Interval1Min Interval = "1min"
	// Interval1Day is the daily candle, requested one year per call.
	Interval1Day Interval = "1day"
)

// PriceType selects which side of the quote a candle is built from.
type PriceType string

const (
	PriceBid PriceType = "BID"
	PriceAsk PriceType = "ASK"
)

// MarketStatus is GMO's 外国為替FX status of a pair.
type MarketStatus string

const (
	MarketOpen  MarketStatus = "OPEN"
	MarketClose MarketStatus = "CLOSE"
)

// jst is the zone GMO's trading-day switch (06:00) and year keys are defined
// in. A fixed zone avoids depending on the OS tz database (Windows).
var jst = time.FixedZone("JST", 9*60*60)

// minuteDayStartHour is the JST hour at which GMO's 1min trading day
// (the date= parameter) rolls over.
const minuteDayStartHour = 6

// MinuteCandlesSince is the first trading day (JST) GMO serves 1min
// candles for.
var MinuteCandlesSince = time.Date(2023, time.October, 28, 0, 0, 0, 0, jst)

// Ticker is one pair's current quote. Ask is the price you buy at, Bid the
// price you sell at. There is no volume or depth: the API does not return it.
type Ticker struct {
	Symbol    string
	Bid       float64
	Ask       float64
	Timestamp time.Time
	Status    MarketStatus
}

// Candle is one OHLC bar of a single price type (BID or ASK; the caller knows
// which from the request). There is no volume: the API does not return it.
type Candle struct {
	OpenTime time.Time
	Open     float64
	High     float64
	Low      float64
	Close    float64
}

// KlinesRequest selects the candles to fetch.
type KlinesRequest struct {
	Symbol    string
	PriceType PriceType
	Interval  Interval
	// At picks the period to fetch. For Interval1Min it is any instant of the
	// wanted GMO trading day (06:00 JST to the next 06:00 JST; the whole day's
	// candles are returned). For Interval1Day it picks the calendar year (JST)
	// whose daily candles are returned.
	At time.Time
}

// dateParam renders the GMO "date" query value for r.At and reports an
// unsupported period. interval must already be validated.
func (r KlinesRequest) dateParam() (string, error) {
	switch r.Interval {
	case Interval1Min:
		day := r.At.In(jst).Add(-minuteDayStartHour * time.Hour)
		y, m, d := day.Date()
		if time.Date(y, m, d, 0, 0, 0, 0, jst).Before(MinuteCandlesSince) {
			return "", ErrUnsupportedDate
		}
		return day.Format("20060102"), nil
	default:
		return r.At.In(jst).Format("2006"), nil
	}
}
