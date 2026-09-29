package featureengine

import "time"

// volumeDelta returns currentVolume minus the cumulative session volume
// window earlier than at (i.e. this instrument's traded volume over the
// trailing window ending at at), or false if no bar exists that far back
// or the cumulative value went backwards (new session).
func volumeDelta(series []point, at time.Time, currentVolume int64, window time.Duration) (float64, bool) {
	ref, ok := atOrBefore(series, at.Add(-window))
	if !ok || currentVolume < ref.volume {
		return 0, false
	}
	return float64(currentVolume - ref.volume), true
}

func volumeOverWindow(series []point, at time.Time, currentVolume int64, window time.Duration) *int64 {
	delta, ok := volumeDelta(series, at, currentVolume, window)
	if !ok {
		return nil
	}
	v := int64(delta)
	return &v
}

// turnoverOverWindow is the traded value over the trailing window ending
// at at: currentTurnover minus the cumulative turnover of the latest bar
// at or before at-window. nil if no bar reaches back that far or the
// cumulative value went backwards (new session).
func turnoverOverWindow(series []point, at time.Time, currentTurnover float64, window time.Duration) *float64 {
	ref, ok := atOrBefore(series, at.Add(-window))
	if !ok || currentTurnover < ref.turnover {
		return nil
	}
	v := currentTurnover - ref.turnover
	return &v
}

// volumeRatio compares the trailing-window traded volume ending at at
// against the average trailing-window volume sampled at every earlier
// bar in series, using the same volumeDelta calculation for each sample.
// nil if either the recent window or every historical sample is
// unavailable (insufficient history).
func volumeRatio(series []point, at time.Time, currentVolume int64, window time.Duration) *float64 {
	recent, ok := volumeDelta(series, at, currentVolume, window)
	if !ok {
		return nil
	}

	var baseline []float64
	for _, p := range series {
		if !p.ts.Before(at) {
			continue
		}
		if delta, ok := volumeDelta(series, p.ts, p.volume, window); ok {
			baseline = append(baseline, delta)
		}
	}
	if len(baseline) == 0 {
		return nil
	}

	avg := mean(baseline)
	if avg == 0 {
		return nil
	}
	ratio := recent / avg
	return &ratio
}
