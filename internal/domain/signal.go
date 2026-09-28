package domain

import "time"

// TradeSignal mirrors one trade_signals row: a Policy Engine trade
// candidate derived from a Jev Trader decision (docs/architecture/er.md
// §trade_signals). It is the persisted decision log FR-POLICY-5 requires
// - every LONG/SHORT/NONE Policy Engine outcome, not only the ones that
// pass - so Backtesting/Calibration (functional.md §4.9, a later
// sub-scope) can replay what the Policy Engine decided and why.
type TradeSignal struct {
	ID           int64
	InstrumentID int64
	// JevDecisionID references the jev_decisions row (decision_type=trader)
	// this signal was derived from, or nil when no Jev Trader decision was
	// available (functional.md FR-POLICY-3 "API異常"/"データ欠損").
	JevDecisionID *int64
	Symbol        string
	Timestamp     time.Time
	// Direction is one of the JevDirection* constants: LONG/SHORT when
	// every FR-POLICY-1/FR-POLICY-2 threshold is cleared, NONE whenever
	// any FR-POLICY-3 condition applies.
	Direction string
	// Score is the Policy Engine's internal score for this signal (Jev's
	// own confidence for Direction), nil when Direction is NONE.
	Score *float64
	// EntryPriceReference is the reference price the signal was evaluated
	// against, nil when unavailable.
	EntryPriceReference *float64
	// PolicyVersion is the threshold-evaluation logic/config version that
	// produced this signal (functional.md FR-POLICY-4,
	// internal/service/policy.Version).
	PolicyVersion string
	// RiskPassed is true only when Direction is LONG/SHORT and Risk Engine
	// (functional.md §4.7, a later sub-scope - internal/service/policy.
	// RiskChecker is this scope's placeholder extension point) allowed it.
	// It is always false for a NONE signal.
	RiskPassed bool
	// RejectReason explains why RiskPassed is false / Direction is NONE,
	// nil when Direction is LONG/SHORT and RiskPassed is true.
	RejectReason *string
	CreatedAt    time.Time
}
