package researchquotes

import (
	"context"
	"fmt"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

type fakeBars struct {
	bars  []domain.DailyBar
	since string
}

func (f *fakeBars) Recent(_ context.Context, since string, _ int) ([]domain.DailyBar, error) {
	f.since = since
	return f.bars, nil
}

func bar(symbol, date string, closePrice float64) domain.DailyBar {
	return domain.DailyBar{Symbol: symbol, TradeDate: date, Close: closePrice, AdjClose: closePrice, AdjHigh: closePrice, AdjLow: closePrice, Volume: 1000, AdjVolume: 1000}
}

// The screen ranks the bars of the 立会日 before today (Friday 2026-10-09 asks
// about Thursday 10-08), skips the nightly batch's exclusions and returns best first.
func TestScreenSymbolsRanksThePreviousTradingDayExcludingConfiguredSymbols(t *testing.T) {
	bars := &fakeBars{bars: []domain.DailyBar{
		bar("UP", "2026-10-07", 100), bar("UP", "2026-10-08", 110), // +10 %
		bar("TOP", "2026-10-07", 100), bar("TOP", "2026-10-08", 120), // +20 % but excluded
		bar("MID", "2026-10-07", 100), bar("MID", "2026-10-08", 105),
		bar("STALE", "2026-10-06", 100), bar("STALE", "2026-10-07", 300), // no bar on the basis day
	}}
	set := tachibanasource.BuildTachibanaSource(nil)
	set.Screen = []tachibanasource.TachibanaScreenIndicator{{Name: tachibanasource.ScreenGainRate, Weight: 1, TopN: 5}}
	set.Nightly.ExcludeSymbols = []string{"TOP"}

	screen := ScreenSymbols(bars)
	got, err := screen(context.Background(), set, thursday(8, 50, 0).AddDate(0, 0, 1)) // Friday 2026-10-09
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != "[UP MID]" {
		t.Errorf("picks = %v, want [UP MID] (best first, TOP excluded, STALE has no bar on the basis day)", got)
	}
	if bars.since == "" {
		t.Error("the bars were not read from a bounded window")
	}

	// Tuesday 2026-10-13 follows the 体育の日 long weekend (10-12): the basis is Friday 10-09, which has no bars.
	got, err = screen(context.Background(), set, thursday(8, 50, 0).AddDate(0, 0, 5))
	if err != nil || len(got) != 0 {
		t.Errorf("Tuesday after the long weekend = %v, %v, want no picks (no bar on Friday)", got, err)
	}
}
