package featureengine_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

// fullScanBarSpacing is the gap between consecutive bars of one symbol when
// scan.full_scan_enabled: true (one full REST cycle of ~4,000 symbols is
// about 8 minutes, FR-SCHED-2/7).
const fullScanBarSpacing = 8 * time.Minute

// fullScanHistory is a symbol's persisted bars under the full scan: n bars,
// one per cycle, ending one cycle before now.
func fullScanHistory(now time.Time, n int) []domain.Snapshot {
	out := make([]domain.Snapshot, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, bar(now.Add(-time.Duration(i)*fullScanBarSpacing), 2000-float64(i)))
	}
	return out
}

// Documents the full-scan limitation (issues #693/#694/#696): bars about 8
// minutes apart never contain a window reference within FR-FE-5's tolerance
// (max(window/2, 90s)) of at-1m/3m/5m, so the short-window history features
// are nil (history shortage) and the Fast Screener excludes the symbol as
// missing_return_5m / missing_volume_ratio. This is the specified behaviour,
// not a bug: the reference tolerance is not widened because that would
// mislabel an 8-minute move as a 5-minute one. Long windows still find a
// reference bar.
func TestCompute_FullScanBarSpacingLeavesShortWindowFeaturesNil(t *testing.T) {
	now := time.Date(2026, 10, 7, 10, 30, 0, 0, jst)
	history := fullScanHistory(now, 6)

	f := featureengine.Compute(featureengine.Input{Timestamp: now, Current: currentAt(2100), History: history})

	for name, v := range map[string]*float64{
		"Return1m": f.Return1m, "Return3m": f.Return3m, "Return5m": f.Return5m,
		"VolumeRatio1m": f.VolumeRatio1m, "VolumeRatio5m": f.VolumeRatio5m,
		"Turnover1m": f.Turnover1m, "Turnover5m": f.Turnover5m,
		"RealizedVol5m": f.RealizedVol5m, "RealizedVol15m": f.RealizedVol15m,
	} {
		if v != nil {
			t.Errorf("%s = %v, want nil with %v-spaced bars", name, *v, fullScanBarSpacing)
		}
	}
	if f.Volume1m != nil || f.Volume5m != nil {
		t.Errorf("Volume1m/5m = %v/%v, want nil with %v-spaced bars", f.Volume1m, f.Volume5m, fullScanBarSpacing)
	}
	if f.Return15m == nil || f.Return30m == nil {
		t.Errorf("Return15m/30m = %v/%v, want non-nil: a bar lies within the wider tolerance of the 15m/30m window start", f.Return15m, f.Return30m)
	}
	if got := featureengine.TurnoverOverWindow(now, 1_000_000, history, 5*time.Minute); got != nil {
		t.Errorf("TurnoverOverWindow(5m) = %v, want nil with %v-spaced bars", *got, fullScanBarSpacing)
	}
}

// The market/sector index returns share the same window reference, so a
// 620s max-age (scan.full_scan_max_snapshot_age_seconds) only widens the
// latest-bar freshness check: it does not make 8-minute-spaced index bars
// yield market_return_1m/5m.
func TestIndexReturn_FullScanBarSpacingStaysNilDespiteWideMaxStale(t *testing.T) {
	at := time.Date(2026, 10, 7, 10, 30, 0, 0, jst)
	latest := at.Add(-30 * time.Second)
	bars := []domain.Snapshot{
		bar(latest.Add(-2*fullScanBarSpacing), 990),
		bar(latest.Add(-fullScanBarSpacing), 1000),
		bar(latest, 1010),
	}
	for _, window := range []time.Duration{time.Minute, 5 * time.Minute} {
		if got := featureengine.IndexReturn(at, bars, window, 620*time.Second); got != nil {
			t.Errorf("IndexReturn(%v) = %v, want nil with %v-spaced bars", window, *got, fullScanBarSpacing)
		}
	}
}
