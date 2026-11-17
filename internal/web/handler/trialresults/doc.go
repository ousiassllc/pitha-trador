// Package trialresults serves the 成績 ＞ お試し tab (`GET /performance/trial`):
// the read-only results of the model-comparison trial. The figures are the ones
// the Paper results MCP's jev_trial_compare and jev_trial_cost return
// (internal/service/paperresultsmcp/query/trialquery, called and not
// reimplemented), so the screen and those tools agree. The handler only reads:
// it registers no POST route, and the trial is switched on and configured on
// the Settings screen only. It depends only on internal/service and
// handler/shared (plus Templ under internal/web); it MUST NOT import sibling
// handler subpackages.
package trialresults
