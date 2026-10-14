// Package metalabel persists the meta-labeling model data: metalabel_models
// (the model versions with their OOS metrics) and metalabel_predictions (the
// per-decision predictions), both in repo.go / prediction_repo.go, and
// defines their row types (types.go).
//
// predictions reference jev_decisions (written by
// internal/repository/judgement) only through the foreign key. It MUST
// depend only on internal/repository/sqlutil. See
// docs/architecture/overview.md §3.
//
// The row types live here, not in internal/domain: that directory is near
// the 2000-line limit enforced by linterly.
package metalabel
