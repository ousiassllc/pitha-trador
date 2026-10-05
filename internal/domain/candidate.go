package domain

import "time"

// Candidate is one Fast Screener-selected instrument: it has passed the
// FR-FS-1 numeric filters and been ranked among the top N by screen_score
// (requirements/functional.md §4.2), ready to hand off to Jev Scout and to
// display on the Scanner Dashboard (functional.md §5.1).
//
// The Jev* fields and CurrentPosition are not set by the Fast Screener;
// internal/bootstrap/candidates.Refresher fills them each refresh cycle from
// the latest Jev Trader decision (functional.md §4.5) and the open position.
// While there is no Trader decision / no open position they stay nil rather
// than a fabricated placeholder value, which callers must render as
// "pending"/"flat".
type Candidate struct {
	InstrumentID int64
	Symbol       string
	Price        float64

	Return1m *float64
	Return5m *float64

	VolumeRatio5m  *float64
	PriceVsVWAPBps float64
	SpreadBps      *float64

	// ScreenScore is the FR-FS-2 weighted score used to rank candidates;
	// higher ranks first.
	ScreenScore float64

	// JevDirection is one of "LONG"/"SHORT"/"NONE", or nil if Jev Trader
	// has not yet evaluated this candidate.
	JevDirection  *string
	JevConfidence *float64
	// EntryQuality is one of "poor".."exceptional" (functional.md §4.5),
	// or nil if Jev Trader has not yet evaluated this candidate.
	EntryQuality *string

	// CurrentPosition is this instrument's open Paper/Live position size
	// (positive for LONG, negative for SHORT), or nil if flat.
	CurrentPosition *float64

	// AsOf is the scan cycle timestamp this candidate's data was computed
	// at (Feature Engine bar timestamp).
	AsOf time.Time
}
