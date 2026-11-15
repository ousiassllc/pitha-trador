// Package trialresults renders the 成績 ＞ お試し tab: the read-only results of
// the model-comparison trial (missing rate, skips, cost by source and the NONE /
// counterfactual proxy figures). It takes plain props (internal/web/handler/trialresults
// maps the trialquery aggregation that jev_trial_compare and jev_trial_cost
// return onto them), so it stays decoupled from internal/service. It has no
// form, button or link that writes: the trial is switched on and configured on
// the Settings screen only. It shows no winner, vote or promotion. It is its
// own package because internal/web/organisms is at its line limit.
package trialresults
