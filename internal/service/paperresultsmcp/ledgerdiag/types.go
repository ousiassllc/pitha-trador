package ledgerdiag

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
)

// Ledger is candidate_ledger's data. It names no winner and ranks nothing: Candidates
// are in a fixed order of kind and id, never of a value.
type Ledger struct {
	From        string            `json:"from"`
	To          string            `json:"to"`
	Horizon     string            `json:"horizon"`
	Alignment   string            `json:"alignment"`
	Notice      string            `json:"notice"`
	LLMCutoff   evaldiag.Cutoff   `json:"llm_cutoff"`
	Reference   Reference         `json:"reference"`
	Exclusions  Exclusions        `json:"excluded_decisions"`
	Statistics  Statistics        `json:"statistics"`
	Candidates  []Entry           `json:"candidates"`
	Definitions map[string]string `json:"definitions"`
	Caveats     []string          `json:"caveats"`
}

// Statistics are the fixed constants behind every interval and p-value, and the
// multiple-comparison family.
type Statistics struct {
	Method     string  `json:"method"`
	Alpha      float64 `json:"alpha"`
	Power      float64 `json:"power"`
	Resamples  int     `json:"resamples"`
	Seed       uint64  `json:"seed"`
	Correction string  `json:"correction"`
	FamilySize int     `json:"family_size"`
}

// Reference is the live Jev (the llm arm of the live prompt version's trader decisions).
type Reference struct {
	ID              string   `json:"id"`
	PromptVersion   string   `json:"prompt_version"`
	ModelIDFilter   string   `json:"model_id_filter,omitempty"`
	ModelIDs        []string `json:"model_ids"`
	MultipleModelID bool     `json:"multiple_model_ids"`
	// Rows are the reference's evaluated rows in the period: the denominator of every missing rate.
	Rows       int          `json:"rows"`
	Days       int          `json:"days"`
	MeanNetBps *float64     `json:"mean_net_bps"`
	CI         *evaldiag.CI `json:"ci"`
	AICost     Cost         `json:"ai_cost"`
	// CommonRows are the rows every candidate is measured on under common_all (null under pairwise).
	CommonRows *int `json:"common_rows"`
}

// Exclusions counts the trader LONG/SHORT decisions of the period that no candidate can be
// measured on: left out for everyone, never silently.
type Exclusions struct {
	// Decisions is every trader LONG/SHORT decision of the period (all cohorts).
	Decisions    int `json:"decisions"`
	BeforeCutoff int `json:"before_cutoff"`
	// NoLabel: no label for both sides at the horizon (10m: no entry quote or the window does not reach the
	// horizon; 30m/60m/close: no research_horizon_labels row, or one side missing).
	NoLabel int `json:"no_label_both_sides"`
	// NoBarForMomentum: the decision's own bar is missing (the momentum baseline cannot be formed); only
	// counted for the research horizons (for 10m it is part of no_label_both_sides, as in jev_evaluation).
	NoBarForMomentum int `json:"no_bar_for_momentum"`
	Evaluated        int `json:"evaluated"`
}

// Entry is one candidate's column of the ledger.
type Entry struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// NoAnswers: the candidate has no answer on any reference row of the period (it is listed, but
	// is left out of the common_all intersection so one empty candidate does not empty the table).
	NoAnswers bool `json:"no_answers"`
	// NAnswered is the number of reference rows the candidate answered (live_cohort: its own rows).
	NAnswered int `json:"n_answered"`
	// NCommon is the number of rows the candidate's values are computed on (null for live_cohort: it
	// judged other decisions, so it has no common rows with the live reference).
	NCommon *int `json:"n_common"`
	// MissingRate is 1 - n_common / reference rows; every row the candidate did not answer (absent,
	// failed, refused, partial) is missing, and under common_all so is a row another candidate missed.
	MissingRate *float64 `json:"missing_rate"`
	Missing     Missing  `json:"missing"`

	Days                 int          `json:"days"`
	MeanNetBps           *float64     `json:"mean_net_bps"`
	CI                   *evaldiag.CI `json:"ci"`
	LiveMeanOnCommonRows *float64     `json:"live_mean_on_common_rows"`
	DiffVsLive           *Diff        `json:"diff_vs_live"`
	// DiffReason says why DiffVsLive is null: different_decisions (live_cohort).
	DiffReason       string   `json:"diff_reason,omitempty"`
	AICost           Cost     `json:"ai_cost"`
	Combined         Combined `json:"combined"`
	DistinctModelIDs []string `json:"distinct_model_ids"`

	// Series is the candidate's and the reference's net on the common rows in time order, for the
	// promotion checklist; never part of the response.
	Series []PairRow `json:"-"`
	// RefCost is the reference's AI cost on the same common rows (never part of the response; the
	// response shows it in combined and reference).
	RefCost Cost `json:"-"`
	// Diff is the paired difference's full summary, with its SE, for the promotion checklist.
	DiffSummary evaldiag.Summary `json:"-"`
}

// Missing splits the reference rows the candidate did not answer.
type Missing struct {
	// Absent: no answer recorded.
	Absent int `json:"absent"`
	// Failed: a trial row whose status is not ok (failed, refused or partial).
	Failed int `json:"failed_or_refused"`
	// OutsideCommon: answered, but not on the common rows (another candidate missed the row).
	OutsideCommon int `json:"outside_common"`
}

// PairRow is one common row: the candidate's net and the reference's net (bps).
type PairRow struct {
	DecisionID int64
	At         time.Time
	Day        string
	Net, Ref   float64
}

// Diff is the paired difference candidate minus live on the common rows.
type Diff struct {
	N                    int          `json:"n"`
	Days                 int          `json:"days"`
	MeanBps              *float64     `json:"mean_bps"`
	CI                   *evaldiag.CI `json:"ci"`
	DetectableMinimumBps *float64     `json:"detectable_minimum_bps"`
	PValue               *float64     `json:"p_value"`
	// HolmAdjustedP is the Holm-Bonferroni adjusted p across the family (every candidate with a p).
	HolmAdjustedP *float64 `json:"holm_adjusted_p"`
	FamilySize    int      `json:"family_size"`
}

// Cost is a candidate's AI cost (USD) over its common rows, reported separately from the net
// bps. An unknown cost is never turned into 0.
type Cost struct {
	Rows int `json:"rows"`
	// USDTotal sums the rows that carry an amount (null when none does).
	USDTotal *float64 `json:"usd_total"`
	// USDPerRow is USDTotal / Rows, only while Complete.
	USDPerRow *float64 `json:"usd_per_row"`
	// Complete: every row carries an amount. False when some row's cost is unknown.
	Complete          bool         `json:"complete"`
	RowsWithoutAmount int          `json:"rows_without_amount"`
	BySource          []CostSource `json:"by_source"`
	Notes             []string     `json:"notes"`
}

// CostSource counts rows by where their cost comes from (reported, list_price_estimate, unknown,
// recorded, usage_estimate, none).
type CostSource struct {
	Source string   `json:"source"`
	Rows   int      `json:"rows"`
	USD    *float64 `json:"usd"`
}

// Combined is the net bps after the AI cost, computed only when usd_jpy and notional_jpy are both given.
type Combined struct {
	// Reason says why the values are null: usd_jpy_and_notional_jpy_required, cost_unknown or no_common_rows.
	Reason                string   `json:"reason,omitempty"`
	CostBpsPerRow         *float64 `json:"cost_bps_per_row"`
	MeanNetAfterAICostBps *float64 `json:"mean_net_after_ai_cost_bps"`
	// LiveCostBpsPerRow and DiffAfterAICostBps: the reference's cost on the same rows, and the mean
	// difference after the cost difference (candidate cost - reference cost).
	LiveCostBpsPerRow  *float64 `json:"live_cost_bps_per_row"`
	DiffAfterAICostBps *float64 `json:"diff_after_ai_cost_bps"`
}
