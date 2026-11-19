package backtestlab

import (
	"cmp"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/actuals"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/sensitivity"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
)

// CILevel is the confidence level of ExpectancyCI (actuals.ClusterMean's 95%).
const CILevel = 0.95

// Result is what a run records: the result of every variant, in request order.
type Result struct {
	Variants []VariantResult `json:"variants"`
}

// Coverage is the part of the requested period the stored data actually
// covers: the first and last snapshot the replay saw (nil when there is none).
type Coverage struct {
	FirstSnapshotAt *time.Time
	LastSnapshotAt  *time.Time
}

// VariantResult is the cost-inclusive result of one parameter set. Returns are
// percentages of the trade's price (1.0 = 1%), summed in the day and PnL
// fields, as backtest.Metrics reports them.
type VariantResult struct {
	Index  int            `json:"index"`
	Label  string         `json:"label"`
	Params variant.Params `json:"params"`

	TradeCount int `json:"trade_count"`
	// WinRate (0..1) and ExpectancyPct (the mean net return per trade, after
	// slippage, spread and fees) are null without trades.
	WinRate       *float64 `json:"win_rate"`
	ExpectancyPct *float64 `json:"expectancy_pct"`
	// MaxDrawdownPct is the largest peak-to-trough fall of the cumulative net
	// return, trades in exit order.
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	GrossPnLPct    float64 `json:"gross_pnl_pct"`
	SlippagePnLPct float64 `json:"slippage_pnl_pct"`
	FeePnLPct      float64 `json:"fee_pnl_pct"`
	NetPnLPct      float64 `json:"net_pnl_pct"`

	// ExpectancyCI is the 95% interval of the expectancy, clustered by JST exit
	// day (trades of one day are not independent).
	ExpectancyCI ExpectancyCI `json:"expectancy_ci"`
	// Days is the per-JST-exit-day result, oldest first.
	Days        []Day            `json:"days"`
	Sensitivity []SensitivityRow `json:"sensitivity"`
	// IsReferenceBest marks the variant with the highest expectancy among those
	// that traded. It is a reference only (ReferenceCaution).
	IsReferenceBest bool `json:"is_reference_best"`
}

// ExpectancyCI is the interval of the mean net return per trade. Lo and Hi are
// null unless HasCI (fewer than two exit days have none).
type ExpectancyCI struct {
	Lo    *float64 `json:"lo"`
	Hi    *float64 `json:"hi"`
	Level float64  `json:"level"`
	// Days is the number of JST exit days the trades fall on (the clusters).
	Days  int  `json:"days"`
	HasCI bool `json:"has_ci"`
}

// Day is one JST exit day: its trade count and the sum of the net returns.
type Day struct {
	Date       string  `json:"date"`
	TradeCount int     `json:"trade_count"`
	NetPnLPct  float64 `json:"net_pnl_pct"`
}

// SensitivityRow is one extra-cost scenario (sensitivity.Row): every trade
// re-priced with ExtraCostBps more round-trip cost.
type SensitivityRow struct {
	ExtraCostBps  float64 `json:"extra_cost_bps"`
	ExpectancyPct float64 `json:"expectancy_pct"`
	ExpectancyBps float64 `json:"expectancy_bps"`
	// PositiveShare is the share (0..1) of trades still above zero.
	PositiveShare float64 `json:"positive_share"`
	// PredictedEVTradeCount / PredictedEVPositiveShare: see sensitivity.Row.
	PredictedEVTradeCount    int     `json:"predicted_ev_trade_count"`
	PredictedEVPositiveShare float64 `json:"predicted_ev_positive_share"`
}

// summarize turns the trades of one variant into its VariantResult. It sorts a
// copy into a total order first, so the sums, the day series and the interval
// depend on the set of trades only, never on the engine's output order.
func summarize(index int, v Variant, trades []backtest.Trade, extra []float64) VariantResult {
	sorted := slices.Clone(trades)
	slices.SortFunc(sorted, compareTrades)
	m := backtest.Aggregate(sorted)
	r := VariantResult{
		Index: index, Label: v.Label, Params: v.Params, TradeCount: m.TradeCount,
		MaxDrawdownPct: m.MaxDrawdownPct, GrossPnLPct: m.GrossPnLPct, SlippagePnLPct: m.SlippagePnLPct,
		FeePnLPct: m.FeePnLPct, NetPnLPct: m.NetPnLPct,
		Days: []Day{}, Sensitivity: []SensitivityRow{},
	}
	if m.TradeCount > 0 {
		r.WinRate, r.ExpectancyPct = &m.WinRate, &m.Expectancy
	}
	values, days := make([]float64, len(sorted)), make([]int64, len(sorted))
	for i, t := range sorted {
		values[i], days[i] = t.NetReturnPct, actuals.JSTDay(t.ExitTimestamp)
		if n := len(r.Days); n > 0 && r.Days[n-1].Date == dayText(days[i]) {
			r.Days[n-1].TradeCount++
			r.Days[n-1].NetPnLPct += t.NetReturnPct
			continue
		}
		r.Days = append(r.Days, Day{Date: dayText(days[i]), TradeCount: 1, NetPnLPct: t.NetReturnPct})
	}
	ci := actuals.ClusterMean(values, days)
	r.ExpectancyCI = ExpectancyCI{Level: CILevel, Days: len(r.Days), HasCI: ci.HasCI}
	if ci.HasCI {
		r.ExpectancyCI.Lo, r.ExpectancyCI.Hi = &ci.Lo, &ci.Hi
	}
	for _, row := range sensitivity.Table(sorted, extra) {
		r.Sensitivity = append(r.Sensitivity, SensitivityRow{
			ExtraCostBps: row.ExtraCostBps, ExpectancyPct: row.ExpectancyPct, ExpectancyBps: row.ExpectancyBps,
			PositiveShare: row.PositiveShare, PredictedEVTradeCount: row.PredictedEVTradeCount,
			PredictedEVPositiveShare: row.PredictedEVPositiveShare,
		})
	}
	return r
}

// markReferenceBest flags the variant with the highest expectancy among those
// that traded (the earliest on a tie). No variant traded: none is flagged.
func markReferenceBest(variants []VariantResult) {
	best := -1
	for i, v := range variants {
		if v.ExpectancyPct != nil && (best < 0 || *v.ExpectancyPct > *variants[best].ExpectancyPct) {
			best = i
		}
	}
	if best >= 0 {
		variants[best].IsReferenceBest = true
	}
}

// compareTrades is a total order: exit time, then the cost-inclusive return
// (highest first, as backtest.Aggregate does), then the identity of the trade.
func compareTrades(a, b backtest.Trade) int {
	return cmp.Or(
		a.ExitTimestamp.Compare(b.ExitTimestamp),
		cmp.Compare(b.NetReturnPct, a.NetReturnPct),
		a.EntryTimestamp.Compare(b.EntryTimestamp),
		cmp.Compare(a.Symbol, b.Symbol),
		cmp.Compare(a.InstrumentID, b.InstrumentID),
		cmp.Compare(a.Direction, b.Direction),
		cmp.Compare(a.ExitReason, b.ExitReason),
	)
}

// dayText renders an actuals.JSTDay number as its JST date.
func dayText(day int64) string {
	return time.Unix(day*24*60*60, 0).UTC().Format(time.DateOnly)
}
