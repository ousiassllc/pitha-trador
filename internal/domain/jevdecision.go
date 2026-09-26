package domain

import "time"

// Jev decision_type values (docs/architecture/er.md §jev_decisions CHECK
// constraint).
const (
	JevDecisionTypeScout  = "scout"
	JevDecisionTypeTrader = "trader"
)

// Jev direction values, set only for JevDecisionTypeTrader decisions
// (docs/architecture/er.md §jev_decisions, functional.md §4.5).
const (
	JevDirectionLong  = "LONG"
	JevDirectionShort = "SHORT"
	JevDirectionNone  = "NONE"
)

// Jev regime values, set only for JevDecisionTypeTrader decisions
// (functional.md §4.5 FR-TRADER-1, docs/components/overview.md §3
// glossary "Regime").
const (
	JevRegimeTrend    = "TREND"
	JevRegimeRange    = "RANGE"
	JevRegimeBreakout = "BREAKOUT"
	JevRegimeChaotic  = "CHAOTIC"
)

// Jev entry quality values, set only for JevDecisionTypeTrader decisions
// (functional.md §4.5 FR-TRADER-1: "poor〜exceptional"). Ordered from
// worst to best; Policy Engine's FR-POLICY-1/2 threshold
// ("entry_quality >= strong") compares this ordering.
const (
	JevEntryQualityPoor        = "poor"
	JevEntryQualityFair        = "fair"
	JevEntryQualityGood        = "good"
	JevEntryQualityStrong      = "strong"
	JevEntryQualityExceptional = "exceptional"
)

// JevDecision mirrors one jev_decisions row: a single Jev Scout/Trader
// call's input, raw output, and calibration metadata
// (docs/architecture/er.md §jev_decisions). It is the persisted audit
// trail Calibration (functional.md §4.9) is built on.
type JevDecision struct {
	ID           int64
	InstrumentID int64
	Symbol       string
	// Timestamp is when the Jev call was made (RFC3339).
	Timestamp time.Time
	// DecisionType is JevDecisionTypeScout or JevDecisionTypeTrader.
	DecisionType string
	// StateHash is a hash of StateJSON, letting a later Scheduler cycle
	// detect an input state that has not changed since the last call and
	// skip a redundant Jev call (functional.md FR-SCAN-2).
	StateHash string
	// StateJSON is the JSON-encoded input sent to Jev.
	StateJSON string
	// QuestionVersion is the prompt/question-set version that produced
	// this decision (internal/service/jev/prompt_version.go).
	QuestionVersion string
	// ResponseJSON is Jev's raw JSON response.
	ResponseJSON string
	// Direction is one of the JevDirection* constants, set only when
	// DecisionType == JevDecisionTypeTrader.
	Direction *string
	// Regime is one of the JevRegime* constants, set only when
	// DecisionType == JevDecisionTypeTrader.
	Regime *string
	// EntryQuality is one of the JevEntryQuality* constants, set only
	// when DecisionType == JevDecisionTypeTrader.
	EntryQuality *string
	// Confidence is Jev's confidence for Direction, set only when
	// DecisionType == JevDecisionTypeTrader. FR-TRADER-2: this is Jev's
	// own self-reported confidence, not a verified probability of an
	// actual price move - Calibration (functional.md §4.9) independently
	// checks how well it tracks real outcomes.
	Confidence *float64
	// ToxicFlow is Jev's yes-probability that current order flow is
	// toxic to a new entry, set only when DecisionType ==
	// JevDecisionTypeTrader (functional.md §4.5).
	ToxicFlow *float64
	// LiquidityStressed is Jev's yes-probability that liquidity is
	// currently stressed, set only when DecisionType ==
	// JevDecisionTypeTrader (functional.md §4.5).
	LiquidityStressed *float64
	// ContinuationProbability is Jev's yes-probability that the current
	// move continues, set only when DecisionType ==
	// JevDecisionTypeTrader (functional.md §4.5).
	ContinuationProbability *float64
	// LatencyMs is how long the Jev call took to complete, in
	// milliseconds.
	LatencyMs int
	ModelID   string
	// RequestCost is the API billing cost of the call (e.g. USD), or nil
	// when the provider does not report per-call cost.
	RequestCost *float64
	CreatedAt   time.Time
}
