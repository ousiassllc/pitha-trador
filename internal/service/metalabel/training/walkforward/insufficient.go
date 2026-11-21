package walkforward

// InsufficientDataError is the error behind ErrInsufficientData: it carries the
// fixed reason code and the counts the nightly job records when it skips a
// retraining (issue #799), so no reader has to parse the message.
// errors.Is(err, ErrInsufficientData) holds for it.
type InsufficientDataError struct {
	// Code is one of the repo.Skip* codes.
	Code string
	// Usable of Required: the usable labeled decisions before the OOS window
	// (SkipTooFewRowsBeforeOOS), those counted toward the fit and calibration
	// minimums after purging (SkipTooFewRowsAfterPurge) or the rows that could
	// not be learned from (SkipTrainingDataUnusable), against what fit and
	// calibration need together ("必要130件中 x件").
	Usable, Required int
	// OOSRows of RequiredOOSRows: the rows in the OOS window against the rows
	// the window reaches back to hold (the bar's MinOOSSamples), so a skip shows
	// both sides of the split (issue #862). Set where the split fails.
	OOSRows, RequiredOOSRows *int
	// DroppedIncomplete, LabelV1, LabelV2 are filled by the Trainer: the stored
	// rows BuildRows dropped for a missing feature, and the labeled decisions of
	// the lookback per label version (v1 close-only, v2 triple-barrier). They tell
	// a thin history from rows that exist but cannot be used. All the counts are
	// nil while not measured (a failed count stays nil) and 0 only when measured
	// as 0; they are logged as "unknown" and stored as NULL when nil.
	DroppedIncomplete, LabelV1, LabelV2 *int
	// Detail is the human-readable message (logs only; never persisted).
	Detail string
	// Cause is the learner's error for SkipTrainingDataUnusable.
	Cause error
}

func (e *InsufficientDataError) Error() string {
	msg := ErrInsufficientData.Error() + ": " + e.Detail
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap lets errors.Is match ErrInsufficientData and the learner's error.
func (e *InsufficientDataError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrInsufficientData}
	}
	return []error{ErrInsufficientData, e.Cause}
}
