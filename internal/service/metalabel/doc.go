// Package metalabel is the meta-labeling (入る/見送る判定モデル) service of
// docs/architecture/er/tables-metalabel.md (issue #757, child of #752).
//
// Store (store.go) is its persistence layer: it validates and saves each
// trained model with its walk-forward OOS metrics, returns the latest active
// model, and records the per-decision predictions. Callers MUST treat "no
// active model" (LatestActiveModel's found == false) as skip, never take.
//
// This package depends only on internal/repository
// (internal/service/doc.go).
package metalabel
