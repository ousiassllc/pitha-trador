package tachibanawatch

import (
	"sort"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

const (
	// surgeWindow is the number of 営業日 the surge indicators average over
	// (過去20営業日平均との比).
	surgeWindow = 20
	// minSurgeHistory is the fewest earlier days a surge ratio needs; a
	// younger listing has no meaningful average.
	minSurgeHistory = 5
	// barsPerSymbol is how many of a symbol's latest bars the screening reads:
	// the newest day, the day before it and the surge window.
	barsPerSymbol = surgeWindow + 1
)

// Pick is a symbol the screening selected and why.
type Pick struct {
	Symbol string
	// Indicators are the indicators (tachibanasource.Screen*) whose top list
	// holds the symbol, in the configured order.
	Indicators []string
	// Score orders the picks: the sum, over the indicators that selected the
	// symbol, of weight × (TopN − rank) / TopN (rank 0 = best).
	Score float64
}

// Screen is the 日足スクリーニング: for every active indicator it ranks the
// symbols whose newest bar is on basis and takes the indicator's TopN; the
// union is ordered by weighted score (best first, ties by symbol). bySymbol
// holds each symbol's bars, oldest first. Rates and surges use the
// split-adjusted values (consistent across days), 売買高 and 売買代金 the day's
// raw ones. The broker's daily history has no 売買代金: it is 終値 × 出来高.
func Screen(bySymbol map[string][]domain.DailyBar, basis string, indicators []tachibanasource.TachibanaScreenIndicator) []Pick {
	metrics := make(map[string]metric, len(bySymbol))
	for symbol, bars := range bySymbol {
		if m, ok := metricsOf(bars, basis); ok {
			metrics[symbol] = m
		}
	}

	picks := make(map[string]*Pick)
	for _, ind := range indicators {
		if !ind.Active() {
			continue
		}
		type ranked struct {
			symbol string
			value  float64
		}
		var rows []ranked
		for symbol, m := range metrics {
			if v, ok := m.value(ind.Name); ok {
				rows = append(rows, ranked{symbol, v})
			}
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].value != rows[j].value {
				return rows[i].value > rows[j].value
			}
			return rows[i].symbol < rows[j].symbol
		})
		for rank, row := range rows[:min(ind.TopN, len(rows))] {
			p := picks[row.symbol]
			if p == nil {
				p = &Pick{Symbol: row.symbol}
				picks[row.symbol] = p
			}
			p.Indicators = append(p.Indicators, ind.Name)
			p.Score += ind.Weight * float64(ind.TopN-rank) / float64(ind.TopN)
		}
	}

	out := make([]Pick, 0, len(picks))
	for _, p := range picks {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out
}

// metric is one symbol's value per indicator; a missing one is not present.
type metric map[string]float64

func (m metric) value(indicator string) (float64, bool) {
	v, ok := m[indicator]
	return v, ok
}

// metricsOf derives the indicator values of the newest bar in bars, which
// must be dated basis; ok is false for a symbol without a bar on basis.
func metricsOf(bars []domain.DailyBar, basis string) (metric, bool) {
	if len(bars) == 0 || bars[len(bars)-1].TradeDate != basis {
		return nil, false
	}
	last := bars[len(bars)-1]
	m := metric{}
	if last.Volume > 0 {
		m[tachibanasource.ScreenVolume] = last.Volume
		if last.Close > 0 {
			m[tachibanasource.ScreenTurnover] = last.Close * last.Volume
		}
	}
	if len(bars) < 2 {
		return m, true
	}
	prev := bars[len(bars)-2]
	if prev.AdjClose > 0 && last.AdjClose > 0 {
		rate := last.AdjClose/prev.AdjClose - 1
		if rate > 0 {
			m[tachibanasource.ScreenGainRate] = rate
		}
		if rate < 0 {
			m[tachibanasource.ScreenLossRate] = -rate
		}
		if last.AdjHigh >= last.AdjLow && last.AdjHigh > 0 {
			m[tachibanasource.ScreenRangeRate] = (last.AdjHigh - last.AdjLow) / prev.AdjClose
		}
	}

	history := bars[:len(bars)-1]
	history = history[max(0, len(history)-surgeWindow):]
	if len(history) >= minSurgeHistory {
		var volume, turnover float64
		for _, b := range history {
			volume += b.AdjVolume
			turnover += b.AdjClose * b.AdjVolume
		}
		n := float64(len(history))
		if volume > 0 && last.AdjVolume > 0 {
			m[tachibanasource.ScreenVolumeSurge] = last.AdjVolume / (volume / n)
		}
		if turnover > 0 && last.AdjClose*last.AdjVolume > 0 {
			m[tachibanasource.ScreenTurnoverSurge] = last.AdjClose * last.AdjVolume / (turnover / n)
		}
	}
	return m, true
}
