package insight

import (
	"math"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// jst is the Tokyo Stock Exchange's time zone, the boundary of
// Performance.DailyPnL's "today". Japan has no DST, so a fixed zone is
// exact and needs no tzdata (absent on some Windows installs).
var jst = time.FixedZone("JST", 9*60*60)

// Performance is `GET /api/v1/performance`'s realized-trade summary
// (docs/api/endpoints.md §5), aggregated over every closed positions row.
//
// TotalPnL/DailyPnL are currency amounts (sum of realized_pnl). The
// remaining ratios follow internal/service/backtest.Aggregate's
// definitions (WinRate, Expectancy, MaxDrawdownPct) over each position's
// direction-adjusted return on its entry notional, in percent, so live
// numbers are directly comparable with the Performance page's backtest
// numbers. ProfitFactor is the currency-based gross profit / gross loss.
type Performance struct {
	TotalPnL float64
	// DailyPnL sums realized_pnl of positions closed since 00:00 JST of
	// the calendar day containing `now`.
	DailyPnL float64
	// TradeCount is the number of closed positions aggregated.
	TradeCount int
	WinRate    float64
	// ProfitFactor is nil when undefined: no closed positions, or no
	// losing one (an infinite ratio has no JSON representation).
	ProfitFactor           *float64
	Expectancy             float64
	MaxDrawdownPct         float64
	AverageHoldTimeMinutes float64
	// SharpeRef is the per-trade (not annualized) mean return divided by
	// its sample standard deviation; nil with fewer than 2 trades or zero
	// deviation. It is a reference figure only.
	SharpeRef *float64
	// SortinoRef is the per-trade mean return divided by its downside
	// deviation (root mean square of negative returns over all trades);
	// nil when there is no losing trade or no trade at all.
	SortinoRef *float64
	// SignalCount is the number of LONG/SHORT trade_signals rows.
	SignalCount int64
}

// Aggregate summarizes closed (positions with ClosedAt set;
// open ones and ones without RealizedPnL are ignored) as of now.
// signalCount is passed through into Performance.SignalCount.
func Aggregate(closed []domain.Position, signalCount int64, now time.Time) Performance {
	perf := Performance{SignalCount: signalCount}

	y, m, d := now.In(jst).Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, jst)

	var (
		trades          []backtest.Trade
		returns         []float64
		sumWin, sumLoss float64 // sumLoss <= 0
		holdMinutes     float64
	)
	for _, p := range closed {
		if p.ClosedAt == nil || p.RealizedPnL == nil {
			continue
		}
		pnl := *p.RealizedPnL
		perf.TotalPnL += pnl
		if !p.ClosedAt.Before(dayStart) {
			perf.DailyPnL += pnl
		}
		switch {
		case pnl > 0:
			sumWin += pnl
		case pnl < 0:
			sumLoss += pnl
		}
		holdMinutes += p.ClosedAt.Sub(p.OpenedAt).Minutes()

		var ret float64
		if notional := p.EntryPrice * float64(p.Quantity); notional > 0 {
			ret = pnl / notional * 100
		}
		returns = append(returns, ret)
		trades = append(trades, backtest.Trade{
			Symbol: p.Symbol, Direction: p.Side,
			EntryTimestamp: p.OpenedAt, ExitTimestamp: *p.ClosedAt,
			GrossReturnPct: ret, NetReturnPct: ret,
		})
	}

	perf.TradeCount = len(trades)
	if perf.TradeCount == 0 {
		return perf
	}

	agg := backtest.Aggregate(trades)
	perf.WinRate = agg.WinRate
	perf.Expectancy = agg.Expectancy
	perf.MaxDrawdownPct = agg.MaxDrawdownPct
	perf.AverageHoldTimeMinutes = holdMinutes / float64(perf.TradeCount)
	if sumLoss < 0 {
		pf := sumWin / -sumLoss
		perf.ProfitFactor = &pf
	}
	perf.SharpeRef, perf.SortinoRef = returnRatios(returns)
	return perf
}

// returnRatios computes Performance.SharpeRef/SortinoRef over per-trade
// returns.
func returnRatios(returns []float64) (sharpe, sortino *float64) {
	n := float64(len(returns))
	var sum float64
	for _, r := range returns {
		sum += r
	}
	mean := sum / n

	if len(returns) >= 2 {
		var sq float64
		for _, r := range returns {
			sq += (r - mean) * (r - mean)
		}
		if std := math.Sqrt(sq / (n - 1)); std > 0 {
			v := mean / std
			sharpe = &v
		}
	}

	var downSq float64
	for _, r := range returns {
		if r < 0 {
			downSq += r * r
		}
	}
	if dd := math.Sqrt(downSq / n); dd > 0 {
		v := mean / dd
		sortino = &v
	}
	return sharpe, sortino
}
