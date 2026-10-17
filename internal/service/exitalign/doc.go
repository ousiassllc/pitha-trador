// Package exitalign compares the basis the meta-labeling model is trained on
// (the triple-barrier label: TP/SL/timeout/cost, internal/config/barriersettings
// and costsettings) with the exit Paper Trading actually applies
// (internal/service/execution.Config) - issue #774 (parent #751/#752).
//
// Correspondence is the one pure description of how the two map onto each
// other; docs/requirements/functional/components-pipeline.md FR-CAL-4 carries
// the same table at the shipped defaults and table_docs_test.go fails when
// either side changes alone. Reader assembles the Calibration screen's view:
// the table, the mismatch warnings (the label basis differs from the actual
// exit, or from the fill model's cost), the per-decision label-versus-replay
// comparison and the OOS EV beside the realized paper return. All of it is
// computed on read from existing tables.
package exitalign
