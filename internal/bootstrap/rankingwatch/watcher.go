package rankingwatch

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/safego"
)

const (
	// DefaultInterval is how often the ranking is fetched (issue #651:
	// 毎分).
	DefaultInterval = time.Minute
	// DefaultExchange is the ExchangeDivision fetched: 東証全体, matching the
	// exchange code symbols are registered and polled with.
	DefaultExchange = "T"
)

// DefaultTypes are the GET /ranking 種別 fetched: 1 値上がり率, 2 値下がり率,
// 3 売買高上位, 4 売買代金上位, 5 TICK回数, 6 売買高急増, 7 売買代金急増.
var DefaultTypes = []int{1, 2, 3, 4, 5, 6, 7}

// errPanicked marks a ranking request that panicked (already logged with its
// stack by safego).
var errPanicked = errors.New("rankingwatch: ranking request panicked")

// Source fetches one ranking's symbol codes in rank order
// (*marketdata.Client). It must return nothing but codes.
type Source interface {
	RankingSymbols(ctx context.Context, rankType int, exchange string) ([]string, error)
}

// HeldSource lists the symbols with an open position or pending order (Held).
type HeldSource interface {
	HeldSymbols(ctx context.Context) ([]string, error)
}

// Universe lists the instruments that can be ingested (*market.InstrumentRepository);
// a ranked symbol outside it has no instrument to store bars for and is skipped.
type Universe interface {
	ListActiveByKind(ctx context.Context, kind string) ([]domain.Instrument, error)
}

// Registrar makes the watch list the PUSH registration (*pushfeed.Feed).
type Registrar interface {
	SetWatch(ctx context.Context, symbols []string) error
}

// Ingester enqueues the market-data jobs of the watch list (*scheduler.Scheduler).
type Ingester interface {
	EnqueueMarketData(ctx context.Context, instruments []domain.Instrument, now time.Time) (int, error)
}

// Watcher runs the ranking-driven watch cycle: fetch the rankings, select the
// watch list, publish it, register it for PUSH and enqueue its market-data
// jobs (plus those of the market-context index rows). A cycle never fails: an empty or failed ranking leaves only the held
// symbols (see Selector.Update) and the next cycle tries again.
type Watcher struct {
	Source    Source
	Held      HeldSource
	Universe  Universe
	Registrar Registrar
	Ingester  Ingester
	List      *Watchlist
	// Types and Exchange select the rankings; they default to DefaultTypes
	// and DefaultExchange.
	Types    []int
	Exchange string
	// Open reports whether t is inside a trading session; outside it no
	// ranking is requested and PUSH registration and market-data ingestion
	// cover the held symbols only, while the candidate list (List) keeps
	// the last watch list. nil means always open.
	Open func(t time.Time) bool
	// Now defaults to time.Now.
	Now func() time.Time

	selector Selector
	lastHeld []string
	applied  []string // sorted symbols the Registrar last accepted
	health   health
}

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// Cycle runs one watch cycle. A panic anywhere in it is logged and swallowed.
func (w *Watcher) Cycle(ctx context.Context) {
	safego.Run("ranking watch", func() { w.cycle(ctx) })
}

func (w *Watcher) cycle(ctx context.Context) {
	now := w.now()
	start := time.Now()
	stocks, err := w.Universe.ListActiveByKind(ctx, domain.InstrumentKindStock)
	if err != nil {
		if ctx.Err() == nil {
			w.health.logUniverseError(now)
		}
		return
	}
	byCode := make(map[string]domain.Instrument, len(stocks))
	for _, inst := range stocks {
		byCode[inst.Symbol] = inst
	}

	held := w.heldSymbols(ctx)
	var (
		res            rankingResult
		watch, screen  []string
		added, removed int
	)
	if w.Open == nil || w.Open(now) {
		res = w.fetchRanking(ctx, byCode)
		if ctx.Err() != nil {
			return
		}
		watch, added, removed = w.selector.Update(now, held, res.symbols)
		screen = watch
	} else {
		// No ranking is requested outside the session: PUSH registration and
		// market-data ingestion shrink to the held symbols, but the candidate
		// list keeps the last watch list (screened from the stored data).
		res.offSession = true
		watch, screen = w.selector.Retain(held)
	}
	w.List.Set(screen)
	w.register(ctx, now, watch)
	w.enqueue(ctx, now, watch, byCode)
	w.health.report(now, res, len(held), len(watch), added, removed, time.Since(start))
}

// heldSymbols reads the held symbols; a failed read keeps the previous ones,
// so held slots are never dropped by a transient DB error.
func (w *Watcher) heldSymbols(ctx context.Context) []string {
	held, err := w.Held.HeldSymbols(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.health.logHeldError(w.now())
		}
		return w.lastHeld
	}
	w.lastHeld = held
	return held
}

// register hands watch to the Registrar when it differs from what it last
// accepted; a failure is retried on the next cycle.
func (w *Watcher) register(ctx context.Context, now time.Time, watch []string) {
	want := slices.Sorted(slices.Values(watch))
	if slices.Equal(want, w.applied) {
		return
	}
	var err error
	if panicked := safego.Run("ranking watch register", func() { err = w.Registrar.SetWatch(ctx, watch) }); panicked {
		err = errPanicked
	}
	if err != nil {
		if ctx.Err() == nil {
			w.health.logRegisterError(now, err)
		}
		return
	}
	w.applied = want
}

// enqueue enqueues market-data jobs for the watched symbols that are in the
// universe, plus the index rows the market context needs (marketIndexes).
func (w *Watcher) enqueue(ctx context.Context, now time.Time, watch []string, byCode map[string]domain.Instrument) {
	instruments := make([]domain.Instrument, 0, len(watch))
	sectors := make(map[string]struct{})
	for _, sym := range watch {
		if inst, ok := byCode[sym]; ok {
			instruments = append(instruments, inst)
			if inst.Sector != nil {
				sectors[*inst.Sector] = struct{}{}
			}
		}
	}
	instruments = append(instruments, w.marketIndexes(ctx, now, sectors)...)
	if _, err := w.Ingester.EnqueueMarketData(ctx, instruments, now); err != nil && ctx.Err() == nil {
		w.health.logEnqueueError(now, err)
	}
}

// marketIndexes lists the active market_index rows and the sector_index rows
// of the given sectors: the instruments the market context (market_return_*,
// sector_return_5m; FR-FE-4) is derived from, which the full scan ingests
// along with every stock but the watch list does not contain (issue #670;
// without them the Risk Engine's market_adverse_to_direction gate would find
// no market return). A failed listing is logged and skipped.
func (w *Watcher) marketIndexes(ctx context.Context, now time.Time, sectors map[string]struct{}) []domain.Instrument {
	var out []domain.Instrument
	for _, kind := range []string{domain.InstrumentKindMarketIndex, domain.InstrumentKindSectorIndex} {
		rows, err := w.Universe.ListActiveByKind(ctx, kind)
		if err != nil {
			if ctx.Err() == nil {
				w.health.logIndexError(now, err)
			}
			continue
		}
		for _, inst := range rows {
			if kind == domain.InstrumentKindSectorIndex {
				if inst.Sector == nil {
					continue
				}
				if _, ok := sectors[*inst.Sector]; !ok {
					continue
				}
			}
			out = append(out, inst)
		}
	}
	return out
}

// Run calls Cycle immediately and then every interval until ctx is done.
func (w *Watcher) Run(ctx context.Context, interval time.Duration) {
	wait := time.Duration(0) // the first cycle runs immediately
	safego.Loop(ctx, "ranking watch", func() time.Duration {
		d := wait
		wait = interval
		return d
	}, func(ctx context.Context) error {
		w.Cycle(ctx)
		return nil
	})
}
