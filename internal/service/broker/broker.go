package broker

import "context"

// Session is the broker login/session lifecycle.
type Session interface {
	// Start establishes the session and keeps it alive in the background
	// until ctx is done (kabu: issue the API token, then reissue it
	// periodically). A failed first attempt is returned but the background
	// retry still runs, so the process can come up before the broker app.
	Start(ctx context.Context) error
	// Status is the state of the most recent session attempt (never blocks
	// on the network).
	Status() SessionStatus
}

// QuoteSource fetches a Quote over REST.
type QuoteSource interface {
	Quote(ctx context.Context, symbol string) (Quote, error)
}

// StreamFeed is the adapter's streaming subscription plus REST completion.
type StreamFeed interface {
	// SetWatch makes symbols the subscribed set (at most
	// Capabilities.MaxStreamSymbols). The latest set is remembered even when
	// the call fails, so Run subscribes it after the next reconnect; the
	// caller retries on its next cycle.
	SetWatch(ctx context.Context, symbols []string) error
	// UseWatchlist switches the subscription from the adapter's default (the
	// whole universe) to the list SetWatch maintains. Call it before Run.
	UseWatchlist()
	// Latest returns symbol's freshest Quote: a streamed one when recent,
	// otherwise a REST poll. A Quote without a usable price is never
	// returned (ErrPriceUnavailable).
	Latest(ctx context.Context, symbol string) (Quote, error)
	// Run keeps the subscription up, reconnecting with backoff, until ctx
	// is done.
	Run(ctx context.Context)
}

// SymbolInfoSource fetches a symbol's 銘柄情報.
type SymbolInfoSource interface {
	SymbolInfo(ctx context.Context, symbol string) (SymbolInfo, error)
}

// CandidateSource ranks the symbols worth watching, best first (kabu: the
// GET /ranking types interleaved rank by rank). It returns symbol codes
// only and may repeat a symbol.
type CandidateSource interface {
	Candidates(ctx context.Context) ([]string, error)
}

// Capabilities describe what the adapter can do.
type Capabilities struct {
	// Name identifies the adapter ("kabu", "tachibana").
	Name string
	// MaxStreamSymbols is the most symbols StreamFeed.SetWatch may hold.
	MaxStreamSymbols int
	// Ranking is true when CandidateSource.Candidates is available.
	Ranking bool
	// RequestsPerSecond is the adapter's REST request budget.
	RequestsPerSecond int
}

// Broker is one broker adapter as the composition root hands it to the
// consumers. Candidates returns an error unless Capabilities().Ranking.
type Broker interface {
	Capabilities() Capabilities
	Session
	QuoteSource
	StreamFeed
	SymbolInfoSource
	CandidateSource
	Health
}
