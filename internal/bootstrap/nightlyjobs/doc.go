// Package nightlyjobs is the composition root's wiring of the research-only daily jobs
// of issues #1089 (the daily record of the Jev evaluation: EvaluationTask) and #1092 (the
// nightly walk-forward training of the feature-only research model: ModelTask). Each is a
// maintenance task that is OFF by default (researchjobsettings) and writes only its own
// research tables; nothing it writes is a trading input.
package nightlyjobs
