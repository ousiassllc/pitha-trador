package candidates

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

type fakeWatch map[string]bool

func (w fakeWatch) Contains(symbol string) bool { return w[symbol] }

// With the ranking watch list (the default), only watched symbols are
// screened; when the list empties (empty/failed ranking) no stale candidate
// stays published.
func TestRefresh_ScreensOnlyWatchedSymbolsAndEmptyListPublishesNoCandidates(t *testing.T) {
	refresher := newTestRefresher(t)
	refresher.Strategy.FastScreener = config.FastScreenerConfig{
		MinPrice: 0, MaxPrice: 1_000_000, MaxSpreadBps: 100, TopN: 10,
	}
	for _, sym := range []string{"7203", "6758"} {
		inst := mustCreateInstrument(t, refresher, sym)
		if _, err := refresher.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{{
			InstrumentID: inst.ID, Symbol: sym, Timestamp: time.Now().UTC(),
			Price: 2500, Volume: 1000, Turnover: 2_500_000, SpreadBps: ptrF(10),
			Feature: domain.Feature{VWAP: 2490, PriceVsVWAPBps: 40, VolumeRatio5m: ptrF(1.5), Return5m: ptrF(0.5), RealizedVol5m: ptrF(0.01)},
		}}); err != nil {
			t.Fatalf("InsertBatch: %v", err)
		}
	}
	candidates := func() []string {
		t.Helper()
		if err := refresher.Refresh(context.Background()); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		got, _, err := refresher.Screener.Candidates(context.Background())
		if err != nil {
			t.Fatalf("Candidates: %v", err)
		}
		syms := make([]string, len(got))
		for i, c := range got {
			syms[i] = c.Symbol
		}
		return syms
	}

	refresher.Watch = fakeWatch{"7203": true}
	if got := candidates(); len(got) != 1 || got[0] != "7203" {
		t.Fatalf("candidates = %v, want only the watched 7203", got)
	}
	refresher.Watch = fakeWatch{} // empty or failed ranking, nothing held
	if got := candidates(); len(got) != 0 {
		t.Fatalf("candidates = %v, want none once the watch list is empty", got)
	}
	refresher.Watch = nil // no watch list (full scan): everything is screened
	if got := candidates(); len(got) != 2 {
		t.Fatalf("candidates = %v, want both without a watch list", got)
	}
}
