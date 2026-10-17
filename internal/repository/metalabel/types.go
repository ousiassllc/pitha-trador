package metalabel

import "time"

// Model statuses (metalabel_models.status; issue #757, see
// docs/architecture/er/tables-metalabel.md).
const (
	// ModelActive: OOS metrics passed the bar; inference may use it.
	ModelActive = "active"
	// ModelRejected: kept for the record only (see RejectReason).
	ModelRejected = "rejected"
)

// Meta-label actions (metalabel_predictions.action).
const (
	ActionTake = "take"
	ActionSkip = "skip"
)

// CalibrationBin is one point of a model's OOS reliability curve:
// the mean calibrated probability of a probability bin against the share of
// its OOS samples that actually won.
type CalibrationBin struct {
	MeanPredicted float64 `json:"mean_predicted"`
	ObservedRate  float64 `json:"observed_rate"`
	Count         int     `json:"count"`
}

// Model mirrors one metalabel_models row: a trained meta-labeling
// model's version, walk-forward windows and OOS metrics.
type Model struct {
	ID        int64
	Version   string
	Algorithm string
	// LabelVersion is the calibration_outcomes.label_version trained on.
	LabelVersion int
	Status       string
	// RejectReason is why a rejected model was not adopted; empty otherwise.
	RejectReason string
	TrainedAt    time.Time
	// The walk-forward training and out-of-sample windows. The OOS window is
	// kept so the Jev-feature evaluation (after the LLM's cutoff) is shown
	// against it.
	TrainStart, TrainEnd time.Time
	OOSStart, OOSEnd     time.Time
	TrainSamples         int
	OOSSamples           int
	FeatureNames         []string
	ParamsJSON           string
	// OOSAUC and OOSExpectedValueBps (mean cost-adjusted EV of the OOS
	// samples the calibrated model would take) are nil when undefined.
	OOSAUC              *float64
	OOSExpectedValueBps *float64
	OOSCalibration      []CalibrationBin
	CreatedAt           time.Time
}

// Prediction mirrors one metalabel_predictions row: a model's
// verdict on one Jev decision.
type Prediction struct {
	ID            int64
	ModelID       int64
	JevDecisionID int64
	PredictedAt   time.Time
	// Win probability before and after calibration.
	RawProbability        float64
	CalibratedProbability float64
	// ExpectedValueBps is the cost-adjusted EV of taking the signal.
	ExpectedValueBps float64
	// Action is ActionTake or ActionSkip.
	Action string
	// SizeFraction is the Kelly-based size share (0..1); 0 for a skip. It is
	// recorded for analysis only and is NOT used for the order quantity (issue #781).
	SizeFraction float64
	Reason       string
	// FeaturesJSON is the feature vector scored, for audit.
	FeaturesJSON string
	CreatedAt    time.Time
}
