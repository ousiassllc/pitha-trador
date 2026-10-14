package calibration

import (
	"cmp"
	"context"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
)

// RegimeTradeSource lists the closed positions with the A/B arm and market
// regime of the decision that opened them (*decisiontrade.Repository). A
// TradeSource that also implements it enables CalibrationMetrics.ABArms.
type RegimeTradeSource interface {
	ListRegimeTrades(ctx context.Context) ([]marketregime.Trade, error)
}

// armOrder fixes the order the arms are reported in: the control arm first.
var armOrder = []string{marketregime.ArmMomentum, marketregime.ArmRegime}

// regimeOrder fixes the order of an arm's regime breakdown.
var regimeOrder = []string{marketregime.Trend, marketregime.MeanRevert, marketregime.Unknown}

// accumulator sums closed positions: their count, wins, return after costs and
// PnL.
type accumulator struct {
	count, wins int
	returnPct   float64
	pnl         float64
}

func (a *accumulator) add(t marketregime.Trade) {
	a.count++
	if t.RealizedPnL > 0 {
		a.wins++
	}
	a.returnPct += t.ReturnPct
	a.pnl += t.RealizedPnL
}

// stats converts a to RegimeStats for regime: the win rate is the share with
// a positive realized PnL, EVNetBps the mean return after costs in bps (a
// return of 1% is 100 bps); all 0 for an empty group.
func (a accumulator) stats(regime string) marketregime.RegimeStats {
	s := marketregime.RegimeStats{Regime: regime, TradeCount: a.count, TotalPnL: a.pnl}
	if a.count > 0 {
		n := float64(a.count)
		s.WinRate = float64(a.wins) / n
		s.EVNetBps = a.returnPct / n * 100
	}
	return s
}

// ABArms aggregates trades into the A/B comparison of issue #755: for each arm
// (the fixed momentum strategy and the regime-switching one) the closed
// positions' win rate and expected value after costs (RealizedPnL is net of
// fees and the fill model's spread/slippage, FR-ENTRY-8), overall and per
// regime. Both known arms are always reported, empty ones with zero counts;
// regimes with no trades are left out. A trade of an unknown arm is ignored.
func ABArms(trades []marketregime.Trade) []marketregime.ArmStats {
	total := map[string]*accumulator{}
	byRegime := map[string]map[string]*accumulator{}
	for _, arm := range armOrder {
		total[arm] = &accumulator{}
		byRegime[arm] = map[string]*accumulator{}
	}
	for _, t := range trades {
		a, ok := total[t.ABArm]
		if !ok {
			continue
		}
		a.add(t)
		r := byRegime[t.ABArm][t.Regime]
		if r == nil {
			r = &accumulator{}
			byRegime[t.ABArm][t.Regime] = r
		}
		r.add(t)
	}

	out := make([]marketregime.ArmStats, 0, len(armOrder))
	for _, arm := range armOrder {
		regimes := make([]string, 0, len(byRegime[arm]))
		for regime := range byRegime[arm] {
			regimes = append(regimes, regime)
		}
		slices.SortFunc(regimes, func(a, b string) int {
			return cmp.Compare(slices.Index(regimeOrder, a), slices.Index(regimeOrder, b))
		})
		s := marketregime.ArmStats{Arm: arm, RegimeStats: total[arm].stats("")}
		for _, regime := range regimes {
			s.ByRegime = append(s.ByRegime, byRegime[arm][regime].stats(regime))
		}
		out = append(out, s)
	}
	return out
}
