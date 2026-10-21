package risk

import "context"

// EntrySize is the size Check settled on for an approved entry: the quantity
// every limit (position sizing, the bucket budget) was judged at, and the
// latest snapshot price that quantity was sized and judged against.
//
// The entry's fill price is NOT Price. Execution fills a Paper market order
// at the touch plus slippage on the tick grid (fillmodel.Model.Market), so it
// is worse than Price by at most: the distance to the touch (the last price
// may sit on the far side of the spread, so at most one spread; Check rejects
// a spread above max_spread_bps, and a 板寄せ fill crosses none), plus the
// slippage assumption (cost.slippage_*_bps: 2 bps ザラ場 / 5 bps 寄り・引け by
// default, at most costsettings.MaxBps = 1000 bps), plus one tick of
// rounding. The notional that fills can therefore exceed Quantity x Price by
// that fraction (issue #840). The drift is accepted: Quantity is fixed here
// and handed to Enter unchanged, so the judged and the filled share are the
// same number of shares and only the price per share differs.
type EntrySize struct {
	Quantity int64
	Price    float64
}

// CheckThenEnter judges a new entry (Check) and, when it passes, runs enter
// with the quantity that judgement settled on, as one step no other
// CheckThenEnter interleaves with (issue #839): a concurrent candidate cannot
// read the holdings before this entry's position is opened, so two candidates
// that together exceed a limit (the bucket budget, the total exposure, the
// open position count) cannot both pass. enter MUST open the position (or
// fail) before it returns for that to hold, and MUST use size.Quantity as is
// rather than sizing the order again (issue #840).
//
// passed and reason are Check's. err is enter's error; enter is not called
// when the candidate is rejected. Every Paper entry path goes through here;
// Policy Engine's own Check at signal time stays a pre-filter for the
// recorded signal, and this judgement at entry time is the one that counts.
func (e *Engine) CheckThenEnter(ctx context.Context, instrumentID int64, direction string, enter func(size EntrySize) error) (passed bool, reason string, err error) {
	e.entryMu.Lock()
	defer e.entryMu.Unlock()
	var size EntrySize
	if passed, reason = e.check(ctx, instrumentID, direction, &size); !passed {
		return false, reason, nil
	}
	return true, "", enter(size)
}
