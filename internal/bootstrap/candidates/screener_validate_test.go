package candidates

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// seedPassingStock stores a stock whose bars pass validLenientFastScreener.
func seedPassingStock(t *testing.T, refresher *Refresher) {
	t.Helper()
	inst := mustCreateInstrument(t, refresher, "7203")
	now := time.Now().UTC().Truncate(time.Minute)
	bar := func(at time.Time, turnover float64) domain.Snapshot {
		return domain.Snapshot{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: at,
			Price: 2500, Volume: 1000, Turnover: turnover, SpreadBps: ptrF(10),
			Feature: domain.Feature{
				VWAP: 2490, VolumeRatio5m: ptrF(1.5), Return5m: ptrF(0.5), RealizedVol5m: ptrF(0.01),
			},
		}
	}
	if _, err := refresher.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{
		bar(now.Add(-5*time.Minute), 1_000_000), bar(now, 2_500_000),
	}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
}

// Regression for #617: screener.* runtime settings bypassed FR-FS-4's
// validation, so a bad DB value silently disabled the filters or emptied the
// candidate list.
func TestRefresh_RejectsInvalidScreenerRuntimeSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]string
		wantKey  string
	}{
		{"top_n zero", map[string]string{"screener.top_n": "0"}, "top_n"},
		{"min_price zero", map[string]string{"screener.min_price": "0"}, "min_price"},
		{"negative max_spread_bps", map[string]string{"screener.max_spread_bps": "-1"}, "max_spread_bps"},
		{"max_price below min_price", map[string]string{"screener.max_price": "500", "screener.min_price": "1000"}, "max_price"},
		{"all weights zero", map[string]string{
			"screener.weights.volume_ratio": "0",
		}, "weights"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refresher := newTestRefresher(t)
			refresher.Strategy.FastScreener = validLenientFastScreener()
			ctx := context.Background()
			seedPassingStock(t, refresher)
			if err := refresher.Refresh(ctx); err != nil {
				t.Fatalf("baseline Refresh: %v", err)
			}
			for key, value := range tt.settings {
				if err := refresher.Settings.Set(ctx, key, value, time.Now().UTC()); err != nil {
					t.Fatalf("Settings.Set(%s): %v", key, err)
				}
			}

			err := refresher.Refresh(ctx)
			if err == nil {
				t.Fatalf("Refresh with %v returned nil error, want a validation error", tt.settings)
			}
			if !strings.Contains(err.Error(), "screener."+tt.wantKey) {
				t.Errorf("Refresh error = %q, want it to name screener.%s", err, tt.wantKey)
			}
			if candidates, _, _ := refresher.Screener.Candidates(ctx); len(candidates) != 1 {
				t.Errorf("candidates = %d after a rejected refresh, want the baseline 1 left untouched", len(candidates))
			}
		})
	}
}
