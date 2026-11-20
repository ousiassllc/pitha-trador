// Package researchfx records the research-only FX 1-minute candles (issue
// #1084): for each configured currency pair and each completed GMO trading day
// of a short lookback window, it fetches the BID and the ASK 1-minute candles
// through the read-only adapter (internal/service/fx/gmo), merges them by minute
// and stores them with the day's marker (internal/repository/researchfx).
//
// The record is never a trading input. This package must not import a broker,
// kabu or tachibana client, nor Feature Engine, Policy, Risk, order or Jev code
// (repository/researchfx/isolation_test.go pins that): it makes no kabu
// registration request and consumes none of the kabu registration slots. The
// adapter's limiter (at most one HTTP call per second, never lowered here) is
// the only outbound pacing; between two Runs the Recorder adds its own pause so
// the limit also holds across the per-pair clients.
package researchfx

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	rfx "github.com/ousiassllc/pitha-trador/internal/domain/researchfx"
	"github.com/ousiassllc/pitha-trador/internal/service/fx/gmo"
)

// DefaultLookbackDays is how many completed trading days before today the
// Recorder looks at: a day in the window with no marker is fetched, oldest first.
// A day older than the window that was never fetched stays a gap.
const DefaultLookbackDays = 7

// ChunkTimeout bounds the fetch of one Run (both klines requests, retries and
// backoff included): the adapter alone can take minutes on a stuck day (five
// attempts per request, backoff up to 30s, 10s per HTTP call), and the
// maintenance runner holds its lock for the whole Run. A Run that exceeds it
// returns an error, writes no marker and is retried by the next run.
const ChunkTimeout = 90 * time.Second

// Fetcher is the part of the GMO adapter the Recorder uses (*gmo.Client).
type Fetcher interface {
	Klines(ctx context.Context, req gmo.KlinesRequest) ([]gmo.Candle, error)
}

// FetcherSource returns the Fetcher of a pair. Calls for one pair must share a
// Fetcher so the adapter's call-rate limit holds between them.
type FetcherSource func(symbol string) (Fetcher, error)

// Store is the persistence the Recorder needs (internal/repository/researchfx).
// Days are trading days ("2006-01-02").
type Store interface {
	// MarkedDays are the days of symbol in [from, to] that have a marker.
	MarkedDays(ctx context.Context, symbol, from, to string) (map[string]bool, error)
	// RecordDay writes the candles of a day with its marker in one transaction.
	RecordDay(ctx context.Context, symbol, day string, candles []rfx.Candle, at time.Time) (rfx.Day, error)
}

// SymbolSource returns the pairs to record (researchfxsettings.Load).
type SymbolSource func(ctx context.Context) ([]string, error)

// Result is what one Run did.
type Result struct {
	// Symbol and Day are the (pair, trading day) the Run fetched; empty when
	// nothing was pending.
	Symbol, Day string
	// Status is the marker written (rfx.StatusRecorded or rfx.StatusEmpty).
	Status string
	// Candles is the number of merged candles of the day.
	Candles int
	// More is true when other (pair, day) units are still pending: call Run again.
	More bool
}

// Recorder records one (pair, trading day) per Run. It is resumable and
// idempotent: a unit gets its marker only after both sides are fetched and
// written, so a failed fetch leaves no marker and the unit is retried by the next
// Run; candles already stored are kept (INSERT OR IGNORE).
type Recorder struct {
	store    Store
	fetchers FetcherSource
	symbols  SymbolSource
	now      func() time.Time
	lookback int
	// chunkTimeout is the deadline of one Run's fetch (ChunkTimeout).
	chunkTimeout time.Duration
	// pace is the least time between the end of one Run's requests and the start
	// of the next Run's. wait sleeps it (tests replace it); last is when the
	// previous Run's requests ended.
	pace time.Duration
	wait func(ctx context.Context, d time.Duration) error
	last time.Time
	// failed are the units whose last fetch failed in this process: they are tried
	// after the others, so one day GMO keeps refusing does not starve the days behind it.
	failed map[unit]bool
}

// NewRecorder returns a Recorder over store, with the pairs read at the start of
// every Run.
func NewRecorder(store Store, fetchers FetcherSource, symbols SymbolSource) *Recorder {
	return &Recorder{store: store, fetchers: fetchers, symbols: symbols, now: time.Now, lookback: DefaultLookbackDays, chunkTimeout: ChunkTimeout, pace: gmo.MinCallInterval, wait: sleep, failed: map[unit]bool{}}
}

// WithClock overrides time.Now (tests).
func (r *Recorder) WithClock(now func() time.Time) *Recorder {
	r.now = now
	return r
}

// WithLookbackDays overrides DefaultLookbackDays (tests).
func (r *Recorder) WithLookbackDays(n int) *Recorder {
	r.lookback = n
	return r
}

// WithChunkTimeout overrides ChunkTimeout (tests); a non-positive d is ignored.
func (r *Recorder) WithChunkTimeout(d time.Duration) *Recorder {
	if d > 0 {
		r.chunkTimeout = d
	}
	return r
}

// WithPace overrides the pause between Runs (tests; the production value is the
// adapter's one-second minimum and is never shortened).
func (r *Recorder) WithPace(d time.Duration, wait func(ctx context.Context, d time.Duration) error) *Recorder {
	r.pace, r.wait = d, wait
	return r
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type unit struct{ symbol, day string }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Window returns the completed trading days the Recorder considers at now,
// oldest first: the lookback days before the trading day now is in, never
// earlier than the first day GMO serves 1-minute candles for. A trading day is
// complete once its 06:00 JST rollover to the next day has passed.
func Window(now time.Time, lookbackDays int) []string {
	today, _ := time.ParseInLocation(time.DateOnly, rfx.TradingDay(now), rfx.JST)
	first := time.Date(gmo.MinuteCandlesSince.Year(), gmo.MinuteCandlesSince.Month(), gmo.MinuteCandlesSince.Day(), 0, 0, 0, 0, rfx.JST)
	var days []string
	for i := lookbackDays; i >= 1; i-- {
		d := today.AddDate(0, 0, -i)
		if d.Before(first) {
			continue
		}
		days = append(days, d.Format(time.DateOnly))
	}
	return days
}

// pending lists the unmarked (pair, day) units, oldest day first, pairs in the
// configured order.
func (r *Recorder) pending(ctx context.Context, symbols, days []string) ([]unit, error) {
	if len(days) == 0 {
		return nil, nil
	}
	marked := make(map[string]map[string]bool, len(symbols))
	for _, s := range symbols {
		m, err := r.store.MarkedDays(ctx, s, days[0], days[len(days)-1])
		if err != nil {
			return nil, err
		}
		marked[s] = m
	}
	var out []unit
	for _, d := range days {
		for _, s := range symbols {
			if !marked[s][d] {
				out = append(out, unit{s, d})
			}
		}
	}
	return out, nil
}

// Run records the oldest unmarked (pair, day) of the window with two klines
// requests (BID, ASK) and returns Result.More while units are left. With nothing
// pending it returns an empty Result and makes no request. The first failure is
// returned and leaves the unit unmarked.
func (r *Recorder) Run(ctx context.Context) (Result, error) {
	symbols, err := r.symbols(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("researchfx: read pairs: %w", err)
	}
	symbols = slices.Compact(slices.Clone(symbols))
	units, err := r.pending(ctx, symbols, Window(r.now(), r.lookback))
	if err != nil {
		return Result{}, err
	}
	if len(units) == 0 {
		return Result{}, nil
	}
	slices.SortStableFunc(units, func(a, b unit) int { return boolInt(r.failed[a]) - boolInt(r.failed[b]) })
	u := units[0]
	res := Result{Symbol: u.symbol, Day: u.day, More: len(units) > 1}
	candles, err := r.fetchDay(ctx, u)
	if err != nil {
		r.failed[u] = true
		return res, err
	}
	delete(r.failed, u)
	day, err := r.store.RecordDay(ctx, u.symbol, u.day, candles, r.now())
	if err != nil {
		return res, err
	}
	res.Status, res.Candles = day.Status, len(candles)
	slog.Info("researchfx: day recorded", "symbol", u.symbol, "day", u.day, "status", day.Status, "candles", len(candles))
	return res, nil
}

// fetchDay fetches the BID and the ASK 1-minute candles of u and merges them.
// Both requests share the chunk deadline (ChunkTimeout), so a stuck day fails
// instead of holding the maintenance lock. Whatever the outcome, the end of the
// requests is remembered for the pace.
func (r *Recorder) fetchDay(ctx context.Context, u unit) ([]rfx.Candle, error) {
	if !r.last.IsZero() && r.pace > 0 {
		if d := r.pace - time.Since(r.last); d > 0 {
			if err := r.wait(ctx, d); err != nil {
				return nil, err
			}
		}
	}
	defer func() { r.last = time.Now() }()
	f, err := r.fetchers(u.symbol)
	if err != nil {
		return nil, fmt.Errorf("researchfx: fetcher for %s: %w", u.symbol, err)
	}
	start, err := rfx.DayStart(u.day)
	if err != nil {
		return nil, err
	}
	at := start.Add(6 * time.Hour) // any instant of the trading day picks it (gmo.KlinesRequest.At)
	ctx, cancel := context.WithTimeout(ctx, r.chunkTimeout)
	defer cancel()
	var sides [2][]gmo.Candle
	for i, pt := range []gmo.PriceType{gmo.PriceBid, gmo.PriceAsk} {
		sides[i], err = f.Klines(ctx, gmo.KlinesRequest{Symbol: u.symbol, PriceType: pt, Interval: gmo.Interval1Min, At: at})
		if err != nil {
			return nil, fmt.Errorf("researchfx: fetch %s %s candles of %s: %w", pt, u.symbol, u.day, err)
		}
	}
	return Merge(u.symbol, u.day, sides[0], sides[1]), nil
}
