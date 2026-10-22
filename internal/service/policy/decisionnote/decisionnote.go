// Package decisionnote writes what a Policy decision was judged on next to its
// reject reason (issue #881). trade_signals.reject_reason is the only persisted
// free text of a decision, and a migration was out of scope, so a note is
// appended to it: "<reason>; <code>: <detail>", or the note alone for a signal
// that passed (the one case where reject_reason is set while risk_passed is
// true; docs/architecture/er/tables-trading.md). The leading reason is never
// changed, so every reader of its prefix is unaffected.
package decisionnote

import (
	"log/slog"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/textutil"
)

// Codes of the notes.
const (
	// CalibrationAllVersions: the legacy rule judged the confidence bucket's
	// sample count on the total over every Trader prompt version, because the
	// live version's own samples were too few. Paper only.
	CalibrationAllVersions = "calibration_all_versions"
	// MetaModelCohortMismatch: the meta-labeling model that scored the candidate
	// was trained on another prompt cohort than the live version's. Paper only:
	// Live does not use such a model.
	MetaModelCohortMismatch = "meta_model_cohort_mismatch"
)

const separator = "; "

// For is the notes of one decision: the calibration one when the legacy rule
// (bootstrap) passed on the all-versions fallback (calibrated with a non-empty
// calibration detail), and the cohort one when the candidate reached a model
// that was trained on another cohort (cohortMismatch, from metacohort). Each is
// also logged as a warning with the decision's instrument and symbol.
func For(instrumentID int64, symbol string, bootstrap, calibrated bool, calibration, cohortMismatch string, reachedModel bool) []string {
	var notes []string
	if bootstrap && calibrated && calibration != "" {
		notes = append(notes, Note(CalibrationAllVersions, calibration))
	}
	if reachedModel && cohortMismatch != "" {
		notes = append(notes, Note(MetaModelCohortMismatch, cohortMismatch))
	}
	for _, n := range notes {
		slog.Warn("policy: decision judged on a fallback or a mismatching meta model (Paper)", "instrument_id", instrumentID, "symbol", symbol, "note", n)
	}
	return notes
}

// Note is "<code>: <detail>".
func Note(code, detail string) string { return code + ": " + detail }

// Append returns reason followed by notes within maxBytes: the reason gives way
// before a note does. A nil or empty reason gives the notes alone; no notes,
// reason unchanged.
func Append(reason *string, maxBytes int, notes ...string) *string {
	if len(notes) == 0 {
		return reason
	}
	joined := strings.Join(notes, separator)
	out := joined
	if reason != nil && *reason != "" {
		out = textutil.Truncate(*reason, max(0, maxBytes-len(separator)-len(joined))) + separator + joined
	}
	out = textutil.Truncate(out, maxBytes)
	return &out
}
