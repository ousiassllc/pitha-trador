package policy

import "github.com/ousiassllc/pitha-trador/internal/domain"

// InputFromSnapshot returns the snapshot-derived part of Input -
// EntryPriceReference, SpreadBps, Turnover5mJPY and the entry-eligibility
// flags SpecialQuote/PriceLimit/Lendable - for the bar snap. It is the
// single mapping the live Handler and the backtest replay share, so a
// bar cannot be tradable in a replay yet rejected live (issue #595).
// The caller fills in the rest (InstrumentID, Symbol, Timestamp,
// Decision, Calibrated, APIErr).
func InputFromSnapshot(snap domain.Snapshot) Input {
	return Input{
		EntryPriceReference: &snap.Price,
		SpreadBps:           snap.SpreadBps,
		Turnover5mJPY:       snap.Feature.Turnover5m,
		SpecialQuote:        snap.SpecialQuote,
		PriceLimit:          snap.PriceLimit,
		Lendable:            snap.Lendable,
	}
}
