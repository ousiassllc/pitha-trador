// Package marketregime holds the market regime and A/B arm vocabulary of issue
// #755 (parent #751). It lives apart from internal/domain, which it must not
// import, only to keep that directory within the per-directory line budget;
// domain.JevDecision and domain.CalibrationMetrics carry its types.
package marketregime

// Market regimes (issue #755, parent #751): what the tracked market index's
// 1-minute returns say about the session right now. They are distinct from
// the Jev answer's own JevRegime* label.
const (
	// Trend: the returns' lag-1 autocorrelation is at or above the
	// Settings threshold, so a move tends to continue (momentum).
	Trend = "trend"
	// MeanRevert: the autocorrelation is at or below minus the
	// threshold, so a move tends to reverse (VWAP reversion).
	MeanRevert = "mean_revert"
	// Unknown: not enough index data, a flat market or an
	// autocorrelation inside the threshold band.
	Unknown = "unknown"
)

// A/B arms of the paper comparison (issue #755): the fixed momentum
// strategy against the regime-switching one. Every decision belongs to
// exactly one arm.
const (
	// ArmMomentum is the control arm: the existing momentum-following
	// entry, whatever the regime.
	ArmMomentum = "momentum"
	// ArmRegime is the test arm: momentum in a trend, VWAP reversion in
	// a mean-reverting regime.
	ArmRegime = "regime"
)

// DefaultVWAPZThreshold is the shipped |z| a VWAP reversion entry needs
// (Settings regime.vwap_z_threshold).
const DefaultVWAPZThreshold = 2.0

// Assessment is the market regime and A/B arm one Jev trader decision
// was made under, persisted on jev_decisions (market_regime, ab_arm,
// regime_volatility_bps, regime_autocorr, vwap_z). The zero value means the
// regime was not assessed (a legacy row, or no assessor wired), which the
// Policy Engine treats as the plain momentum strategy.
type Assessment struct {
	// Regime is one of the regime constants; "" when not assessed.
	Regime string
	// ABArm is one of the Arm constants; "" when not assessed.
	ABArm string
	// VolatilityBps is the standard deviation of the index's 1-minute
	// returns over the Settings window, in bps; nil when unavailable.
	VolatilityBps *float64
	// Autocorr is those returns' lag-1 autocorrelation (-1..1); nil when
	// unavailable.
	Autocorr *float64
	// VWAPZ is the stock's deviation from VWAP in units of its own typical
	// deviation since the open; computed only in a mean-reverting regime,
	// nil otherwise or when the session is too short.
	VWAPZ *float64
	// VWAPZThreshold is the Settings |z| a VWAP reversion entry needs, as of
	// the decision. It is not persisted: 0 (a decision loaded back for a
	// replay) selects DefaultVWAPZThreshold.
	VWAPZThreshold float64
}

// UsesVWAPReversion reports whether the Policy Engine enters on the VWAP
// reversion rule instead of Jev's momentum direction: the test arm in a
// mean-reverting regime.
func (a Assessment) UsesVWAPReversion() bool {
	return a.ABArm == ArmRegime && a.Regime == MeanRevert
}

// Trade is one closed paper position joined back to the regime
// assessment of the Jev trader decision that opened it, for the A/B
// comparison of the momentum and regime-switching arms (issue #755).
// RealizedPnL is positions.realized_pnl (JPY), net of fees and priced by the
// fill model's spread/slippage; ReturnPct is it relative to the entry notional,
// in percent.
type Trade struct {
	ABArm       string
	Regime      string
	RealizedPnL float64
	ReturnPct   float64
}

// RegimeStats is the cost-adjusted result of the closed positions of one A/B
// arm (and, in ArmStats.ByRegime, of one regime within it): the count, the
// share with a positive realized PnL, the mean return after costs (EVNetBps,
// bps of the entry notional) and the total PnL.
type RegimeStats struct {
	// Regime is one of the regime constants; "" for an arm's overall figures.
	Regime     string
	TradeCount int
	WinRate    float64
	EVNetBps   float64
	TotalPnL   float64
}

// ArmStats is one A/B arm's result: its overall figures (Regime "") and the
// same split by the regime the decisions were made under.
type ArmStats struct {
	Arm string
	RegimeStats
	ByRegime []RegimeStats
}
