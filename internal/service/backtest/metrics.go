package backtest

import (
	"math"
	"sort"
	"time"
)

// Exit reasons a Trade's ExitReason field can hold.
const (
	ExitReasonStopLoss   = "stop_loss"
	ExitReasonTakeProfit = "take_profit"
	ExitReasonMaxHolding = "max_holding_time"
	// ExitReasonDataEnded means none of ExitRule's conditions triggered
	// before the available bars ran out (either the dataset's own end,
	// or - for a position opened near the very end of it - simply no
	// further bars exist yet to evaluate).
	ExitReasonDataEnded = "data_ended"
)

// Trade is one completed LONG/SHORT position the backtest replay opened
// and closed. Every value is a direction-adjusted percentage return
// (1.0 == +1%), not a currency amount: Risk Engine & Paper Trading
// Execution (issue #8) has not landed, so there is no position-sizing/
// capital model yet to convert into JPY - percentages are comparable
// across symbols/instruments regardless of position size, the same
// convention config/risk.yaml's own *_pct limits already use.
type Trade struct {
	InstrumentID int64
	Symbol       string
	// Direction is domain.JevDirectionLong or domain.JevDirectionShort.
	Direction string

	EntryTimestamp time.Time
	// EntryPrice/ExitPrice are the fill prices the fill model produced
	// (spread, slippage, tick grid, 寄り/引け applied), i.e. what a paper
	// position's EntryPrice and exit order's filled_price would be - not
	// the bars' last prices GrossReturnPct is computed from.
	EntryPrice    float64
	ExitTimestamp time.Time
	ExitPrice     float64
	ExitReason    string

	// GrossReturnPct is the raw price return between the entry and exit
	// bars' last prices, before any execution cost or fee.
	GrossReturnPct float64
	// SlippageReturnPct is the return between EntryPrice and ExitPrice:
	// GrossReturnPct after the execution costs (spread, slippage, tick
	// grid, 寄り/引けの板寄せ) fillmodel charges on both fills (FR-BT-1
	// "スリッページ込みPnL").
	SlippageReturnPct float64
	// FeeReturnPct subtracts fillmodel.Model.FeeBps (charged on both entry
	// and exit notional) from GrossReturnPct (FR-BT-1 "手数料込みPnL").
	FeeReturnPct float64
	// NetReturnPct combines SlippageReturnPct's price adjustment and
	// FeeReturnPct's fee deduction: the realistic "what this trade would
	// actually have returned" figure.
	NetReturnPct float64
}

// Metrics is one backtest run's aggregated trade statistics (FR-BT-1).
type Metrics struct {
	TradeCount int
	WinRate    float64

	// AvgProfit/AvgLoss are the mean GrossReturnPct of winning (>0) and
	// losing (<0) trades respectively (AvgLoss <= 0). Both are 0 when
	// there are no trades in that bucket.
	AvgProfit float64
	AvgLoss   float64

	// ProfitFactor is sum(winning GrossReturnPct) / -sum(losing
	// GrossReturnPct). +Inf when there are wins and no losses; 0 when
	// there are no trades at all.
	ProfitFactor float64
	// Expectancy is the mean GrossReturnPct across every trade.
	Expectancy float64
	// MaxDrawdownPct is the largest peak-to-trough decline of the
	// cumulative NetReturnPct equity curve, trades taken in
	// chronological (exit time) order.
	MaxDrawdownPct float64

	GrossPnLPct    float64
	SlippagePnLPct float64
	FeePnLPct      float64
	NetPnLPct      float64
}

// Aggregate computes Metrics over trades (FR-BT-1). trades need not be
// sorted; Aggregate sorts a copy by ExitTimestamp before computing
// MaxDrawdownPct's running equity curve.
func Aggregate(trades []Trade) Metrics {
	if len(trades) == 0 {
		return Metrics{}
	}

	sorted := make([]Trade, len(trades))
	copy(sorted, trades)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ExitTimestamp.Before(sorted[j].ExitTimestamp) })

	var (
		m                                     Metrics
		wins, losses                          int
		sumWin, sumLoss                       float64 // sumLoss <= 0
		sumGross, sumSlippage, sumFee, sumNet float64
		equity, peak, maxDD                   float64
	)
	m.TradeCount = len(sorted)

	for _, tr := range sorted {
		sumGross += tr.GrossReturnPct
		sumSlippage += tr.SlippageReturnPct
		sumFee += tr.FeeReturnPct
		sumNet += tr.NetReturnPct

		switch {
		case tr.GrossReturnPct > 0:
			wins++
			sumWin += tr.GrossReturnPct
		case tr.GrossReturnPct < 0:
			losses++
			sumLoss += tr.GrossReturnPct
		}

		equity += tr.NetReturnPct
		if equity > peak {
			peak = equity
		}
		if dd := peak - equity; dd > maxDD {
			maxDD = dd
		}
	}

	m.WinRate = float64(wins) / float64(m.TradeCount)
	if wins > 0 {
		m.AvgProfit = sumWin / float64(wins)
	}
	if losses > 0 {
		m.AvgLoss = sumLoss / float64(losses)
	}
	switch {
	case sumLoss < 0:
		m.ProfitFactor = sumWin / -sumLoss
	case sumWin > 0:
		m.ProfitFactor = math.Inf(1)
	}
	m.Expectancy = sumGross / float64(m.TradeCount)
	m.MaxDrawdownPct = maxDD
	m.GrossPnLPct = sumGross
	m.SlippagePnLPct = sumSlippage
	m.FeePnLPct = sumFee
	m.NetPnLPct = sumNet

	return m
}
