package disclosureskip

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

const (
	// followupGrace is how long past t+FetchLag a decision waits for News
	// Ingest's own poll to cover it before the Followup steps in: a symbol that
	// is still a candidate or held is fetched every minute anyway, and one
	// request is only made for the symbol that dropped out of those targets.
	followupGrace = 2 * time.Minute
	// followupSymbols bounds the requests of one cycle. A cycle only has the
	// symbols of decisions still waiting, so the bound is a safety net.
	followupSymbols = 50
	// followupConcurrency is News Ingest's DefaultConcurrency: the same load on
	// the feed at most.
	followupConcurrency = newsfeed.DefaultConcurrency
	// followupWriteLag is how long the Followup trusts its own successful
	// request while the recording feed's background writer may not have stored
	// it yet. Past it the database is the only authority again, so a result the
	// writer lost (full queue, failed write) is fetched once more instead of
	// never.
	followupWriteLag = 2 * time.Minute
)

// Followup makes the one request a waiting decision still needs when the
// symbol has left News Ingest's targets (issue #1053): News Ingest polls only
// the current Fast Screener candidates and open positions, so a symbol that
// drops out right after a decision would never get the fetch started at or
// after t+FetchLag that covers it, and the decision would end as unknown /
// no_fetch. The Followup fetches such a symbol through the recording feed, in
// the trading session only, until a successful fetch covers it or the give-up
// time passes.
//
// It is not News Ingest. It calls the Feed directly and drops the response:
// nothing is classified by Luna, nothing enters News Ingest's cache (so no
// news_context and no news flag come out of it), and the symbol is not added
// to the targets. It never fetches for a decision that is not waiting, never
// polls the universe (#531), and shares the Feed's failure backoff, so a
// failing feed is queried no more often than News Ingest queries it.
type Followup struct {
	store  *Store
	feed   Feed
	cal    Calendar
	now    func() time.Time
	giveUp time.Duration

	// fetched is the start time of the Followup's last successful request per
	// symbol, kept for followupWriteLag. The recording feed stores a fetch
	// asynchronously, so a cycle that runs before the write lands would find no
	// covering fetch in the database and ask やのしん for the same symbol again.
	mu      sync.Mutex
	fetched map[string]time.Time
}

// FollowupOption configures a Followup.
type FollowupOption func(*Followup)

// WithFollowupClock overrides time.Now (tests).
func WithFollowupClock(now func() time.Time) FollowupOption {
	return func(f *Followup) { f.now = now }
}

// WithFollowupGiveUp is WithGiveUp for the Followup: it must equal the
// Evaluator's give-up time.
func WithFollowupGiveUp(d time.Duration) FollowupOption {
	return func(f *Followup) {
		if d > 0 {
			f.giveUp = d
		}
	}
}

// NewFollowup returns a Followup over store that fetches through feed (the
// RecordingFeed, so each request is recorded like News Ingest's).
func NewFollowup(store *Store, feed Feed, cal Calendar, opts ...FollowupOption) *Followup {
	f := &Followup{store: store, feed: feed, cal: cal, now: time.Now, giveUp: DefaultGiveUp, fetched: make(map[string]time.Time)}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Cycle fetches, once each, the symbols of the decisions that still wait for a
// covering fetch. Outside a trading session it does nothing. A failed or
// backed-off request is skipped (the next cycle tries again); only failing to
// read the waiting decisions is returned.
func (f *Followup) Cycle(ctx context.Context) error {
	now := f.now()
	if !inSession(f.cal, now) {
		return nil
	}
	symbols, err := f.waitingSymbols(ctx, now)
	if err != nil || len(symbols) == 0 {
		return err
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, followupConcurrency)
	for _, symbol := range symbols {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			safego.Run("disclosure skip followup "+symbol, func() { f.fetch(ctx, symbol) })
		}()
	}
	wg.Wait()
	return ctx.Err()
}

// recentlyFetched reports whether the Followup itself fetched symbol
// successfully at or after from, no longer than followupWriteLag ago.
func (f *Followup) recentlyFetched(symbol string, from, now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	at, ok := f.fetched[symbol]
	if ok && now.Sub(at) >= followupWriteLag {
		delete(f.fetched, symbol)
		return false
	}
	return ok && !at.Before(from)
}

func (f *Followup) markFetched(symbol string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for s, t := range f.fetched { // entries past the write lag no longer count: keep the map small
		if at.Sub(t) >= followupWriteLag {
			delete(f.fetched, s)
		}
	}
	f.fetched[symbol] = at
}

// waitingSymbols lists, each once, the symbols of decisions that are past
// t+FetchLag+followupGrace, not yet past their give-up time and without a
// successful fetch started at or after t+FetchLag.
func (f *Followup) waitingSymbols(ctx context.Context, now time.Time) ([]string, error) {
	decisions, err := f.store.Pending(ctx, pendingSince(f.cal, now, f.giveUp), pendingBatch)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var symbols []string
	for _, d := range decisions {
		if len(symbols) == followupSymbols {
			break
		}
		if _, dup := seen[d.Symbol]; dup {
			continue
		}
		if now.Before(d.At.Add(FetchLag+followupGrace)) || !now.Before(SessionDeadline(f.cal, d.At, f.giveUp)) {
			continue
		}
		if f.recentlyFetched(d.Symbol, d.At.Add(FetchLag), now) {
			continue
		}
		covered, err := f.store.HasSuccessfulFetch(ctx, d.Symbol, d.At.Add(FetchLag))
		if err != nil {
			return nil, err
		}
		if covered {
			continue
		}
		seen[d.Symbol] = struct{}{}
		symbols = append(symbols, d.Symbol)
	}
	return symbols, nil
}

func (f *Followup) fetch(ctx context.Context, symbol string) {
	at := f.now()
	_, err := f.feed.Fetch(ctx, symbol)
	switch {
	case err == nil:
		f.markFetched(symbol, at)
	case errors.Is(err, newsfeed.ErrBackingOff):
		slog.Debug("disclosure skip: feed is backing off, followup fetch skipped", "symbol", symbol)
	case ctx.Err() == nil:
		// The failed fetch is recorded by the recording feed; it is no coverage.
		slog.Warn("disclosure skip: followup fetch failed", "symbol", symbol, "error", err)
	}
}

// Run calls Cycle every interval until ctx is done. A failed or panicking
// cycle is logged and the next one still runs.
func (f *Followup) Run(ctx context.Context, interval time.Duration) {
	wait := time.Duration(0)
	safego.Loop(ctx, "disclosure skip followup", func() time.Duration {
		d := wait
		wait = interval
		return d
	}, f.Cycle)
}
