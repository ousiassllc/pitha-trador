package jev

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// NewsSource supplies an instrument's recent Luna-classified news
// (FR-LUNA-3). *internal/service/newsfeed.Service implements it. ok=false
// means "no news context" - the state is sent without one.
type NewsSource interface {
	NewsContext(symbol string) (domain.NewsContext, bool)
}

// Option configures a Scout or Trader beyond their required arguments.
type Option func(*options)

type options struct {
	news           NewsSource
	recorder       ScoutRecorder
	now            func() time.Time
	snapshotMaxAge time.Duration
}

// WithClock sets the wall clock Scout.HandleJob judges snapshot staleness
// against (default time.Now; tests pin it). Scout only.
func WithClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
}

// WithSnapshotMaxAge sets how old the latest bar may be before
// Scout.HandleJob skips the job as stale (default domain.MaxSnapshotAge,
// the ranking-watch value; full-scan mode passes the longer full-scan age,
// issue #686). Scout only.
func WithSnapshotMaxAge(d time.Duration) Option {
	return func(o *options) { o.snapshotMaxAge = d }
}

// ScoutRecorder is told each Jev Scout verdict as it is reached, so the
// Scanner Dashboard can show the Scout stage of the scan funnel (issue
// #303). *internal/service/screener.LiveSource implements it.
type ScoutRecorder interface {
	RecordScout(symbol string, outcome domain.ScoutOutcome)
}

// WithScoutRecorder makes Scout report every FR-SCOUT-2 verdict (and Jev
// call failure) for a job handled via HandleJob to r. Scout only.
func WithScoutRecorder(r ScoutRecorder) Option {
	return func(o *options) { o.recorder = r }
}

// WithNewsSource makes Scout/Trader inject news_context into every state
// they send to Jev (and therefore into jev_decisions.state_json),
// FR-LUNA-3. Without it (or when the source has nothing for a symbol) the
// state carries no news_context, exactly as before Luna existed
// (FR-LUNA-4).
func WithNewsSource(source NewsSource) Option {
	return func(o *options) { o.news = source }
}

func newOptions(opts []Option) options {
	o := options{now: time.Now, snapshotMaxAge: domain.MaxSnapshotAge}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// withNewsContext returns state with news_context filled in from source
// when state does not already carry one. It only adds context; no field
// Jev computes or any Jev response is touched (FR-LUNA-5).
func withNewsContext(state ScoutState, source NewsSource) ScoutState {
	if source == nil || state.NewsContext != nil {
		return state
	}
	if news, ok := source.NewsContext(state.Symbol); ok {
		state.NewsContext = &news
	}
	return state
}
