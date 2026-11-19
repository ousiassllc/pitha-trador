package promotiondiag

import "github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"

// MinCoverage is the share of the reference rows of the period the common rows must cover
// (checklist item same_rows_and_period). It is a documented constant, not a setting.
const MinCoverage = 0.8

// Item statuses.
const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusUnknown = "unknown"
)

// Item names, in the fixed order of the checklist.
const (
	ItemSameRows           = "same_rows_and_period"
	ItemCommonRows         = "common_rows"
	ItemCommonDays         = "common_days"
	ItemDiffCILower        = "diff_ci_lower"
	ItemMultipleCompare    = "multiple_comparison"
	ItemPositiveDays       = "positive_days"
	ItemNotOneDayWins      = "not_one_day_wins"
	ItemMaxDrawdown        = "max_drawdown"
	ItemAfterAICost        = "after_ai_cost"
	ItemAfterCutoff        = "after_training_cutoff"
	ItemModelVersionStable = "model_version_stable"
)

// Result is promotion_checklist's data. Applied is always false: the checklist applies nothing.
type Result struct {
	From      string   `json:"from"`
	To        string   `json:"to"`
	Horizon   string   `json:"horizon"`
	Candidate Ref      `json:"candidate"`
	Current   Ref      `json:"current"`
	Notice    string   `json:"notice"`
	Criteria  Criteria `json:"criteria"`
	Items     []Item   `json:"items"`
	// FirstUnmet is the name of the first item that is not pass (null when all pass).
	FirstUnmet *string `json:"first_unmet"`
	// EligibleAsEvidence: every item passed. Evidence for a person's proposal, not an instruction.
	EligibleAsEvidence bool `json:"eligible_as_evidence"`
	// HorizonNote is set (and EligibleAsEvidence false) when a research model is compared at a horizon it was not trained for.
	HorizonNote string `json:"horizon_note,omitempty"`
	Applied     bool   `json:"applied"`
	// KeyNumbers are the numbers of a candidate read from the candidate ledger; nil for an experiment / setting
	// candidate, whose numbers are ExperimentNumbers.
	KeyNumbers  *KeyNumbers       `json:"key_numbers,omitempty"`
	Definitions map[string]string `json:"definitions"`
	// Experiment, ExperimentNumbers and EvidenceNote are set for a candidate of kind experiment / setting only
	// (settingsset_result.go): the experiment the result is read from, its numbers, and why such a result is
	// never eligible_as_evidence.
	Experiment        *ExperimentInfo    `json:"experiment,omitempty"`
	ExperimentNumbers *ExperimentNumbers `json:"experiment_numbers,omitempty"`
	EvidenceNote      string             `json:"evidence_note,omitempty"`
}

// Criteria are the numeric criteria in force with their documented floors and where each value comes from.
type Criteria struct {
	Effective   CriteriaValues    `json:"effective"`
	Floor       CriteriaValues    `json:"floor"`
	Source      map[string]string `json:"source"`
	MinCoverage float64           `json:"min_coverage"`
	Note        string            `json:"note"`
}

// CriteriaValues are the five numeric criteria.
type CriteriaValues struct {
	MinCommonRows     int     `json:"min_common_rows"`
	MinDays           int     `json:"min_days"`
	MinDiffCILowerBps float64 `json:"min_diff_ci_lower_bps"`
	MinPositiveDayPct float64 `json:"min_positive_day_pct"`
	MaxAdjustedP      float64 `json:"max_adjusted_p"`
}

// Item is one checklist line. Actual and Required are numbers (or, for the model count, an object);
// Comparator says how Actual must stand to Required to pass (">=", ">", "<="). Detail is a code.
type Item struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Actual     any    `json:"actual"`
	Required   any    `json:"required"`
	Comparator string `json:"comparator"`
	Detail     string `json:"detail"`
	// NotApplicable marks an item that cannot be judged for the candidate's kind (an experiment's before and
	// after are different trades, not the same rows). It is status unknown, never fail, and never first_unmet.
	NotApplicable bool `json:"not_applicable,omitempty"`
}

// KeyNumbers are the numbers behind the items.
type KeyNumbers struct {
	ReferenceRows   int          `json:"reference_rows"`
	CommonRows      int          `json:"n_common"`
	Coverage        *float64     `json:"coverage"`
	Days            int          `json:"days"`
	MeanNetBps      *float64     `json:"candidate_mean_net_bps"`
	CurrentMeanBps  *float64     `json:"current_mean_net_bps"`
	DiffMeanBps     *float64     `json:"diff_mean_bps"`
	DiffCI          *evaldiag.CI `json:"diff_ci"`
	PValue          *float64     `json:"p_value"`
	HolmAdjustedP   *float64     `json:"holm_adjusted_p"`
	FamilySize      int          `json:"family_size"`
	FamilySizeExtra int          `json:"family_size_extra"`
	PositiveDayPct  *float64     `json:"positive_day_pct"`
}
