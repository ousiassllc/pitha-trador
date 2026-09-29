package featureengine_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

var jst = time.FixedZone("JST", 9*3600)

// bar builds a history bar with cumulative volume/turnover and a VWAP.
func bar(at time.Time, price float64) domain.Snapshot {
	return domain.Snapshot{
		Timestamp: at, Price: price, Volume: 1000, Turnover: 2_000_000,
		Feature: domain.Feature{VWAP: price},
	}
}

func currentAt(price float64) featureengine.Reading {
	return featureengine.Reading{Price: price, VWAP: price, Volume: 500, Turnover: 1_000_000}
}

func assertGapFeaturesNil(t *testing.T, f domain.Feature) {
	t.Helper()
	for name, v := range map[string]*float64{
		"Return1m": f.Return1m, "Return3m": f.Return3m, "Return5m": f.Return5m,
		"Return15m": f.Return15m, "Return30m": f.Return30m,
		"VWAPSlope": f.VWAPSlope, "RealizedVol5m": f.RealizedVol5m,
		"RealizedVol15m": f.RealizedVol15m, "Turnover1m": f.Turnover1m, "Turnover5m": f.Turnover5m,
	} {
		if v != nil {
			t.Errorf("%s = %v, want nil (reference bar is across a session gap)", name, *v)
		}
	}
	if f.Volume1m != nil || f.Volume5m != nil || f.VWAPCrossDirection != nil {
		t.Errorf("Volume1m/5m/VWAPCrossDirection = %v/%v/%v, want nil", f.Volume1m, f.Volume5m, f.VWAPCrossDirection)
	}
}

// #166/#176: the previous session's closing bars must not become the
// reference for 9:01's N-minute features (overnight gap != momentum).
func TestCompute_PreviousSessionBarsAreNotAWindowReference(t *testing.T) {
	now := time.Date(2026, 9, 29, 9, 1, 0, 0, jst)
	prevClose := time.Date(2026, 9, 28, 15, 29, 0, 0, jst)
	history := []domain.Snapshot{bar(prevClose, 2000), bar(prevClose.Add(-time.Minute), 1990), bar(prevClose.Add(-5*time.Minute), 1980)}

	f := featureengine.Compute(featureengine.Input{Timestamp: now, Current: currentAt(2100), History: history})
	assertGapFeaturesNil(t, f)
}

// #166/#176: after the 12:30 reopening, the last morning bar (11:29) is not
// a 5-minute reference.
func TestCompute_PreLunchBarIsNotAWindowReferenceAfterReopen(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 31, 0, 0, jst)
	history := []domain.Snapshot{
		bar(time.Date(2026, 9, 29, 11, 29, 0, 0, jst), 2000),
		bar(time.Date(2026, 9, 29, 11, 28, 0, 0, jst), 1995),
	}

	f := featureengine.Compute(featureengine.Input{Timestamp: now, Current: currentAt(2100), History: history})
	assertGapFeaturesNil(t, f)
}

// A restart in mid-session leaves a gap: return_1m must not silently become
// a 50-minute return off the pre-restart bar.
func TestCompute_OutageGapMakesShortWindowsMissing(t *testing.T) {
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, jst)
	history := []domain.Snapshot{bar(time.Date(2026, 9, 29, 9, 10, 0, 0, jst), 2000)}

	f := featureengine.Compute(featureengine.Input{Timestamp: now, Current: currentAt(2100), History: history})
	if f.Return1m != nil || f.Return5m != nil || f.VWAPSlope != nil || f.Turnover1m != nil {
		t.Errorf("Return1m/5m/VWAPSlope/Turnover1m = %v/%v/%v/%v, want nil after a 50-minute gap", f.Return1m, f.Return5m, f.VWAPSlope, f.Turnover1m)
	}
}

// Ordinary 60-second cadence, with the scan jittering a few seconds, still
// yields every window once enough same-session bars exist.
func TestCompute_ContinuousMinuteBarsStillYieldWindows(t *testing.T) {
	now := time.Date(2026, 9, 29, 9, 40, 3, 0, jst)
	var history []domain.Snapshot
	for m := 1; m <= 35; m++ {
		history = append(history, bar(now.Add(-time.Duration(m)*time.Minute).Add(-2*time.Second), 2000+float64(m)))
	}

	f := featureengine.Compute(featureengine.Input{Timestamp: now, Current: currentAt(2100), History: history})
	for name, v := range map[string]*float64{
		"Return1m": f.Return1m, "Return5m": f.Return5m, "Return30m": f.Return30m,
		"VWAPSlope": f.VWAPSlope, "RealizedVol5m": f.RealizedVol5m,
	} {
		if v == nil {
			t.Errorf("%s = nil, want a value with continuous minute bars", name)
		}
	}
}

// After the reopening, once same-session bars exist the returns come back
// and are measured against them, not the morning session.
func TestCompute_ReturnUsesSameSessionBarAfterReopen(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 36, 0, 0, jst)
	history := []domain.Snapshot{
		bar(time.Date(2026, 9, 29, 11, 29, 0, 0, jst), 1000),
		bar(time.Date(2026, 9, 29, 12, 31, 0, 0, jst), 2000),
	}

	f := featureengine.Compute(featureengine.Input{Timestamp: now, Current: currentAt(2100), History: history})
	if f.Return5m == nil || !approxEqual(*f.Return5m, 2100.0/2000.0-1) {
		t.Fatalf("Return5m = %v, want %v against the 12:31 bar", f.Return5m, 2100.0/2000.0-1)
	}
}
