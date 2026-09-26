package backtest

import "time"

// Period is a half-open [Start, End) time range.
type Period struct {
	Start time.Time
	End   time.Time
}

// Contains reports whether t falls in the half-open range [p.Start,
// p.End).
func (p Period) Contains(t time.Time) bool {
	return !t.Before(p.Start) && t.Before(p.End)
}

// Split is one Walk Forward fold (functional.md FR-BT-2): Training/
// Calibration, then Validation, then Forward period, each a contiguous,
// non-overlapping Period (Training.End == Validation.Start,
// Validation.End == Forward.Start).
type Split struct {
	Training   Period
	Validation Period
	Forward    Period
}

// WalkForwardConfig defines how Splits divides [Start, End) into
// repeated Training/Calibration → Validation → Forward folds (FR-BT-2),
// stepping StepPeriod between each fold's Training.Start.
type WalkForwardConfig struct {
	Start time.Time
	End   time.Time

	TrainingPeriod   time.Duration
	ValidationPeriod time.Duration
	ForwardPeriod    time.Duration

	// StepPeriod is how far each fold's Training.Start advances from the
	// previous fold's. Defaults to ForwardPeriod (rolling exactly one
	// Forward period ahead each time, the conventional walk-forward
	// step) when zero or negative.
	StepPeriod time.Duration
}

// Splits returns every Split fitting inside [wf.Start, wf.End), oldest
// first (FR-BT-2 "Walk Forwardを繰り返す"). It returns nil when
// TrainingPeriod/ValidationPeriod/ForwardPeriod is not positive, or the
// range is too short to fit even one fold - callers must not silently
// treat that as "the whole period, evaluated once" (the "全期間一括最適
// 化しない" requirement FR-BT-2 forbids).
func (wf WalkForwardConfig) Splits() []Split {
	step := wf.StepPeriod
	if step <= 0 {
		step = wf.ForwardPeriod
	}
	if wf.TrainingPeriod <= 0 || wf.ValidationPeriod <= 0 || wf.ForwardPeriod <= 0 || step <= 0 {
		return nil
	}

	var splits []Split
	trainingStart := wf.Start
	for {
		validationStart := trainingStart.Add(wf.TrainingPeriod)
		forwardStart := validationStart.Add(wf.ValidationPeriod)
		forwardEnd := forwardStart.Add(wf.ForwardPeriod)
		if forwardEnd.After(wf.End) {
			break
		}
		splits = append(splits, Split{
			Training:   Period{Start: trainingStart, End: validationStart},
			Validation: Period{Start: validationStart, End: forwardStart},
			Forward:    Period{Start: forwardStart, End: forwardEnd},
		})
		trainingStart = trainingStart.Add(step)
	}
	return splits
}
