// Package counterlabel persists the counterfactual labels of skipped candidates
// (issue #1052, parent #752): the triple-barrier outcome "had it entered" of a
// LONG/SHORT Jev decision the guards skipped and the per-minute outcome
// labeling left without a calibration_outcomes row (counterfactual_labels,
// migration 000050).
//
// The record is for learning and analysis only. Policy, Risk, order placement,
// the Paper guard, Live and the Jev prompts never read it: the only readers are
// internal/repository/metalabel (the nightly retraining, and only while the
// Settings switch metalabel.include_skipped_labels is on) and the recorder that
// writes it (internal/service/calibration/counterfactual, wired by
// internal/bootstrap/metalabeljob). isolation_test.go pins that.
//
// It reads jev_decisions, trade_signals, calibration_outcomes and
// calibration_label_skips only through SQL. It MUST depend only on
// internal/domain and internal/repository/sqlutil. See
// docs/architecture/overview.md §3.
package counterlabel
