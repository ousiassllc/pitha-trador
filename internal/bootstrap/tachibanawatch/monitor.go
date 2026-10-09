package tachibanawatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingwatch"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/event"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// noListWarnEvery bounds the "no watch list yet" warning.
const noListWarnEvery = time.Hour

// MonitorConfig configures a Monitor.
type MonitorConfig struct {
	// Source is the saved watch list in use now (Source).
	Source broker.CandidateSource
	Held   HeldSource
	// Universe, Registrar and Ingester are the ranking watch's collaborators
	// (rankingwatch): the EVENT subscription is the Registrar and the
	// market-data jobs go to the Ingester.
	Universe  rankingwatch.Universe
	Registrar rankingwatch.Registrar
	Ingester  rankingwatch.Ingester
	// List is shared with candidates.Refresher.Watch: the Fast Screener's
	// universe.
	List *rankingwatch.Watchlist
	// Max is the subscription cap (broker.Capabilities.MaxStreamSymbols);
	// zero means event.MaxSymbols.
	Max int
	// Clock defaults to the wall clock (tests inject a fake).
	Clock tachibana.Clock
}

// Monitor is the 立花 daytime watch (issue #731, child of #726): once a minute
// it turns the watch list decided the night before (Source) into the EVENT
// subscription, the market-data jobs and the Fast Screener's universe - the
// kabu ranking watch's role (rankingwatch.Watcher) without a ranking. The
// list is never swapped during the day: the only change is a symbol that
// becomes held (open position or pending order) after the list was decided,
// which takes a slot of the list's tail (held symbols come first, like the
// kabu Selector's fixed slots). One cycle's additions are one SetWatch call -
// at most one EVENT reconnect, which the Feed counts against the daily
// connection budget (and suspends with a warning once it is used up).
//
// The subscription is only touched from 03:30 (the broker's daily close,
// after which the day's list is the one in use) until the 立会日's close: the
// list that is decided in the evening for the next day is not registered
// then, so the morning's login connects once with the final list instead of
// reconnecting the evening before as well. The Monitor never fetches prices:
// 時価 comes from the EVENT stream, with the Feed's rate-limited REST refill
// for stale watched symbols only (FR-SCHED-10); no REST call walks the
// universe.
type Monitor struct {
	cfg   MonitorConfig
	clock tachibana.Clock
	max   int

	lastHeld     []string
	applied      []string // sorted symbols the Registrar last accepted
	hasApplied   bool
	noListWarned time.Time
}

// NewMonitor returns a Monitor; nothing runs until Run (or Cycle) is called.
func NewMonitor(cfg MonitorConfig) *Monitor {
	max := cfg.Max
	if max <= 0 {
		max = event.MaxSymbols
	}
	return &Monitor{cfg: cfg, clock: tachibana.OrReal(cfg.Clock), max: max}
}

// Run calls Cycle immediately and then every minute until ctx is done. A
// panic or error in one cycle is logged and the next cycle still runs.
func (m *Monitor) Run(ctx context.Context) {
	wait := time.Duration(0) // the first cycle runs immediately
	safego.Loop(ctx, "tachibana watch monitor", func() time.Duration {
		d := wait
		wait = pollInterval
		return d
	}, m.Cycle)
}

// inWatchWindow reports whether the EVENT subscription and the ingestion may
// be updated at t: a 立会日 between the broker's 03:30 close and the market's
// close.
func inWatchWindow(t time.Time) bool {
	t = t.In(tachibana.JST)
	if !marketcalendar.TSE.IsTradingDay(t) || tachibana.TimeOfDay(t) < tachibana.CloseAt {
		return false
	}
	closeAt, ok := marketcalendar.TSE.CloseAt(t)
	return ok && t.Before(closeAt)
}

// Cycle runs one watch cycle. Outside the watch window nothing changes: the
// Fast Screener's list stays the last in-window one (like the ranking watch's
// out-of-session list) and nothing is registered or enqueued.
func (m *Monitor) Cycle(ctx context.Context) error {
	now := m.clock.Now()
	if !inWatchWindow(now) {
		return nil
	}
	stocks, err := m.cfg.Universe.ListActiveByKind(ctx, domain.InstrumentKindStock)
	if err != nil {
		return fmt.Errorf("tachibanawatch: list the universe: %w", err)
	}
	byCode := make(map[string]domain.Instrument, len(stocks))
	for _, inst := range stocks {
		byCode[inst.Symbol] = inst
	}
	listed, err := m.listed(ctx, now)
	if err != nil {
		return err
	}
	watch := mergeWatch(listed, m.heldSymbols(ctx), byCode, m.max)
	m.cfg.List.Set(watch)
	m.register(ctx, watch)
	m.enqueue(ctx, now, watch, byCode)
	return nil
}

// listed is the saved list in use now. A missing list is logged (hourly) and
// leaves the held symbols only; a failed read is returned and the cycle
// changes nothing.
func (m *Monitor) listed(ctx context.Context, now time.Time) ([]string, error) {
	symbols, err := m.cfg.Source.Candidates(ctx)
	switch {
	case errors.Is(err, ErrNoList):
		if now.Sub(m.noListWarned) >= noListWarnEvery {
			m.noListWarned = now
			slog.Warn("tachibanawatch: no watch list has been decided yet; only the held symbols are watched")
		}
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("tachibanawatch: read the watch list: %w", err)
	}
	return symbols, nil
}

// heldSymbols reads the held symbols; a failed read keeps the previous ones,
// so a held slot is never dropped by a transient DB error.
func (m *Monitor) heldSymbols(ctx context.Context) []string {
	held, err := m.cfg.Held.HeldSymbols(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("tachibanawatch: read the held symbols; keeping the previous ones", "error", err)
		}
		return m.lastHeld
	}
	m.lastHeld = held
	return held
}

// mergeWatch is the symbols to watch: the held ones first (they keep their
// slots), then the list in its order, at most max, without duplicates or
// symbols the universe cannot ingest.
func mergeWatch(listed, held []string, byCode map[string]domain.Instrument, max int) []string {
	out := make([]string, 0, min(max, len(listed)+len(held)))
	for _, group := range [][]string{held, listed} {
		for _, sym := range group {
			if len(out) == max {
				return out
			}
			if _, ok := byCode[sym]; ok && !slices.Contains(out, sym) {
				out = append(out, sym)
			}
		}
	}
	return out
}

// register hands watch to the Registrar when it differs from what it last
// accepted; a failure is retried on the next cycle.
func (m *Monitor) register(ctx context.Context, watch []string) {
	want := slices.Sorted(slices.Values(watch))
	if m.hasApplied && slices.Equal(want, m.applied) {
		return
	}
	var err error
	if panicked := safego.Run("tachibana watch register", func() { err = m.cfg.Registrar.SetWatch(ctx, watch) }); panicked {
		err = errors.New("tachibanawatch: the EVENT registration panicked")
	}
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("tachibanawatch: EVENT registration failed, retrying next cycle", "error", err)
		}
		return
	}
	slog.Info("tachibanawatch: EVENT watch list set", "symbols", len(want))
	m.applied, m.hasApplied = want, true
}

// enqueue enqueues market-data jobs for the watched symbols, plus the index
// rows the market context needs; the Scheduler skips it outside the session.
func (m *Monitor) enqueue(ctx context.Context, now time.Time, watch []string, byCode map[string]domain.Instrument) {
	instruments := rankingwatch.IngestSet(ctx, m.cfg.Universe, watch, byCode, func(err error) {
		if ctx.Err() == nil {
			slog.Warn("tachibanawatch: list the market index rows; skipping them", "error", err)
		}
	})
	if _, err := m.cfg.Ingester.EnqueueMarketData(ctx, instruments, now); err != nil && ctx.Err() == nil {
		slog.Warn("tachibanawatch: enqueue the market-data jobs", "error", err)
	}
}
