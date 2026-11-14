// Package evaldiag is the read-only analysis behind the Paper results MCP's
// jev_evaluation tool (issue #1073): is Jev (AI) any good, judged on every
// trader LONG/SHORT decision of a period (not only those that passed the
// entry-quality and Risk gates) against simple baselines on the very same
// rows and with the very same cost-inclusive label.
//
// The label is the Labeler's own (calibration.LabelWindow) re-run on the
// stored market_snapshots with today's calibration.barrier_* and cost.*
// settings, so the LLM arm, the long/short legs and every baseline are
// measured alike. The package only reads: no setting, no label, no signal is
// written, and nothing here feeds Jev, Risk, Policy, an order or the
// meta-model. Its numbers are research outputs: no threshold is applied and
// none is an adoption criterion.
package evaldiag

// Limits of one call. A period beyond them is query.ErrLimitExceeded rather
// than a partial result.
const (
	// MaxDecisions is the most trader LONG/SHORT decisions one call reads.
	MaxDecisions = 20000
	// MaxCohorts is the most distinct (model_id, question_version) pairs.
	MaxCohorts = 50
	// MaxModelIDLen is the width of jev_decisions.model_id.
	MaxModelIDLen = 100
)

// The fixed statistics of every interval and detectable difference. They are
// descriptive constants, not adoption thresholds.
const (
	// Alpha is the two-sided level of the intervals and of the detectable
	// minimum difference; 1-Alpha is the interval's level.
	Alpha = 0.05
	// Power is the power of the detectable minimum difference.
	Power = 0.8
	// Resamples is the number of day-cluster bootstrap draws.
	Resamples = 2000
	// Seed is the bootstrap's fixed seed: a call repeats exactly.
	Seed uint64 = 1073
)

// TargetsBps are the differences (edges) "how many more days until it can be
// judged" is answered for.
var TargetsBps = []float64{3, 5}

// Arm names.
const (
	ArmLLM        = "llm"
	ArmLongLeg    = "long_leg"
	ArmShortLeg   = "short_leg"
	ArmMomentum   = "momentum"
	ArmContrarian = "contrarian"
	ArmNone       = "none"
	ArmRandom     = "random"
)

// Arms are every arm in output order (llm first); everything after it is a
// baseline the LLM is compared with.
var Arms = []string{ArmLLM, ArmRandom, ArmMomentum, ArmContrarian, ArmNone, ArmLongLeg, ArmShortLeg}

// Exclusion reasons of a decision that cannot be labeled on every arm: a leg's
// prefix (the decision's own side, or the other one) plus why its label is
// missing, or the missing bar of the momentum signal.
const (
	PrefixLLM           = "llm_"
	PrefixOpposite      = "opposite_leg_"
	KindNoEntry         = "no_entry_quote"
	KindWindowShort     = "window_short"
	ReasonNoBarMomentum = "no_bar_for_momentum"
)

// Entry split of the LLM arm: a decision is "entered" when a trade signal of it
// passed Risk (risk_passed = 1), otherwise "skipped".
const (
	Entered = "entered"
	Skipped = "skipped"
)

// CI is a percentile interval of a day-cluster bootstrap.
type CI struct {
	Lower float64 `json:"lower"`
	Upper float64 `json:"upper"`
	Level float64 `json:"level"`
}

// DaysToDetect answers, for one target (an edge or a difference in bps), how
// many more JST days of the same kind are needed before the target is
// detectable: the standard error is assumed to shrink with 1/sqrt(days), so
// AdditionalDays = max(0, ceil(days * (minimum / target)^2 - days)).
// AdditionalDays is null when the standard error is undefined (fewer than two
// days). AlreadyDetectable is true when the detectable minimum is already at or
// below the target.
type DaysToDetect struct {
	TargetBps         float64 `json:"target_bps"`
	AdditionalDays    *int    `json:"additional_days"`
	AlreadyDetectable bool    `json:"already_detectable"`
}

// Day is one JST day's mean net return of an arm.
type Day struct {
	Date        string  `json:"date"`
	N           int     `json:"n"`
	MeanNetBps  float64 `json:"mean_net_bps"`
	WinRate     float64 `json:"win_rate"`
	TotalNetBps float64 `json:"total_net_bps"`
}

// Metrics is one arm's net return (bps, after the label's cost) on a set of
// decisions. MeanNetBps, CI and WinRate are null while N is 0, CI also with
// fewer than two days. DetectableMinimumEdgeBps is the smallest true mean the
// day-cluster standard error could tell from 0 at Alpha and Power. Daily is
// omitted in the entered/skipped split.
type Metrics struct {
	N                        int            `json:"n"`
	Days                     int            `json:"days"`
	MeanNetBps               *float64       `json:"mean_net_bps"`
	CI                       *CI            `json:"ci"`
	WinRate                  *float64       `json:"win_rate"`
	DetectableMinimumEdgeBps *float64       `json:"detectable_minimum_edge_bps"`
	DaysToDetect             []DaysToDetect `json:"days_to_detect"`
	Daily                    []Day          `json:"daily,omitempty"`
}

// Diff is the paired difference llm minus a baseline on the same rows (the
// cluster of a day is the sum of the row differences).
type Diff struct {
	N                              int            `json:"n"`
	Days                           int            `json:"days"`
	MeanDiffBps                    *float64       `json:"mean_diff_bps"`
	CI                             *CI            `json:"ci"`
	DetectableMinimumDifferenceBps *float64       `json:"detectable_minimum_difference_bps"`
	DaysToDetect                   []DaysToDetect `json:"days_to_detect"`
}

// ByDirection is Metrics for the decisions whose own direction is LONG, SHORT,
// and for all of them.
type ByDirection struct {
	All   Metrics `json:"all"`
	Long  Metrics `json:"long"`
	Short Metrics `json:"short"`
}

// Arm is one arm's Metrics.
type Arm struct {
	Arm string `json:"arm"`
	ByDirection
}

// DiffByDirection is Diff for each direction subset.
type DiffByDirection struct {
	All   Diff `json:"all"`
	Long  Diff `json:"long"`
	Short Diff `json:"short"`
}

// Difference is llm minus Baseline.
type Difference struct {
	Baseline string `json:"baseline"`
	DiffByDirection
}

// EntrySplit is the LLM arm's Metrics for the decisions that entered (a signal
// passed Risk) or were skipped (no signal, or none passed).
type EntrySplit struct {
	Entry string `json:"entry"`
	ByDirection
}

// Bucket is a count under a fixed reason code.
type Bucket struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// StoredStats is the stored calibration_outcomes label (label_version 2, at the
// call's horizon) over a set of decisions: the number the Calibration screen
// averages. MeanNetBps is null without a label.
type StoredStats struct {
	Decisions    int      `json:"decisions"`
	WithLabel    int      `json:"with_label"`
	WithoutLabel int      `json:"without_label"`
	MeanNetBps   *float64 `json:"mean_net_bps"`
}

// StoredLabel reconciles the recomputed label with the stored one.
// RecomputedMeanOnLabeled and MaxAbsDifferenceBps compare, on the evaluated
// decisions that have a stored label, the recomputed llm net return with the
// stored one (they differ only when the settings changed after the label was
// written, or the bars changed).
type StoredLabel struct {
	LabelVersion            int         `json:"label_version"`
	HorizonMinutes          int         `json:"horizon_minutes"`
	AllDecisions            StoredStats `json:"all_decisions"`
	EvaluatedRows           StoredStats `json:"evaluated_rows"`
	RecomputedMeanOnLabeled *float64    `json:"recomputed_mean_on_labeled_rows"`
	MaxAbsDifferenceBps     *float64    `json:"max_abs_difference_bps"`
}

// Cohort is one (model_id, question_version) pair: its own full result, so a
// version change restarts the count. Decisions = ExcludedBeforeCutoff + the
// ExcludedByReason counts + Evaluated.
type Cohort struct {
	ModelID              string       `json:"model_id"`
	QuestionVersion      string       `json:"question_version"`
	FirstDecisionDay     string       `json:"first_decision_day"`
	LastDecisionDay      string       `json:"last_decision_day"`
	Decisions            int          `json:"decisions"`
	ExcludedBeforeCutoff int          `json:"excluded_before_cutoff"`
	ExcludedByReason     []Bucket     `json:"excluded_by_reason"`
	Evaluated            int          `json:"evaluated"`
	StoredLabel          StoredLabel  `json:"stored_label"`
	Arms                 []Arm        `json:"arms"`
	Differences          []Difference `json:"llm_minus_baseline"`
	LLMByEntry           []EntrySplit `json:"llm_by_entry"`
}

// CostBps is the label cost (bps, round trip) of each side.
type CostBps struct {
	Long  float64 `json:"long"`
	Short float64 `json:"short"`
}

// LabelAssumptions are the barrier and cost settings in force now, which every
// arm is labeled with. A stored label may have used earlier values.
type LabelAssumptions struct {
	LabelVersion   int     `json:"label_version"`
	HorizonMinutes int     `json:"horizon_minutes"`
	TakeProfitBps  float64 `json:"take_profit_bps"`
	StopLossBps    float64 `json:"stop_loss_bps"`
	BarrierCostBps float64 `json:"barrier_cost_bps"`
	LabelCostBps   CostBps `json:"label_cost_bps"`
	Settings       string  `json:"settings"`
}

// Statistics are the fixed constants behind every interval.
type Statistics struct {
	Method    string    `json:"method"`
	Alpha     float64   `json:"alpha"`
	Power     float64   `json:"power"`
	Resamples int       `json:"resamples"`
	Seed      uint64    `json:"seed"`
	Targets   []float64 `json:"targets_bps"`
}

// Cutoff is the training cutoff the LLM evaluation honors. Applied is false
// while the cutoff is the placeholder: nothing is dropped then.
type Cutoff struct {
	Date    string `json:"date"`
	Status  string `json:"status"`
	Model   string `json:"model"`
	Applied bool   `json:"applied"`
}

// Data is jev_evaluation's data.
type Data struct {
	From             string            `json:"from"`
	To               string            `json:"to"`
	HorizonMinutes   int               `json:"horizon_minutes"`
	PromptVersion    string            `json:"prompt_version,omitempty"`
	ModelID          string            `json:"model_id,omitempty"`
	Notice           string            `json:"notice"`
	Decisions        int               `json:"decisions"`
	LLMCutoff        Cutoff            `json:"llm_cutoff"`
	LabelAssumptions LabelAssumptions  `json:"label_assumptions"`
	Statistics       Statistics        `json:"statistics"`
	Cohorts          []Cohort          `json:"cohorts"`
	Definitions      map[string]string `json:"definitions"`
	Caveats          []string          `json:"caveats"`
}
