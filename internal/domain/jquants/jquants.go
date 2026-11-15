// Package jquants holds the value types of the J-Quants read adapter (issue
// #1093): listed issues, daily bars and minute bars as the J-Quants API V2
// returns them, in the shape the research tables store them.
//
// The data is research-only and private-use (J-Quants' terms: no external
// sending, deleted on cancellation). It is never a trading input.
package jquants

import (
	"strings"
	"time"
)

// Source is the value of the `source` column of every research_jquants_* table
// and of research_horizon_labels.exit_source for a row derived from the data.
const Source = "jquants"

// Datasets are the kinds of data the adapter fetches, as the `dataset` column
// of research_jquants_fetch_log / research_jquants_coverage names them.
const (
	DatasetSymbols = "symbols"
	DatasetDaily   = "daily"
	DatasetMinute  = "minute"
	// DatasetRelabel is only a coverage mark: the research labels of the day
	// were re-labeled with the stored minute bars.
	DatasetRelabel = "relabel"
)

// Symbol is one row of the listed-issue master (/equities/master).
type Symbol struct {
	// AsOf is the master's date (JST, YYYY-MM-DD).
	AsOf string
	// Code is the J-Quants issue code (5 characters, e.g. "72030").
	Code string
	// Symbol is the 4-character TSE code the rest of the application uses
	// (e.g. "7203"); see SymbolOf.
	Symbol        string
	CompanyName   string
	CompanyNameEn string
	Sector17Code  string
	Sector33Code  string
	ScaleCategory string
	MarketCode    string
	MarketName    string
}

// DailyBar is one issue-day of /equities/bars/daily. Prices are unadjusted;
// the Adj* fields are J-Quants' adjusted values. A day without an execution
// has no prices (all pointers nil), as the API returns them as null.
type DailyBar struct {
	// Day is the trading date (JST, YYYY-MM-DD).
	Day    string
	Code   string
	Symbol string

	Open, High, Low, Close *float64
	Volume, Turnover       *float64

	AdjFactor                                     *float64
	AdjOpen, AdjHigh, AdjLow, AdjClose, AdjVolume *float64

	// UpperLimit / LowerLimit are the day's limit-hit flags ("0" / "1"), "" when
	// the response carries none.
	UpperLimit, LowerLimit string
}

// MinuteBar is one issue-minute of /equities/bars/minute (a minute without
// execution is not returned by the API).
type MinuteBar struct {
	// Day is the trading date (JST, YYYY-MM-DD).
	Day    string
	Code   string
	Symbol string
	// At is the START of the minute (UTC). The API's Time is JST HH:mm.
	At time.Time

	Open, High, Low, Close float64
	Volume, Turnover       float64
}

// SymbolOf is the 4-character TSE symbol of a J-Quants issue code. J-Quants
// codes are 5 characters: the 4-character code plus a check digit "0" for
// common stock ("72030" -> "7203"); a code of another shape is kept as is.
func SymbolOf(code string) string {
	code = strings.TrimSpace(code)
	if len(code) == 5 && code[4] == '0' {
		return code[:4]
	}
	return code
}

// CodeOf is the 5-character J-Quants issue code of a 4-character TSE symbol
// (the inverse of SymbolOf for common stock).
func CodeOf(symbol string) string {
	symbol = strings.TrimSpace(symbol)
	if len(symbol) == 4 {
		return symbol + "0"
	}
	return symbol
}
