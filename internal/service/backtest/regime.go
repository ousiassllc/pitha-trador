package backtest

import "github.com/ousiassllc/pitha-trador/internal/domain/marketregime"

// regimeOrder is the order AggregateByRegime reports regimes in; trades from
// decisions made before the regime was assessed (Regime "") come last.
var regimeOrder = []string{marketregime.Trend, marketregime.MeanRevert, marketregime.Unknown, ""}

// RegimeMetrics is Aggregate's result over the trades of one market regime
// (issue #755), so the momentum and VWAP reversion entries can each be judged
// in the regime they trade in.
type RegimeMetrics struct {
	// Regime is one of domain.Regime*; "" for trades of decisions made before
	// the regime was assessed.
	Regime  string
	Metrics Metrics
}

// AggregateByRegime splits trades by the regime of the decision that opened
// them and aggregates each group (FR-BT-1's metrics, after costs). Regimes
// without a trade are left out.
func AggregateByRegime(trades []Trade) []RegimeMetrics {
	groups := make(map[string][]Trade, len(regimeOrder))
	for _, tr := range trades {
		groups[tr.Regime] = append(groups[tr.Regime], tr)
	}
	var out []RegimeMetrics
	for _, regime := range regimeOrder {
		if g := groups[regime]; len(g) > 0 {
			out = append(out, RegimeMetrics{Regime: regime, Metrics: Aggregate(g)})
		}
	}
	return out
}
