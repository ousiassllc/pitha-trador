package actuals

import "time"

// Metric is one evaluated figure together with its sample size and 95%
// confidence interval (issue #777 要件2).
//
// Value is reported even when it is not Reliable so the operator can see it,
// but a screen/export MUST then show "判定不能" instead of treating it as a
// result. Reliable is false when the sample is below Criteria.MinSample, the
// observations come from fewer than Criteria.MinClusters trading days, the
// figure is undefined (Defined false, e.g. a Profit Factor without a losing
// trade) or no interval could be computed.
type Metric struct {
	// N is the number of observations behind Value.
	N int `json:"n" doc:"Sample size behind the value."`
	// Value is the point estimate (meaningful only when Defined).
	Value float64 `json:"value"`
	// Lo/Hi bound the 95% confidence interval (meaningful only when HasCI).
	Lo    float64 `json:"lo"`
	Hi    float64 `json:"hi"`
	HasCI bool    `json:"has_ci"`
	// Defined is false when the figure cannot be computed from the sample.
	Defined bool `json:"defined"`
	// Reliable is false when the metric must be shown as 判定不能.
	Reliable bool `json:"reliable" doc:"false: below the minimum sample / trading days, undefined or without an interval - show 判定不能."`
}

// Stats is the realized (Paper) result of a set of closed positions.
//
// All JPY figures come from positions.realized_pnl and paper_orders; the
// costs follow what the fill model books (internal/service/fillmodel):
// slippage (with the spread and tick rounding) is embedded in the fill price
// and so already inside NetPnL, whereas fees are subtracted from NetPnL
// explicitly. Hence GrossBeforeFeesJPY = NetPnL + FeesJPY is the result at
// the fill prices, and GrossBeforeCostsJPY adds the slippage back: the result
// had every fill been at its reference price.
type Stats struct {
	N int `json:"n"`
	// Days is the number of distinct JST days with a closed position.
	Days int `json:"days"`
	Wins int `json:"wins"`

	NetPnLJPY            float64 `json:"net_pnl_jpy"`
	GrossBeforeFeesJPY   float64 `json:"gross_before_fees_jpy"`
	GrossBeforeCostsJPY  float64 `json:"gross_before_costs_jpy"`
	FeesJPY              float64 `json:"fees_jpy"`
	SlippageJPY          float64 `json:"slippage_jpy" doc:"Slippage (incl. spread and tick rounding) embedded in the fill prices, in JPY; fills without a recorded slippage_bps count as 0."`
	SlippageRecordedFill int     `json:"slippage_recorded_fills" doc:"Number of fills with a recorded slippage_bps (of 2*n at most)."`

	WinRate        Metric `json:"win_rate" doc:"Share of positions with a positive realized PnL (0..1)."`
	EVJPY          Metric `json:"ev_jpy" doc:"Mean net PnL per position (JPY)."`
	EVBps          Metric `json:"ev_bps" doc:"Mean net return per position on its entry notional (bps)."`
	ProfitFactor   Metric `json:"profit_factor" doc:"Gross profit / gross loss (JPY)."`
	MaxDrawdownJPY Metric `json:"max_drawdown_jpy" doc:"Largest peak-to-trough fall of the cumulative net PnL, the start being 0 (JPY, >= 0). The interval is a day-block bootstrap."`
	// MaxDrawdownPct is MaxDrawdownJPY over InitialCapital in percent;
	// undefined when InitialCapital is not set.
	MaxDrawdownPct Metric `json:"max_drawdown_pct"`
}

// Group is Stats of one slice (a day, week, direction, exit reason, model
// version or A/B arm).
type Group struct {
	// Key is the slice's identifier: "2026-10-14", "2026-W42", "LONG",
	// "stop_loss", a model version or "momentum"; "" for trades without one.
	Key string `json:"key"`
	Stats
}

// JevGroup is Jev's direction hit rate and Brier score over the graded
// directional trader decisions of one calibration label version (the
// versions are never mixed, issue #777 要件5). Ties (a return of exactly 0)
// are excluded.
type JevGroup struct {
	LabelVersion int    `json:"label_version" doc:"calibration_outcomes.label_version: 1 = close only, 2 = triple barrier (correct means profitable after costs)."`
	HitRate      Metric `json:"hit_rate"`
	Brier        Metric `json:"brier" doc:"Mean squared error between confidence and the outcome (0 = perfect, 0.25 = coin flip at 50%)."`
}

// MetaComparison sets one meta-labeling model's out-of-sample metrics beside
// the realized Paper results of the same model (issue #777 要件1/4).
type MetaComparison struct {
	Version      string    `json:"version"`
	Algorithm    string    `json:"algorithm"`
	Status       string    `json:"status"`
	LabelVersion int       `json:"label_version"`
	TrainedAt    time.Time `json:"trained_at"`
	OOSStart     time.Time `json:"oos_start"`
	OOSEnd       time.Time `json:"oos_end"`
	OOSSamples   int       `json:"oos_samples"`
	// OOSReliable is false when OOSSamples is below Criteria.MinSample: the
	// OOS figures are then 判定不能.
	OOSReliable    bool     `json:"oos_reliable"`
	OOSAUC         *float64 `json:"oos_auc"`
	OOSExpectedBps *float64 `json:"oos_expected_bps"`

	// Linked are the Paper trades entered on a decision this model scored
	// (metalabel_predictions), PredictedEVBps the mean EV the model predicted
	// for them and EVGapBps the mean of realized minus predicted EV per trade
	// (a negative gap: the model over-promised), all bps.
	Linked         Stats  `json:"linked"`
	PredictedEVBps Metric `json:"predicted_ev_bps"`
	EVGapBps       Metric `json:"ev_gap_bps"`
	// InOOSWindow are the Paper trades closed inside the model's OOS window,
	// whichever model they used: the same calendar period as the OOS EV.
	InOOSWindow Stats `json:"in_oos_window"`
}

// Status is the outcome of one check or of the whole 合格ライン.
type Status string

const (
	// StatusPass: determinable and the criterion is met.
	StatusPass Status = "pass"
	// StatusFail: determinable and the criterion is not met.
	StatusFail Status = "fail"
	// StatusUndetermined (判定不能): the sample is too small or the input
	// unset, so neither pass nor fail can be stated.
	StatusUndetermined Status = "undetermined"
)

// Check is one line of the 合格ライン.
type Check struct {
	Key      string  `json:"key" enum:"period_days,trades,ev_lower_bound,max_drawdown"`
	Required float64 `json:"required" doc:"The criterion's threshold."`
	// Actual is meaningful only when HasActual.
	Actual    float64 `json:"actual"`
	HasActual bool    `json:"has_actual"`
	Status    Status  `json:"status" enum:"pass,fail,undetermined"`
}

// Verdict is the 合格ライン outcome over Evaluation.Overall: "is it winning".
// It is an input to the pre-Live checks (#55) only; nothing gates on it.
type Verdict struct {
	Status Status  `json:"status" enum:"pass,fail,undetermined"`
	Checks []Check `json:"checks"`
}

// Evaluation is the whole read-only evaluation of closed Paper trades.
type Evaluation struct {
	GeneratedAt time.Time `json:"generated_at"`
	// From/To is the closed-at range [From, To) evaluated; nil: unbounded.
	From *time.Time `json:"from"`
	To   *time.Time `json:"to"`
	// InitialCapital is the risk config's initial_capital (JPY), the base of
	// the capital-ratio drawdown; <= 0: not set. In Paper it is a
	// placeholder, not an account balance.
	InitialCapital float64  `json:"initial_capital"`
	Criteria       Criteria `json:"criteria"`

	Overall      Stats   `json:"overall"`
	ByDay        []Group `json:"by_day"`
	ByWeek       []Group `json:"by_week"`
	ByDirection  []Group `json:"by_direction"`
	ByExitReason []Group `json:"by_exit_reason"`
	// ByMetaModel/ByABArm: Key "" are trades without a prediction / arm.
	ByMetaModel []Group `json:"by_meta_model"`
	ByABArm     []Group `json:"by_ab_arm"`

	Jev     []JevGroup       `json:"jev"`
	Meta    []MetaComparison `json:"meta"`
	Verdict Verdict          `json:"verdict"`
}
