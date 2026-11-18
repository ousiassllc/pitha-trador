package router

import (
	"github.com/ousiassllc/pitha-trador/internal/web/handler/home"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/trialresults"
)

// The options of the read-only, database-backed screens.

// WithHomeSource backs the read-only Home screen (`GET /`, issue #896) with its
// database reads (handler/home.Source); cmd/* pass bootstrap/homeview's Reader.
func WithHomeSource(source home.Source) Option {
	return func(o *options) { o.homeSource = source }
}

// WithTrialResultsSource backs the 成績 ＞ お試し tab (`GET /performance/trial`,
// handler/trialresults.Source) with the trial's aggregation, the one the MCP's
// jev_trial_compare / jev_trial_cost use; cmd/* pass bootstrap/trialview's
// Reader. The tab is read-only.
func WithTrialResultsSource(source trialresults.Source) Option {
	return func(o *options) { o.trialResults = source }
}
