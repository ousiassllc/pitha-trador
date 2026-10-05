package performance

import (
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/insight"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// The Performance page's Templ components take plain props, so a change to
// the internal/service result types surfaces here (docs/components/
// overview.md §3) rather than as a Templ compile error.

func performanceActuals(p insight.Performance) organisms.PerformanceActuals {
	return organisms.PerformanceActuals{
		TotalPnL:               p.TotalPnL,
		DailyPnL:               p.DailyPnL,
		TradeCount:             p.TradeCount,
		WinRate:                p.WinRate,
		ProfitFactor:           p.ProfitFactor,
		Expectancy:             p.Expectancy,
		MaxDrawdownPct:         p.MaxDrawdownPct,
		AverageHoldTimeMinutes: p.AverageHoldTimeMinutes,
		SharpeRef:              p.SharpeRef,
		SortinoRef:             p.SortinoRef,
		SignalCount:            p.SignalCount,
	}
}

func performanceSummary(m backtest.Metrics) organisms.PerformanceSummary {
	return organisms.PerformanceSummary{
		TradeCount:     m.TradeCount,
		WinRate:        m.WinRate,
		AvgProfit:      m.AvgProfit,
		AvgLoss:        m.AvgLoss,
		ProfitFactor:   m.ProfitFactor,
		Expectancy:     m.Expectancy,
		MaxDrawdownPct: m.MaxDrawdownPct,
		GrossPnLPct:    m.GrossPnLPct,
		SlippagePnLPct: m.SlippagePnLPct,
		FeePnLPct:      m.FeePnLPct,
		NetPnLPct:      m.NetPnLPct,
	}
}

func performanceResult(r backtest.Result) *pages.PerformanceResult {
	folds := make([]pages.PerformanceFold, len(r.Splits))
	for i, sp := range r.Splits {
		folds[i] = pages.PerformanceFold{
			ForwardStart: sp.Split.Forward.Start,
			ForwardEnd:   sp.Split.Forward.End,
			Forward:      performanceSummary(sp.Forward),
		}
	}
	return &pages.PerformanceResult{Combined: performanceSummary(r.Combined), Folds: folds}
}
