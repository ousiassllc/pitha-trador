package metalabel

import (
	"context"
	"encoding/json"
	"fmt"
)

// Interval is a confidence interval [Low, High] (issue #780).
type Interval struct {
	Low  float64 `json:"low"`
	High float64 `json:"high"`
}

// Criteria is the adoption bar a model was judged against, recorded with the
// result so a stored row stays interpretable after the defaults change.
type Criteria struct {
	MinOOSSamples int `json:"min_oos_samples"`
	MinTakes      int `json:"min_takes"`
	MinOOSDays    int `json:"min_oos_days"`
	// MinEVLowerBps and MinAUCLower: the lower confidence limit of the OOS EV /
	// AUC must exceed them.
	MinEVLowerBps float64 `json:"min_ev_lower_bps"`
	MinAUCLower   float64 `json:"min_auc_lower"`
	// Alpha is the one-sided tail of the bootstrap interval ([Alpha, 1-Alpha]
	// percentiles); Resamples the bootstrap size; Seed makes it reproducible.
	Alpha     float64 `json:"alpha"`
	Resamples int     `json:"resamples"`
	Seed      uint64  `json:"seed"`
}

// Comparison is the candidate-versus-active evaluation on the same OOS rows.
type Comparison struct {
	ActiveVersion string `json:"active_version"`
	// Comparable is false when the active model could not be evaluated on
	// the candidate's OOS rows (the current model is then kept); Note says why.
	Comparable bool   `json:"comparable"`
	Note       string `json:"note,omitempty"`
	// PairedSamples is the number of OOS rows both models scored.
	PairedSamples int `json:"paired_samples"`
	// CandidateBps and ActiveBps are each model's mean net return per OOS
	// decision opportunity (a skip earns 0); DiffBps is their difference
	// (candidate - active) with its confidence interval.
	CandidateBps float64   `json:"candidate_bps"`
	ActiveBps    float64   `json:"active_bps"`
	DiffBps      float64   `json:"diff_bps"`
	DiffCI       *Interval `json:"diff_ci,omitempty"`
	// ActiveEVCI is the confidence interval of the active model's mean EV per
	// take on the same OOS rows: when its lower limit is above 0 the active
	// model still passes the bar and must be beaten significantly.
	ActiveEVCI *Interval `json:"active_ev_ci,omitempty"`
}

// Gate records a change-rate limit that blocked an otherwise adoptable model.
type Gate struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
	// Until is when the block ends (RFC3339, UTC).
	Until string `json:"until"`
}

// Adoption is metalabel_models.adoption_json.
type Adoption struct {
	Criteria   Criteria    `json:"criteria"`
	Comparison *Comparison `json:"comparison,omitempty"`
	Gate       *Gate       `json:"gate,omitempty"`
	// TrainedCohort is the Trader prompt cohort (internal/service/jev cohort.go,
	// e.g. "entry-quality-v3") the model's training rows came from: the live
	// version's cohort when it was trained (issue #881). Stored here rather than
	// in a column so no migration is needed. Empty for a model stored before it
	// was recorded: it counts as the pre-promotion cohort
	// (jev.LegacyModelCohort).
	TrainedCohort string `json:"trained_cohort,omitempty"`
}

// Forward is metalabel_models.forward_json: the forward (holdout) evaluation of
// a model on decisions made after its training ended, which no adoption
// decision ever used. Empty (zero EvaluatedAt) until the first nightly run
// after adoption.
type Forward struct {
	EvaluatedAt string `json:"evaluated_at,omitempty"`
	// From is the start of the forward window: the model's trained_at.
	From    string `json:"from,omitempty"`
	Samples int    `json:"samples"`
	Takes   int    `json:"takes"`
	Days    int    `json:"days"`
	// EVBps is the mean net return per taken decision with its interval; nil
	// until there is a take.
	EVBps *float64  `json:"ev_bps,omitempty"`
	EVCI  *Interval `json:"ev_ci,omitempty"`
	// Degraded is true when the interval's upper limit is below 0: the model
	// loses money out of sample (a warning only; adoption criteria are separate).
	Degraded bool `json:"degraded"`
}

// UpdateForward stores the forward evaluation of model id. It never changes
// the model's status or adoption record.
func (r *Repository) UpdateForward(ctx context.Context, id int64, f Forward) error {
	raw, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("repository: encode forward evaluation of meta-label model %d: %w", id, err)
	}
	res, err := r.db.ExecContext(ctx, `UPDATE metalabel_models SET forward_json = ? WHERE id = ?`, string(raw), id)
	if err != nil {
		return fmt.Errorf("repository: update forward evaluation of meta-label model %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: read forward evaluation update result of meta-label model %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("repository: meta-label model %d not found for its forward evaluation", id)
	}
	return nil
}
