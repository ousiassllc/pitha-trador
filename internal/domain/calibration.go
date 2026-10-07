package domain

import "time"

// CalibrationOutcome mirrors one calibration_outcomes row: a single Jev
// trader decision's realized outcome at one judgment horizon
// (docs/architecture/er.md §calibration_outcomes, functional.md §4.12
// FR-CAL-4).
type CalibrationOutcome struct {
	ID             int64
	JevDecisionID  int64
	HorizonMinutes int
	// FutureReturn is the raw (direction-neutral) % price return from the
	// decision's entry price to the price horizon_minutes later (1.0 ==
	// +1%, the same percentage-return convention
	// internal/service/backtest.Trade already uses).
	FutureReturn float64
	// MaxAdverseExcursion/MaxFavorableExcursion are the worst/best
	// direction-adjusted % price move seen at any point within the
	// horizon window (LONG: the raw price return as-is; SHORT: the raw
	// price return negated; NONE: the raw price return, same as
	// FutureReturn - there is no held direction to adjust for). Both are
	// 0 when price never moves favorably/adversely at all, since the
	// entry bar's own 0% return is always a candidate.
	MaxAdverseExcursion   float64
	MaxFavorableExcursion float64
	// WasDirectionCorrect is nil when the decision's Direction was NONE
	// (er.md: "direction=NONEの場合NULL"), otherwise true iff FutureReturn's
	// sign matches Direction (LONG: positive; SHORT: negative).
	WasDirectionCorrect *bool
	CreatedAt           time.Time
}

// ConfidenceBucketRange is one static confidence band Calibration groups
// Jev trader decisions into (functional.md FR-CAL-3): [Low, High), except
// the last range (High == 1.00) which also includes High itself.
type ConfidenceBucketRange struct {
	Range string
	Low   float64
	High  float64
}

// DefaultConfidenceBucketRanges are FR-CAL-3's five confidence bands
// (docs/api/endpoints.md §GET /api/v1/calibration).
var DefaultConfidenceBucketRanges = []ConfidenceBucketRange{
	{Range: "0.50-0.60", Low: 0.50, High: 0.60},
	{Range: "0.60-0.70", Low: 0.60, High: 0.70},
	{Range: "0.70-0.80", Low: 0.70, High: 0.80},
	{Range: "0.80-0.90", Low: 0.80, High: 0.90},
	{Range: "0.90-1.00", Low: 0.90, High: 1.00},
}

// ConfidenceBucket is one ConfidenceBucketRange's aggregated calibration
// outcomes (functional.md FR-CAL-2/3, docs/api/endpoints.md §GET
// /api/v1/calibration "buckets"): how often the predicted Direction was
// correct and the average direction-adjusted future return, among every
// labeled trader decision/horizon outcome whose Confidence fell in Range.
// AvgConfidence is those outcomes' mean Confidence (the Reliability
// Curve's x-axis value). TradeCount/TotalPnL/AvgPnLPct/TradeWinRate are
// FR-CAL-2's "confidence bucket別PnL" and the realized-PnL ground truth
// (issue #711): the closed positions entered on a signal derived from a
// trader decision whose Confidence fell in Range (see DecisionTrade),
// independent of SampleCount, which counts outcome horizons.
// TradeWinRate is the share of those positions whose RealizedPnL was
// positive (0 when TradeCount is 0), the realized-PnL counterpart of
// DirectionAccuracy.
type ConfidenceBucket struct {
	Range              string
	AvgConfidence      float64
	DirectionAccuracy  float64
	AvgFutureReturnPct float64
	SampleCount        int
	TradeCount         int
	TotalPnL           float64
	AvgPnLPct          float64
	TradeWinRate       float64
}

// DirectionMetric is one predicted direction's (LONG or SHORT) aggregated
// outcomes (functional.md FR-CAL-2 "方向別平均リターン"): the share of
// labeled outcomes graded correct and the average direction-adjusted
// future return, over every labeled outcome with that Direction.
type DirectionMetric struct {
	Direction          string
	SampleCount        int
	DirectionAccuracy  float64
	AvgFutureReturnPct float64
}

// CalibrationMetrics is Calibration's full evaluation result (functional.md
// FR-CAL-2, docs/api/endpoints.md §GET /api/v1/calibration): the
// Reliability Curve (Buckets) plus Brier Score, Log Loss, and Expected
// Calibration Error computed over every labeled trader decision/horizon
// outcome, and the per-direction (LONG, SHORT) breakdown in ByDirection.
// TradeCount, PnLBrierScore and PnLLogLoss are the same two scores computed
// against the realized-PnL ground truth instead of the price-path one
// (issue #711): over every closed position traced to a trader decision
// (DecisionTrade) whose Confidence falls in a bucket, the predicted
// probability is Confidence and the outcome is RealizedPnL > 0. They are 0
// when TradeCount is 0.
type CalibrationMetrics struct {
	Buckets                  []ConfidenceBucket
	ByDirection              []DirectionMetric
	BrierScore               float64
	LogLoss                  float64
	ExpectedCalibrationError float64
	SampleCount              int
	TradeCount               int
	PnLBrierScore            float64
	PnLLogLoss               float64
}

// DecisionTrade is one closed position joined back to the Jev trader
// decision that produced its entry (positions.entry_order_id ->
// paper_orders.trade_signal_id -> trade_signals.jev_decision_id), for
// internal/service/calibration.WithTradePnL to fold into each
// ConfidenceBucket's PnL and the realized-PnL ground truth (issue #711).
// RealizedPnL is positions.realized_pnl (JPY), net of both fills' fees and
// priced by the fill model's spread/slippage (FR-ENTRY-8); a trade is a
// "win" iff RealizedPnL > 0;
// ReturnPct is RealizedPnL relative to the entry notional
// (entry_price*quantity), in percent.
type DecisionTrade struct {
	Confidence  float64
	RealizedPnL float64
	ReturnPct   float64
}

// LabeledSample is one labeled Jev trader decision/horizon outcome, joined
// with its decision's Direction/Confidence, for internal/service/
// calibration.Metrics to aggregate into CalibrationMetrics. Direction is
// always JevDirectionLong or JevDirectionShort: NONE decisions have no
// predicted direction to grade (WasDirectionCorrect is always NULL for
// them) and are excluded before this type is populated.
type LabeledSample struct {
	Direction           string
	Confidence          float64
	FutureReturn        float64
	WasDirectionCorrect bool
}
