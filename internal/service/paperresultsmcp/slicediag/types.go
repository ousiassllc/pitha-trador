package slicediag

import "github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"

// Limits of a bucket's own sample: below either, the bucket is flagged low_sample
// (it stays in its multiple-comparison family). They are descriptive flags, not
// verdict thresholds.
const (
	// MinBucketRows is the fewest decisions a bucket needs not to be flagged.
	MinBucketRows = 30
	// MinBucketDays is the fewest JST days a bucket needs not to be flagged.
	MinBucketDays = 5
	// MaxSectorBuckets is the most sector buckets (the rest are "other").
	MaxSectorBuckets = 20
)

// The families of dimensions. Holm is applied inside a family only.
const (
	FamilyPreregistered = "preregistered"
	FamilyExploratory   = "exploratory"
)

// Pre-registration of the preregistered family: the dimensions below were fixed in
// code on this date, for this issue, before any result was looked at.
const (
	PreregisteredOn    = "2026-11-15"
	PreregisteredIssue = "#1089"
)

// Reasons a bucket is flagged low_sample.
const (
	ReasonRowsBelowMin = "rows_below_min"
	ReasonDaysBelowMin = "days_below_min"
)

// Bucket is the LLM arm's net return (bps, after the label's cost) on one bucket's
// decisions. MeanNetBps, CI, WinRate, DetectableMinimumEdgeBps and PValue are null
// while undefined (no row; CI, the minimum edge and p also with fewer than two days).
// HolmAdjustedP is the Holm-adjusted PValue inside the bucket's family (null without
// a p-value, and for the cohort's "all" reference, which is in no family). Nothing here
// is a verdict.
type Bucket struct {
	Bucket                   string       `json:"bucket"`
	N                        int          `json:"n"`
	Days                     int          `json:"days"`
	MeanNetBps               *float64     `json:"mean_net_bps"`
	CI                       *evaldiag.CI `json:"ci"`
	WinRate                  *float64     `json:"win_rate"`
	DetectableMinimumEdgeBps *float64     `json:"detectable_minimum_edge_bps"`
	PValue                   *float64     `json:"p_value"`
	HolmAdjustedP            *float64     `json:"holm_adjusted_p"`
	LowSample                bool         `json:"low_sample"`
	LowSampleReasons         []string     `json:"low_sample_reasons"`
}

// Dimension is one cut of the cohort's evaluated rows: every row is in exactly one
// bucket, so the buckets' n add up to Rows (the cohort's evaluated rows). Buckets are
// in a fixed order, never sorted by a result. Edges are the tercile boundaries (the
// first value of the mid and of the high tercile) of a volatility / turnover / spread
// dimension, null otherwise or with fewer than three rows with a value.
type Dimension struct {
	Dimension string    `json:"dimension"`
	Rows      int       `json:"rows"`
	Edges     []float64 `json:"edges,omitempty"`
	Buckets   []Bucket  `json:"buckets"`
}

// Family is the dimensions of one family. FamilySize is the number of buckets (over
// ALL the family's dimensions, also those not asked for) with a p-value: the m of
// the Holm correction. Buckets is the number of all the family's buckets.
type Family struct {
	Family     string      `json:"family"`
	FamilySize int         `json:"family_size"`
	Buckets    int         `json:"buckets"`
	Dimensions []Dimension `json:"dimensions"`
}

// Cohort is one (model_id, question_version) pair's slices, as jev_evaluation
// counts the pair: Evaluated rows are the ones of its comparison set.
type Cohort struct {
	ModelID         string   `json:"model_id"`
	QuestionVersion string   `json:"question_version"`
	Decisions       int      `json:"decisions"`
	Evaluated       int      `json:"evaluated"`
	All             Bucket   `json:"all"`
	Families        []Family `json:"families"`
}

// Preregistration states what was fixed in advance.
type Preregistration struct {
	Date       string   `json:"date"`
	Issue      string   `json:"issue"`
	Dimensions []string `json:"dimensions"`
	Statement  string   `json:"statement"`
}

// Statistics are the fixed constants behind every interval, p-value and flag.
type Statistics struct {
	Method        string  `json:"method"`
	Alpha         float64 `json:"alpha"`
	Power         float64 `json:"power"`
	Resamples     int     `json:"resamples"`
	Seed          uint64  `json:"seed"`
	Correction    string  `json:"correction"`
	MinBucketRows int     `json:"min_bucket_rows"`
	MinBucketDays int     `json:"min_bucket_days"`
}

// LabelAssumptions are the label settings in force, as in jev_evaluation.
type LabelAssumptions struct {
	HorizonMinutes int     `json:"horizon_minutes"`
	TakeProfitBps  float64 `json:"take_profit_bps"`
	StopLossBps    float64 `json:"stop_loss_bps"`
	BarrierCostBps float64 `json:"barrier_cost_bps"`
}

// Data is jev_evaluation_slices' data.
type Data struct {
	From             string            `json:"from"`
	To               string            `json:"to"`
	HorizonMinutes   int               `json:"horizon_minutes"`
	PromptVersion    string            `json:"prompt_version,omitempty"`
	ModelID          string            `json:"model_id,omitempty"`
	Dimensions       []string          `json:"dimensions"`
	Notice           string            `json:"notice"`
	ExploratoryNote  string            `json:"exploratory_note"`
	Preregistration  Preregistration   `json:"preregistration"`
	Decisions        int               `json:"decisions"`
	CutoffApplied    bool              `json:"llm_cutoff_applied"`
	LabelAssumptions LabelAssumptions  `json:"label_assumptions"`
	Statistics       Statistics        `json:"statistics"`
	Cohorts          []Cohort          `json:"cohorts"`
	Definitions      map[string]string `json:"definitions"`
	Caveats          []string          `json:"caveats"`
}
