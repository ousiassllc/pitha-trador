// Package calibration is Calibration (docs/architecture/overview.md §4,
// functional.md §4.12): Outcome Labeling turns each Jev trader decision
// (jev_decisions, decision_type=trader) into one calibration_outcomes row
// per judgment horizon (labeler.go, FR-CAL-4), and Metrics aggregates
// every labeled decision into the Brier Score/Log Loss/Expected
// Calibration Error/Reliability Curve GET /api/v1/calibration serves
// (metrics.go, FR-CAL-2/3).
//
// This package depends only on internal/domain and internal/repository
// (internal/service/doc.go), and in turn internal/service/scheduler
// depends only on internal/domain and internal/repository too (its own
// doc.go) - so it constructs judgement.OutcomeLabelJobPayload directly
// rather than importing this package, and Labeler.HandleJob decodes that
// same repository-defined payload shape.
package calibration
