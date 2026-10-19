// Package metamodel holds the meta-labeling model report the Calibration screen
// shows (issue #766, parent #752), with the adoption record of issue #780. It is
// its own package to keep internal/domain within the linterly directory budget.
package metamodel

import "time"

// CalibrationBin is one point of the model's OOS reliability curve: the mean
// calibrated probability of a probability bin against the share of its OOS
// samples that actually won.
type CalibrationBin struct {
	MeanPredicted float64
	ObservedRate  float64
	Count         int
}

// Interval is a confidence interval.
type Interval struct{ Low, High float64 }

// Criteria is the adoption bar the model was judged against.
type Criteria struct {
	MinOOSSamples, MinTakes, MinOOSDays int
	// MinEVLowerBps and MinAUCLower: the lower confidence limit of the EV /
	// AUC must exceed them.
	MinEVLowerBps, MinAUCLower float64
	// Alpha is the one-sided tail of the bootstrap intervals, Resamples their size.
	Alpha     float64
	Resamples int
}

// Comparison is the candidate versus the active model on the same OOS rows.
type Comparison struct {
	ActiveVersion string
	Comparable    bool
	Note          string
	PairedSamples int
	// CandidateBps, ActiveBps: mean net return per decision opportunity (a skip
	// earns 0); DiffBps = candidate - active, with DiffCI its interval (nil when undefined).
	CandidateBps, ActiveBps, DiffBps float64
	DiffCI                           *Interval
}

// Gate is the change-rate limit that blocked an otherwise adoptable model.
type Gate struct {
	Reason, Detail string
	// Until is when the block ends.
	Until string
}

// Forward is the holdout evaluation of the model after its training (never used
// for the adoption decision). EvaluatedAt is empty until the first evaluation.
type Forward struct {
	EvaluatedAt string
	// From is the start of the forward window (the model's trained_at).
	From                 string
	Samples, Takes, Days int
	EVBps                *float64
	EVCI                 *Interval
	// Degraded: the EV interval's upper limit is below 0.
	Degraded bool
}

// Report is what the Calibration screen shows about the newest meta-labeling
// model: its out-of-sample metrics, the Jev evaluation period (the OOS window,
// never before the LLM's training-data cutoff) and how it was judged.
type Report struct {
	Version   string
	Algorithm string
	// Status is "active" (the model is in force) or "rejected" (kept for the
	// record only; RejectReason says why).
	Status       string
	RejectReason string
	TrainedAt    time.Time
	// TrainStart/TrainEnd and OOSStart/OOSEnd are the walk-forward windows.
	// OOSStart..OOSEnd is the Jev evaluation period.
	TrainStart, TrainEnd time.Time
	OOSStart, OOSEnd     time.Time
	TrainSamples         int
	OOSSamples           int
	// OOSTakes and OOSDays: the signals the calibrated model took and the
	// trading days (JST) the OOS rows span. OOSEVCI/OOSAUCCI are the cluster-
	// bootstrap intervals of the EV and AUC, nil when undefined.
	OOSTakes, OOSDays int
	OOSEVCI, OOSAUCCI *Interval
	// OOSAUC and OOSEVBps (mean cost-adjusted EV of the OOS samples the
	// calibrated model would take) are nil when undefined.
	OOSAUC         *float64
	OOSEVBps       *float64
	OOSCalibration []CalibrationBin
	// Criteria, Comparison and Gate record how the model was judged (nil when
	// the row predates issue #780).
	Criteria   *Criteria
	Comparison *Comparison
	Gate       *Gate
	// Forward is the holdout evaluation of the model in force.
	Forward Forward
	// ActiveVersion is the version of the model currently in force ("" when
	// none: entries are skipped). It differs from Version when the newest
	// retraining was rejected and an older model stays in force.
	ActiveVersion string
	// LLMCutoff is the Settings LLM training-data cutoff ("YYYY-MM-DD", UTC),
	// before which no decision is evaluated.
	LLMCutoff string
}

// RetrainSkip is one nightly retraining that stored no model for lack of
// usable labeled history (issue #799). Code is a fixed reason code (the
// internal/repository/metalabel Skip* constants), never free text.
type RetrainSkip struct {
	SkippedAt time.Time
	Code      string
	// UsableRows of RequiredRows: "必要130件中 x件".
	UsableRows, RequiredRows int
}
