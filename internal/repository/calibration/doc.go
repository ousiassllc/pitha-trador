// Package calibration persists the Jev calibration data:
// calibration_outcomes (CalibrationRepository, calibration_repo.go) and the
// calibration_label_skips marks that stop the labeling job from re-enqueuing
// a pair that can never be labeled (calibration_skip.go).
//
// It reads jev_decisions (written by internal/repository/judgement) only
// through SQL joins. It MUST depend only on internal/domain and
// internal/repository/sqlutil. See docs/architecture/overview.md §3.
package calibration
