package exitalign

import "time"

// DistributionBps summarises a sample of bps values (issue #774): its size,
// mean, standard deviation and percentiles. Every field is 0 when Count is 0.
type DistributionBps struct {
	Count  int
	Mean   float64
	StdDev float64
	Min    float64
	P5     float64
	P25    float64
	Median float64
	P75    float64
	P95    float64
	Max    float64
}

// ReasonCount is how many positions (or replays) ended for one exit reason.
type ReasonCount struct {
	Reason string
	Count  int
}

// LabelExitComparison compares, per decision, the triple-barrier label's net
// return with the net return of the same decision replayed under the actual
// exit rules (issue #774). It is computed on read from calibration_outcomes,
// market_snapshots and jev_decisions; nothing is stored.
type LabelExitComparison struct {
	// From/To is the decision window [From, To) compared.
	From, To time.Time
	// HorizonMinutes is the label timeout (Settings) the compared labels use.
	HorizonMinutes int
	// ReplayedCount is the number of decisions with both numbers.
	// SkippedCount is the number of labeled decisions whose bars do not
	// cover the exit rules' maximum holding time, so no replay was possible.
	ReplayedCount int
	SkippedCount  int
	// LabelNetBps and ReplayNetBps are the label and the replay net return
	// (bps, after cost) distributions; DiffBps is replay minus label per
	// decision. A negative mean means the label is optimistic against the
	// actual exit rules.
	LabelNetBps  DistributionBps
	ReplayNetBps DistributionBps
	DiffBps      DistributionBps
	// LabelTPCount/SLCount/TimeoutCount are the label's barrier reach counts
	// over the replayed decisions; ReplayExits is the replay's exit reasons
	// (domain.ExitReason*) and PaperExits the exit reasons of the closed
	// paper positions of the window, for comparison.
	LabelTPCount      int
	LabelSLCount      int
	LabelTimeoutCount int
	ReplayExits       []ReasonCount
	PaperExits        []ReasonCount
	// NotEvaluatedExits lists the exit conditions the replay cannot evaluate
	// from the stored data; a replayed decision that would have ended on one
	// of them is replayed to a later exit instead.
	NotEvaluatedExits []string
	// CostLongBps/CostShortBps are the label costs the replay deducted: the
	// same cost the labels assume (Settings barrier cost moved by the cost
	// settings), so the difference isolates the exit rules.
	CostLongBps  float64
	CostShortBps float64
}

// OOSPaperComparison sets the newest meta-labeling model's out-of-sample EV
// beside the mean realized paper return (cost-inclusive: realized PnL, which
// is net of fees, over the entry notional) of the positions closed in the same
// OOS window (issue #774). PaperMeanBps is nil without closed positions
// ("データ無し"); OOSEVBps is nil when the model's OOS EV is undefined.
type OOSPaperComparison struct {
	ModelVersion string
	OOSStart     time.Time
	OOSEnd       time.Time
	OOSEVBps     *float64
	PaperTrades  int
	PaperMeanBps *float64
	// DiffBps is PaperMeanBps - OOSEVBps; nil unless both exist.
	DiffBps *float64
}

// AlignmentRow is one line of the label-basis versus actual-exit table.
type AlignmentRow struct {
	// Key is take_profit, stop_loss, horizon, cost or trailing_stop.
	Key       string
	Item      string
	LabelText string
	ExitText  string
	// Comparable is false for a condition the label cannot express (trailing
	// stop): it has no number to compare and never warns by itself.
	Comparable bool
	// Aligned is true when the label and the exit agree within the tolerance.
	Aligned bool
}

// AlignmentWarning is one mismatch between the label basis and the actual exit
// (or the fill-model cost).
type AlignmentWarning struct {
	Code    string
	Message string
}

// LabelExitAlignment is the Calibration screen's view of how the label basis
// compares with the actual exit and the fill-model cost, plus the on-read
// evidence of what the difference does to the EV.
type LabelExitAlignment struct {
	Rows     []AlignmentRow
	Warnings []AlignmentWarning
	// ToleranceBps/ToleranceMinutes are the (provisional) thresholds the
	// warnings use.
	ToleranceBps     float64
	ToleranceMinutes float64
	// Comparison is nil when no window was compared (no replay source).
	Comparison *LabelExitComparison
	// OOSPaper is nil when no model was ever trained.
	OOSPaper *OOSPaperComparison
}
