package researchquotes

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/tachibanawatch"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

const (
	// screenHistoryDays is how far back (calendar days) the screening reads
	// bars, and screenBarsPerSymbol how many per symbol: the watch list's own
	// screening window (the newest day, the day before and the surge history).
	screenHistoryDays   = 45
	screenBarsPerSymbol = 21
)

// ScreenSymbols returns the symbols the 日足スクリーニング ranks highest on the
// 立会日 before now's JST date, best first: the daily-screen extra source of
// the 時価 recording. It reads the stored daily bars only (nothing is requested
// from a broker) with the screening indicators of settings, minus the
// nightly batch's excluded symbols. No bars of that day, no symbols.
func ScreenSymbols(bars tachibanawatch.Bars) func(ctx context.Context, settings tachibanasource.TachibanaSourceSettings, now time.Time) ([]string, error) {
	return func(ctx context.Context, settings tachibanasource.TachibanaSourceSettings, now time.Time) ([]string, error) {
		prev, ok := marketcalendar.TSE.PreviousTradingDay(now)
		if !ok {
			return nil, nil
		}
		basis := prev.In(tachibana.JST).Format(time.DateOnly)
		since := prev.AddDate(0, 0, -screenHistoryDays).Format(time.DateOnly)
		recent, err := bars.Recent(ctx, since, screenBarsPerSymbol)
		if err != nil {
			return nil, fmt.Errorf("researchquotes: daily bars for the screening: %w", err)
		}
		bySymbol := make(map[string][]domain.DailyBar)
		for _, b := range recent {
			if !slices.Contains(settings.Nightly.ExcludeSymbols, b.Symbol) {
				bySymbol[b.Symbol] = append(bySymbol[b.Symbol], b)
			}
		}
		picks := tachibanawatch.Screen(bySymbol, basis, settings.Screen)
		out := make([]string, len(picks))
		for i, p := range picks {
			out[i] = p.Symbol
		}
		return out, nil
	}
}
