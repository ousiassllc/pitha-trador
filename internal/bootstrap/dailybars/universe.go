package dailybars

import (
	"slices"
	"sort"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// universe narrows the master's symbols by the Settings: the 市場区分, the
// price floor (the master's 前日終値; a symbol without one is kept) and the
// exclusion list. The result is sorted by symbol, which is the order the batch
// walks (and the order its cursor refers to).
func universe(targets []domain.DailyBarTarget, s tachibanasource.TachibanaNightlySettings) []string {
	seen := make(map[string]bool, len(targets))
	var symbols []string
	for _, t := range targets {
		switch {
		case t.Symbol == "" || seen[t.Symbol]:
			continue
		case !slices.Contains(s.Markets, t.Market):
			continue
		case s.MinPriceJPY > 0 && t.PrevClose > 0 && t.PrevClose < float64(s.MinPriceJPY):
			continue
		case slices.Contains(s.ExcludeSymbols, t.Symbol):
			continue
		}
		seen[t.Symbol] = true
		symbols = append(symbols, t.Symbol)
	}
	sort.Strings(symbols)
	return symbols
}
