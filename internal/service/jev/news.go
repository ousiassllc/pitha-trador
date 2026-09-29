package jev

import "github.com/ousiassllc/pitha-trador/internal/domain"

// NewsSource supplies an instrument's recent Luna-classified news
// (FR-LUNA-3). *internal/service/newsfeed.Service implements it. ok=false
// means "no news context" - the state is sent without one.
type NewsSource interface {
	NewsContext(symbol string) (domain.NewsContext, bool)
}

// Option configures a Scout or Trader beyond their required arguments.
type Option func(*options)

type options struct {
	news NewsSource
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
	var o options
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
