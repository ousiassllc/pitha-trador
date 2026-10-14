package calibration

import "github.com/ousiassllc/pitha-trador/internal/domain/marketregime"

// abRegimeOutput is the cost-adjusted result of the closed positions of one A/B
// arm within one market regime (issue #755).
type abRegimeOutput struct {
	Regime     string  `json:"regime" doc:"Market regime the decisions were made under: trend, mean_revert or unknown."`
	TradeCount int     `json:"trade_count" doc:"Number of closed positions."`
	WinRate    float64 `json:"win_rate" doc:"Share of those positions with a positive realized PnL (0..1; 0 when trade_count is 0)."`
	EVNetBps   float64 `json:"ev_net_bps" doc:"Mean return after costs per position (bps of the entry notional; realized PnL is net of fees and the fill model's spread/slippage; 0 when trade_count is 0)."`
	TotalPnL   float64 `json:"total_pnl" doc:"Sum of realized PnL (JPY)."`
}

// abArmOutput is one A/B arm of the paper comparison between the fixed momentum
// strategy and the regime-switching one (issue #755).
type abArmOutput struct {
	Arm        string           `json:"arm" enum:"momentum,regime" doc:"momentum: the fixed momentum-following arm (also every decision while the A/B comparison is off); regime: momentum in a trend, VWAP reversion in a mean-reverting regime."`
	TradeCount int              `json:"trade_count" doc:"Number of closed positions of the arm."`
	WinRate    float64          `json:"win_rate" doc:"Share of those positions with a positive realized PnL (0..1; 0 when trade_count is 0)."`
	EVNetBps   float64          `json:"ev_net_bps" doc:"Mean return after costs per position (bps of the entry notional; realized PnL is net of fees and the fill model's spread/slippage; 0 when trade_count is 0)."`
	TotalPnL   float64          `json:"total_pnl" doc:"Sum of realized PnL (JPY) of the arm."`
	ByRegime   []abRegimeOutput `json:"by_regime" doc:"The same figures per market regime, only regimes with at least one position."`
}

func abArmsResponse(arms []marketregime.ArmStats) []abArmOutput {
	out := make([]abArmOutput, len(arms))
	for i, a := range arms {
		out[i] = abArmOutput{
			Arm: a.Arm, TradeCount: a.TradeCount, WinRate: a.WinRate, EVNetBps: a.EVNetBps, TotalPnL: a.TotalPnL,
			ByRegime: make([]abRegimeOutput, len(a.ByRegime)),
		}
		for j, r := range a.ByRegime {
			out[i].ByRegime[j] = abRegimeOutput{Regime: r.Regime, TradeCount: r.TradeCount, WinRate: r.WinRate, EVNetBps: r.EVNetBps, TotalPnL: r.TotalPnL}
		}
	}
	return out
}
