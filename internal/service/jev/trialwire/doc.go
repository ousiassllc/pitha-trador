// Package trialwire is the HTTP client of the trial arms (issues #1058,
// #1059): it asks a non-live decision model (OpenRouter's Clef, Clef-flash,
// Gemini Decision 1, ...) the FR-TRADER-1 question group about the same state
// the live Jev answered, reading the answer with the trial's looser
// validation (systemone.DecodeTrial).
//
// It is deliberately NOT jev.Client. The live client (and its strict
// validation, retry policy, rolling error rate behind jev_api_down and its
// defaults: jev.DefaultBaseURL, jev.DefaultModel, JEV_*) stays untouched; a
// trial client has its own base URL, endpoint, key, model, routing preference
// and http.Client, makes one attempt per call (a paid call is never retried;
// the next live decision is the next chance) and feeds nothing but its caller.
package trialwire
