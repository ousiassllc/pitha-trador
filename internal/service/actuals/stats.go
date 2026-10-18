package actuals

import repo "github.com/ousiassllc/pitha-trador/internal/repository/actuals"

// summarize computes Stats of trades, which MUST be ordered by ClosedAt: the
// drawdown follows the cumulative PnL in that order. capital is the risk
// config's initial_capital (<= 0: unset, the capital-ratio drawdown is then
// undefined).
func summarize(trades []repo.Trade, capital float64, c Criteria) Stats {
	s := Stats{N: len(trades)}
	var (
		pnl, bps, win []float64
		days          []int64
	)
	for _, t := range trades {
		d := jstDay(t.ClosedAt)
		days = append(days, d)
		pnl = append(pnl, t.RealizedPnL)
		bps = append(bps, returnBps(t))
		w := 0.0
		if t.RealizedPnL > 0 {
			w = 1
			s.Wins++
		}
		win = append(win, w)

		s.NetPnLJPY += t.RealizedPnL
		s.FeesJPY += t.EntryFee + t.ExitFee
		entry, exit := slippageCosts(t)
		s.SlippageJPY += entry.jpy + exit.jpy
		s.SlippageRecordedFill += entry.recorded + exit.recorded
	}
	s.Days = distinctDays(days)
	s.GrossBeforeFeesJPY = s.NetPnLJPY + s.FeesJPY
	s.GrossBeforeCostsJPY = s.GrossBeforeFeesJPY + s.SlippageJPY

	s.WinRate = proportionMetric(win, days, c)
	s.EVJPY = meanMetric(pnl, days, c)
	s.EVBps = meanMetric(bps, days, c)
	s.ProfitFactor = bootstrapMetric(pnl, days, profitFactor, c)
	s.MaxDrawdownJPY = bootstrapMetric(pnl, days, func(p []float64) (float64, bool) { return maxDrawdown(p), true }, c)
	s.MaxDrawdownPct = drawdownPct(s.MaxDrawdownJPY, capital)
	return s
}

// drawdownPct expresses the JPY drawdown as a percentage of capital; with no
// capital set it is the empty (undefined, unreliable) metric.
func drawdownPct(dd Metric, capital float64) Metric {
	if capital <= 0 {
		return Metric{N: dd.N}
	}
	return scaled(dd, 100/capital)
}

// returnBps is the net return on the entry notional in bps; 0 when the
// notional is not positive (as Performance's per-trade return does).
func returnBps(t repo.Trade) float64 {
	notional := t.EntryPrice * float64(t.Quantity)
	if notional <= 0 {
		return 0
	}
	return t.RealizedPnL / notional * 10_000
}

// profitFactor is gross profit over gross loss; undefined without a loss.
func profitFactor(pnl []float64) (float64, bool) {
	var profit, loss float64
	for _, p := range pnl {
		if p > 0 {
			profit += p
		} else {
			loss += p
		}
	}
	if loss >= 0 {
		return 0, false
	}
	return profit / -loss, true
}

// maxDrawdown is the largest fall of the cumulative PnL below its running
// peak, the curve starting (and the peak starting) at 0: a loss before any
// profit is a drawdown from the initial capital.
func maxDrawdown(pnl []float64) float64 {
	var cum, peak, dd float64
	for _, p := range pnl {
		cum += p
		peak = max(peak, cum)
		dd = max(dd, peak-cum)
	}
	return dd
}

func distinctDays(days []int64) int {
	n := 0
	for i, d := range days {
		if i == 0 || d != days[i-1] {
			n++
		}
	}
	return n
}

// fillCost is the slippage of one fill in JPY and whether it was recorded.
type fillCost struct {
	jpy      float64
	recorded int
}

// slippageCosts converts the entry and exit fills' slippage_bps into JPY.
// slippage_bps is how many bps worse than the reference price the fill was
// (fillmodel.SlippageBps), so for a buy (price-ref)/ref*1e4 = bps and for a
// sell (ref-price)/ref*1e4 = bps, whence the cost qty*|price-ref| from the
// fill price. A LONG position buys to enter and sells to exit; a SHORT the
// reverse. A fill without a recorded slippage or price costs 0 here.
func slippageCosts(t repo.Trade) (entry, exit fillCost) {
	buyEntry := t.Side != "SHORT"
	entry = fillSlippage(t.EntryPrice, t.Quantity, t.EntrySlippageBps, buyEntry)
	if t.ExitPrice != nil {
		exit = fillSlippage(*t.ExitPrice, t.Quantity, t.ExitSlippageBps, !buyEntry)
	}
	return entry, exit
}

func fillSlippage(price float64, qty int64, bps *float64, buy bool) fillCost {
	if bps == nil {
		return fillCost{}
	}
	b := *bps / 10_000
	denom := 1 + b // buy: price = ref*(1+b)
	if !buy {
		denom = 1 - b // sell: price = ref*(1-b)
	}
	if denom <= 0 {
		return fillCost{recorded: 1}
	}
	return fillCost{jpy: float64(qty) * price * b / denom, recorded: 1}
}
