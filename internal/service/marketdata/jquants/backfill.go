package jquants

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jquantssettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jquants"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// DefaultMaxCallsPerRun bounds the reads (each one or a few HTTP requests) one Run
// makes before it reports More: the maintenance runner holds its lock for the
// length of a call, so a long backfill is a series of short runs.
const DefaultMaxCallsPerRun = 10

// maxFailuresPerRun stops a run after this many failed reads (other than the cap,
// a refused key or a 429, which stop it at once).
const maxFailuresPerRun = 3

// Reader is the J-Quants read client (*Client).
type Reader interface {
	Symbols(ctx context.Context, day string) ([]jquants.Symbol, error)
	DailyBars(ctx context.Context, day string) ([]jquants.DailyBar, error)
	MinuteBars(ctx context.Context, day, code string) ([]jquants.MinuteBar, error)
}

// Store is the persistence Backfiller needs (repository/jquants).
type Store interface {
	UpsertSymbols(ctx context.Context, rows []jquants.Symbol, fetchedAt time.Time) (int, error)
	UpsertDailyBars(ctx context.Context, rows []jquants.DailyBar, fetchedAt time.Time) (int, error)
	UpsertMinuteBars(ctx context.Context, rows []jquants.MinuteBar, fetchedAt time.Time) (int, error)
	MarkCovered(ctx context.Context, dataset, day, code string, rows int, at time.Time) error
	Covered(ctx context.Context, dataset, day string) (map[string]bool, error)
	RecordSuccess(ctx context.Context, day, dataset string, rows int, at time.Time) error
}

// SymbolSource lists the 4-character symbols whose minute bars a day needs
// (the issues the application recorded that day).
type SymbolSource interface {
	SnapshotSymbols(ctx context.Context, day string) ([]string, error)
}

// RelabelFunc re-labels a day's research horizon labels with the stored minute
// bars (jquantsrelabel.Relabeler). ready is false when the day's labels are not
// recorded yet (nothing was done, try again later).
type RelabelFunc func(ctx context.Context, day string) (newRows int, ready bool, err error)

// SettingsFunc loads the effective settings (jquantssettings.Load).
type SettingsFunc func(ctx context.Context) (jquantssettings.Settings, error)

// Request is a backfill: the JST days From..To (inclusive, YYYY-MM-DD) and, for
// the minute bars, the symbols. A zero Request is the scheduled run: the last
// BackfillDays days up to yesterday and the symbols recorded on each day.
type Request struct {
	From, To string
	Symbols  []string
}

// Result is what one Run did.
type Result struct {
	// Skipped is why nothing was done ("" = it ran): "disabled", "no_key", "no_plan".
	Skipped string
	// Calls are the reads made (each one or more HTTP requests).
	Calls int
	// DailyDays / MinuteDays are the days whose daily / minute bars were completed
	// in this run; Relabeled those whose research labels were re-labeled.
	DailyDays, MinuteDays, Relabeled []string
	// DailyRows, MinuteRows and RelabelRows are the rows written.
	DailyRows, MinuteRows, RelabelRows int
	// CapReached: the day's request cap stopped the run. More: work is left, call Run again.
	CapReached, More bool
}

// Backfiller fetches the days not yet fetched, within the cap, and re-labels the
// days whose minute bars it completed. It is resumable and idempotent: a unit is
// marked only after its rows are written, rows are upserted on their natural
// keys, and nothing already marked is fetched again.
type Backfiller struct {
	reader   Reader
	store    Store
	symbols  SymbolSource
	relabel  RelabelFunc
	settings SettingsFunc
	hasKey   bool
	now      func() time.Time
	maxCalls int
}

// NewBackfiller returns a Backfiller. hasKey is Client.HasKey(); relabel may be nil.
func NewBackfiller(reader Reader, store Store, symbols SymbolSource, relabel RelabelFunc, settings SettingsFunc, hasKey bool) *Backfiller {
	return &Backfiller{reader: reader, store: store, symbols: symbols, relabel: relabel, settings: settings, hasKey: hasKey, now: time.Now, maxCalls: DefaultMaxCallsPerRun}
}

// WithClock overrides time.Now (tests).
func (b *Backfiller) WithClock(now func() time.Time) *Backfiller {
	b.now = now
	return b
}

// WithMaxCalls overrides DefaultMaxCallsPerRun (tests).
func (b *Backfiller) WithMaxCalls(n int) *Backfiller {
	b.maxCalls = n
	return b
}

// run is the state of one Run.
type run struct {
	*Backfiller
	s        jquantssettings.Settings
	req      Request
	today    string
	res      Result
	failures int
	// marked counts the coverage marks written in this run: the run's progress.
	marked int
}

// Run does one bounded chunk. With the switch off, no key, or no endpoint
// subscribed it returns at once having made no request. The first error that
// makes going on pointless (a refused key, a 429, a context error, too many
// failures) is returned; the request cap is not an error (Result.CapReached).
func (b *Backfiller) Run(ctx context.Context, req Request) (Result, error) {
	s, err := b.settings(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("jquants: %w", err)
	}
	r := &run{Backfiller: b, s: s, req: req, today: Day(b.now())}
	switch {
	case !s.Enabled:
		r.res.Skipped = "disabled"
	case !b.hasKey:
		r.res.Skipped = "no_key"
	case !s.PlanDaily && !s.PlanMinute:
		r.res.Skipped = "no_plan"
	}
	if r.res.Skipped != "" {
		return r.res, nil
	}
	from, to, err := r.window(req)
	if err != nil {
		return r.res, err
	}
	if err := r.finish(r.fetchSymbols(ctx)); err != nil {
		return r.res, err
	}
	for day := from; !day.After(to) && !r.res.CapReached && !r.res.More && r.res.Calls < r.maxCalls; day = day.AddDate(0, 0, 1) {
		if !marketcalendar.TSE.IsTradingDay(day) {
			continue
		}
		if err := r.finish(r.fetchDay(ctx, day.Format(time.DateOnly))); err != nil {
			return r.res, err
		}
	}
	if r.res.Calls >= r.maxCalls && !r.res.CapReached {
		// The bound ended the run: call again if this run made progress (a spurious
		// extra run costs no request, as everything it would fetch is marked).
		r.res.More = r.res.More || r.marked > 0
	}
	return r.res, nil
}

// window is the JST days to look at: never today or later (today's bars are not
// complete, and the minute bars are delivered the next day).
func (r *run) window(req Request) (from, to time.Time, err error) {
	yesterday, err := time.ParseInLocation(time.DateOnly, r.today, marketcalendar.JST)
	if err != nil {
		return from, to, err
	}
	yesterday = yesterday.AddDate(0, 0, -1)
	from, to = yesterday.AddDate(0, 0, -(r.s.BackfillDays-1)), yesterday
	if req.From != "" || req.To != "" {
		if from, err = time.ParseInLocation(time.DateOnly, req.From, marketcalendar.JST); err != nil {
			return from, to, fmt.Errorf("jquants: backfill from %q: %w", req.From, err)
		}
		if to, err = time.ParseInLocation(time.DateOnly, req.To, marketcalendar.JST); err != nil {
			return from, to, fmt.Errorf("jquants: backfill to %q: %w", req.To, err)
		}
		if to.After(yesterday) {
			to = yesterday
		}
	}
	return from, to, nil
}

// finish turns the error of a step into the run's state: the cap and the call
// bound end the run quietly, a refused key / 429 / cancelled context end it with
// the error, and other failures end it after maxFailuresPerRun of them.
func (r *run) finish(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrCapReached):
		r.res.CapReached, r.res.More = true, true
		return nil
	case errors.Is(err, errCallBound):
		// More only with progress: a run that spent its calls on days the API has not
		// delivered yet must not make the runner call again at once.
		r.res.More = r.marked > 0
		return nil
	case errors.Is(err, ErrAuth), errors.Is(err, ErrRateLimited), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	if r.failures++; r.failures >= maxFailuresPerRun {
		return err
	}
	return nil
}

var errCallBound = errors.New("jquants: call bound of the run reached")

// call counts one read against the run's bound.
func (r *run) call() error {
	if r.res.Calls >= r.maxCalls {
		return errCallBound
	}
	r.res.Calls++
	return nil
}

// markCovered writes a coverage mark and counts it as progress.
func (r *run) markCovered(ctx context.Context, dataset, day, code string, rows int, at time.Time) error {
	if err := r.store.MarkCovered(ctx, dataset, day, code, rows, at); err != nil {
		return err
	}
	r.marked++
	return nil
}
