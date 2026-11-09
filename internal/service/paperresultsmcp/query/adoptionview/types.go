package adoptionview

// adoption_recorded_status values.
const (
	// StatusRecorded: adoption_json holds the criteria the model was judged by.
	StatusRecorded = "recorded"
	// StatusNotRecorded: a row stored before issue #780 (adoption_json is "{}"
	// and oos_takes / oos_days are the column default 0, not results).
	StatusNotRecorded = "not_recorded"
	// StatusUnreadable: adoption_json is not valid for this build.
	StatusUnreadable = "unreadable"
)

// comparison_status values.
const (
	ComparisonRecorded = "recorded"
	// ComparisonNotRecorded: the adoption record itself is missing or unreadable.
	ComparisonNotRecorded = "not_recorded"
	// ComparisonNotPerformed: the model was judged but no comparison was stored:
	// it did not get past the adoption bar or the cutoff gate, or no model was in force.
	ComparisonNotPerformed = "not_performed"
)

// forward_status values.
const (
	ForwardEvaluated = "evaluated"
	// ForwardNotEvaluated: the model has an adoption record but forward_json is
	// empty (a rejected model, or one adopted since the last nightly run).
	ForwardNotEvaluated = "not_evaluated"
	// ForwardNotRecorded: forward_json is unreadable, or empty on a row without
	// an adoption record (stored before the evaluation existed).
	ForwardNotRecorded = "not_recorded"
)

// Names of the numeric checks, in training.AcceptanceConfig.Judge's order.
const (
	CheckOOSSamples  = "oos_samples"
	CheckOOSDays     = "oos_days"
	CheckOOSTakes    = "oos_takes"
	CheckOOSEVCILow  = "oos_ev_ci_low"
	CheckOOSAUCCILow = "oos_auc_ci_low"
)

// ReasonOther stands for a gate reason this build does not know.
const ReasonOther = "other"

// Interval is a confidence interval.
type Interval struct {
	Low  float64 `json:"low"`
	High float64 `json:"high"`
}

// Criteria is the adoption bar one model was judged against (names as in the
// Calibration API's meta_model.criteria; the bootstrap seed is not returned).
type Criteria struct {
	MinOOSSamples int     `json:"min_oos_samples"`
	MinTakes      int     `json:"min_takes"`
	MinOOSDays    int     `json:"min_oos_days"`
	MinEVLowerBps float64 `json:"min_ev_lower_bps"`
	MinAUCLower   float64 `json:"min_auc_lower"`
	Alpha         float64 `json:"alpha"`
	Resamples     int     `json:"resamples"`
}

// Check is one numeric criterion: the stored result, the bar and whether the
// result meets it. Met is null when either side is not known (criteria not
// recorded, result not recorded or CI undefined): unknown, not false.
type Check struct {
	Actual   *float64 `json:"actual"`
	Required *float64 `json:"required"`
	Met      *bool    `json:"met"`
}

// Checks are the five numeric criteria of training.AcceptanceConfig.Judge.
type Checks struct {
	OOSSamples  Check `json:"oos_samples"`
	OOSDays     Check `json:"oos_days"`
	OOSTakes    Check `json:"oos_takes"`
	OOSEVCILow  Check `json:"oos_ev_ci_low"`
	OOSAUCCILow Check `json:"oos_auc_ci_low"`
}

// Comparison is the candidate versus the model then in force (no free-text note).
type Comparison struct {
	ActiveVersion string    `json:"active_version"`
	Comparable    bool      `json:"comparable"`
	PairedSamples int       `json:"paired_samples"`
	CandidateBps  float64   `json:"candidate_bps"`
	ActiveBps     float64   `json:"active_bps"`
	DiffBps       float64   `json:"diff_bps"`
	DiffCI        *Interval `json:"diff_ci"`
	ActiveEVCI    *Interval `json:"active_ev_ci"`
}

// Gate is the change-rate limit that blocked an otherwise adoptable model
// (a fixed reason code and when the block ends; no free-text detail).
type Gate struct {
	Reason string `json:"reason"`
	Until  string `json:"until"`
}

// Forward is the holdout evaluation after training.
type Forward struct {
	EvaluatedAt string    `json:"evaluated_at"`
	From        string    `json:"from"`
	Samples     int       `json:"samples"`
	Takes       int       `json:"takes"`
	Days        int       `json:"days"`
	EVBps       *float64  `json:"ev_bps"`
	EVCI        *Interval `json:"ev_ci"`
	Degraded    bool      `json:"degraded"`
}

// View is the adoption diagnostics of one model. It is embedded in
// query.Model, so its fields sit beside the model's own in the JSON.
type View struct {
	// OOSTakes and OOSDays are null unless AdoptionRecordedStatus is recorded:
	// before issue #780 the columns hold their default 0, which is not a result.
	OOSTakes *int64    `json:"oos_takes"`
	OOSDays  *int64    `json:"oos_days"`
	OOSEVCI  *Interval `json:"oos_ev_ci"`
	OOSAUCCI *Interval `json:"oos_auc_ci"`
	// TakeRate is oos_takes / oos_samples; null when either is unknown or oos_samples is 0.
	TakeRate               *float64  `json:"take_rate"`
	AdoptionRecordedStatus string    `json:"adoption_recorded_status"`
	Criteria               *Criteria `json:"criteria"`
	NumericChecks          Checks    `json:"numeric_checks"`
	// FirstUnmet is the first numeric check with met = false, in Judge's order,
	// from the numeric checks only: it is not matched to reject_reason, which the
	// cutoff gate, the comparison and the change-rate limit can decide instead.
	// With NumericComplete false it is the first KNOWN unmet check.
	FirstUnmet *string `json:"first_unmet"`
	// NumericComplete is true when no numeric check is unknown (met is never null).
	NumericComplete  bool        `json:"numeric_complete"`
	Comparison       *Comparison `json:"comparison"`
	ComparisonStatus string      `json:"comparison_status"`
	Gate             *Gate       `json:"gate"`
	Forward          *Forward    `json:"forward"`
	ForwardStatus    string      `json:"forward_status"`
}
