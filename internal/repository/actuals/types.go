package actuals

import "time"

// Trade is one closed Paper position with everything the realized-performance
// evaluation (issue #777, internal/service/actuals) needs: the net result,
// the costs behind it, and the A/B arm and meta-labeling model of the
// decision that opened it. It is read-only evaluation input; no market data
// (board/quote values) is carried beyond the position's own fill prices.
//
// RealizedPnL is positions.realized_pnl (JPY): net of both fills' fees, and
// priced at the fill model's fill prices, so slippage and the spread are
// already embedded in EntryPrice/ExitPrice rather than booked separately.
type Trade struct {
	PositionID int64
	// Side is "LONG" or "SHORT".
	Side     string
	Quantity int64

	EntryPrice float64
	// ExitPrice is the exit order's filled price; nil without an exit order.
	ExitPrice   *float64
	RealizedPnL float64
	OpenedAt    time.Time
	ClosedAt    time.Time
	// ExitReason is positions.exit_reason ("" when NULL).
	ExitReason string

	// EntryFee/ExitFee are paper_orders.fees of the entry/exit orders (JPY);
	// ExitFee is 0 without an exit order.
	EntryFee float64
	ExitFee  float64
	// EntrySlippageBps/ExitSlippageBps are paper_orders.slippage_bps: how many
	// bps worse than the reference price each fill was (negative: price
	// improvement). Nil when not recorded.
	EntrySlippageBps *float64
	ExitSlippageBps  *float64

	// ABArm is the jev_decisions.ab_arm of the opening decision ("" when the
	// trade has no decision or the decision carries no arm).
	ABArm string
	// MetaModelVersion is metalabel_models.version of the newest meta-labeling
	// prediction made for the opening decision ("" when there is none), and
	// MetaExpectedBps that prediction's expected_value_bps (nil likewise).
	MetaModelVersion string
	MetaExpectedBps  *float64
}

// JevSample is one directional Jev trader decision graded by one
// calibration outcome (a decision/horizon row): the input of the Jev
// direction hit rate and Brier score of the evaluation. LabelVersion keeps
// the labeling methods apart (calibration_outcomes.label_version: 1 = close
// only, 2 = triple barrier, where Correct means "profitable after costs").
// Outcomes whose return is exactly zero (ties) are not returned.
type JevSample struct {
	DecidedAt      time.Time
	Direction      string
	Confidence     float64
	LabelVersion   int
	HorizonMinutes int
	Correct        bool
}

// MetaModel is one metalabel_models row: a trained meta-labeling model with
// its out-of-sample (walk-forward validation window) metrics.
type MetaModel struct {
	Version      string
	Algorithm    string
	Status       string
	LabelVersion int
	TrainedAt    time.Time
	OOSStart     time.Time
	OOSEnd       time.Time
	OOSSamples   int
	// OOSAUC and OOSExpectedBps are nil when undefined (NULL).
	OOSAUC         *float64
	OOSExpectedBps *float64
}
