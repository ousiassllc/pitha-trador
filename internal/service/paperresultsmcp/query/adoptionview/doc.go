// Package adoptionview derives metalabel_history's adoption diagnostics
// (issue #1045) from the values metalabel_models stores for one model: the OOS
// results, the adoption criteria it was judged against, the numeric checks of
// training.AcceptanceConfig.Judge, the comparison with the model then in force,
// the change-rate gate and the forward evaluation.
//
// It only reads what is stored. A value that was not stored is null, never 0,
// and the criteria are never filled from today's defaults. Free text of the
// adoption record (Comparison.Note, Gate.Detail) is not part of any output type.
// It is a package of its own, below query, to keep query within the linterly
// directory budget; it must not import query.
package adoptionview
