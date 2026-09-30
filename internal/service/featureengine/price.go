package featureengine

import "time"

func returnOverWindow(series []point, at time.Time, currentPrice float64, window time.Duration) *float64 {
	ref, ok := windowRef(series, at, window)
	if !ok || ref.price == 0 {
		return nil
	}
	v := currentPrice/ref.price - 1
	return &v
}

// trailingRange is the highest and lowest price over the trailing window
// ending at at (the current bar included), or nil, nil when no positive
// price exists in it.
func trailingRange(series []point, at time.Time, window time.Duration) (high, low *float64) {
	for _, p := range inWindow(series, at, window) {
		if p.price <= 0 {
			continue
		}
		if high == nil {
			h, l := p.price, p.price
			high, low = &h, &l
			continue
		}
		*high = max(*high, p.price)
		*low = min(*low, p.price)
	}
	return high, low
}

// distanceTo is price/reference - 1: <= 0 against a high, >= 0 against a
// low. nil if reference is missing or non-positive.
func distanceTo(price float64, reference *float64) *float64 {
	if reference == nil || *reference <= 0 {
		return nil
	}
	v := price/(*reference) - 1
	return &v
}

// vwapSlope is the fractional change of VWAP over the trailing window:
// vwap / (VWAP at-or-before at-window) - 1. nil without such a bar or
// with a zero reference/current VWAP.
func vwapSlope(series []point, at time.Time, vwap float64, window time.Duration) *float64 {
	ref, ok := windowRef(series, at, window)
	if !ok || ref.vwap <= 0 || vwap <= 0 {
		return nil
	}
	v := vwap/ref.vwap - 1
	return &v
}

// vwapCrossDirection is +1 when price crossed from below VWAP on the
// previous bar to above it now, -1 for the reverse, 0 when it did not
// cross (including when price sits exactly on VWAP). nil without a
// previous bar (one older than minRefTolerance is a session/outage gap,
// not the previous bar) or a usable VWAP on either bar.
func vwapCrossDirection(series []point, at time.Time, price, vwap float64) *int64 {
	var prev point
	found := false
	for _, p := range series {
		if !p.ts.Before(at) {
			break
		}
		prev, found = p, true
	}
	if !found || at.Sub(prev.ts) > minRefTolerance || prev.vwap <= 0 || vwap <= 0 {
		return nil
	}
	dir := int64(0)
	switch {
	case prev.price < prev.vwap && price > vwap:
		dir = 1
	case prev.price > prev.vwap && price < vwap:
		dir = -1
	}
	return &dir
}
