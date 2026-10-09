package tachibanawatch_test

import (
	"fmt"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/tachibanawatch"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func dailyBars(symbol string, closes, volumes []float64) []domain.DailyBar {
	var bars []domain.DailyBar
	for i, c := range closes {
		bars = append(bars, domain.DailyBar{
			Symbol: symbol, TradeDate: fmt.Sprintf("2026-09-%02d", i+1),
			Close: c, Volume: volumes[i], AdjHigh: c * 1.02, AdjLow: c * 0.98, AdjClose: c, AdjVolume: volumes[i],
		})
	}
	return bars
}

func constant(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func indicator(name string, weight float64, topN int) tachibanasource.TachibanaScreenIndicator {
	return tachibanasource.TachibanaScreenIndicator{Name: name, Weight: weight, TopN: topN}
}

func TestScreen_RanksEachIndicatorOnTheBasisDay(t *testing.T) {
	last := "2026-09-10"
	closes := func(prev, today float64) []float64 { return append(constant(9, prev), today) }
	bySymbol := map[string][]domain.DailyBar{
		"UP":   dailyBars("UP", closes(100, 110), constant(10, 1000)),
		"DOWN": dailyBars("DOWN", closes(100, 90), constant(10, 1000)),
		"FLAT": dailyBars("FLAT", closes(100, 100), append(constant(9, 1000), 5000)),
		"OLD":  dailyBars("OLD", closes(100, 200), constant(10, 1000))[:9], // no bar on the basis day
	}

	byIndicator := func(name string, topN int) []string {
		var out []string
		for _, p := range tachibanawatch.Screen(bySymbol, last, []tachibanasource.TachibanaScreenIndicator{indicator(name, 1, topN)}) {
			out = append(out, p.Symbol)
		}
		return out
	}
	if got := fmt.Sprint(byIndicator(tachibanasource.ScreenGainRate, 5)); got != "[UP]" {
		t.Errorf("gain_rate = %s, want only the riser (OLD has no bar on the basis day)", got)
	}
	if got := fmt.Sprint(byIndicator(tachibanasource.ScreenLossRate, 5)); got != "[DOWN]" {
		t.Errorf("loss_rate = %s, want only the faller", got)
	}
	if got := fmt.Sprint(byIndicator(tachibanasource.ScreenVolumeSurge, 1)); got != "[FLAT]" {
		t.Errorf("volume_surge = %s, want the symbol whose volume jumped (the others are at their average)", got)
	}
	if got := fmt.Sprint(byIndicator(tachibanasource.ScreenVolume, 5)); got != "[FLAT DOWN UP]" {
		t.Errorf("volume = %s, want FLAT first (largest), then ties by symbol", got)
	}
	if got := byIndicator(tachibanasource.ScreenRangeRate, 5); len(got) != 3 {
		t.Errorf("range_rate = %v, want the three symbols with a bar on the basis day", got)
	}
}

func TestScreen_UnionIsOrderedByWeightedScoreAndRecordsIndicators(t *testing.T) {
	closes := func(prev, today float64) []float64 { return append(constant(9, prev), today) }
	bySymbol := map[string][]domain.DailyBar{
		"BOTH": dailyBars("BOTH", closes(100, 120), append(constant(9, 1000), 9000)),
		"RISE": dailyBars("RISE", closes(100, 105), constant(10, 1000)),
		"VOL":  dailyBars("VOL", closes(100, 100), append(constant(9, 1000), 4000)),
	}
	picks := tachibanawatch.Screen(bySymbol, "2026-09-10", []tachibanasource.TachibanaScreenIndicator{
		indicator(tachibanasource.ScreenGainRate, 1, 2), indicator(tachibanasource.ScreenVolume, 1, 2),
		indicator(tachibanasource.ScreenLossRate, 5, 0), // TopN 0: not used
	})
	if len(picks) != 3 {
		t.Fatalf("picks = %+v, want BOTH, RISE and VOL", picks)
	}
	if picks[0].Symbol != "BOTH" || fmt.Sprint(picks[0].Indicators) != "[gain_rate volume]" {
		t.Errorf("first pick = %+v, want BOTH selected by both indicators", picks[0])
	}
	if picks[0].Score <= picks[1].Score {
		t.Errorf("scores %v, %v: the symbol on two lists must score higher", picks[0].Score, picks[1].Score)
	}

	heavy := tachibanawatch.Screen(bySymbol, "2026-09-10", []tachibanasource.TachibanaScreenIndicator{
		indicator(tachibanasource.ScreenGainRate, 1, 3), indicator(tachibanasource.ScreenVolume, 10, 3),
	})
	if heavy[0].Symbol != "BOTH" || heavy[1].Symbol != "VOL" {
		t.Errorf("a heavier volume weight must put VOL before RISE: %+v", heavy)
	}
}

func TestScreen_SurgeNeedsEnoughHistory(t *testing.T) {
	bySymbol := map[string][]domain.DailyBar{
		"NEW": dailyBars("NEW", constant(3, 100), []float64{1000, 1000, 9000}),
	}
	if picks := tachibanawatch.Screen(bySymbol, "2026-09-03", []tachibanasource.TachibanaScreenIndicator{indicator(tachibanasource.ScreenVolumeSurge, 1, 5)}); len(picks) != 0 {
		t.Errorf("a symbol with 2 earlier days has no average to compare with: %+v", picks)
	}
}
