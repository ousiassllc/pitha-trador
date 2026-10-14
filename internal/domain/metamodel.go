package domain

import "time"

// MetaModelCalibrationBin is one point of the meta-labeling model's OOS
// reliability curve (issue #766): the mean calibrated probability of a
// probability bin against the share of its OOS samples that actually won.
type MetaModelCalibrationBin struct {
	MeanPredicted float64
	ObservedRate  float64
	Count         int
}

// MetaModelReport is what the Calibration screen shows about the newest
// meta-labeling model (issue #766, parent #752): its out-of-sample metrics
// and the Jev evaluation period (the OOS window, which never starts before
// the LLM's training-data cutoff).
type MetaModelReport struct {
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
	// OOSAUC and OOSEVBps (mean cost-adjusted EV of the OOS samples the
	// calibrated model would take) are nil when undefined.
	OOSAUC         *float64
	OOSEVBps       *float64
	OOSCalibration []MetaModelCalibrationBin
	// ActiveVersion is the version of the model currently in force ("" when
	// none: entries are skipped). It differs from Version when the newest
	// retraining was rejected and an older model stays in force.
	ActiveVersion string
	// LLMCutoff is the Settings LLM training-data cutoff ("YYYY-MM-DD", UTC),
	// before which no decision is evaluated.
	LLMCutoff string
}
