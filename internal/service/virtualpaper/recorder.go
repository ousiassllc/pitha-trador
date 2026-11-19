package virtualpaper

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/virtualpaper"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// The recorder's bounds.
const (
	// BacklogDays is how many of the most recent complete JST days a profile is
	// ever back-filled for: a profile gets the days from max(its FirstDay,
	// today - BacklogDays) up to yesterday, so a long outage never queues an
	// unbounded replay.
	BacklogDays = 10
	// DefaultChunkBudget is the wall time one Run may spend (a context deadline
	// over the loading and the replays; the next Run, after the maintenance
	// runner freed its lock, continues).
	DefaultChunkBudget = 3 * time.Minute
	// maxErrorRunes is the longest failure reason stored with a failed day.
	maxErrorRunes = 200
)

// Source supplies the replay configs of a period: the stored market_snapshots and
// jev_decisions as backtest.RunConfig, under the baseline settings in force
// (bootstrap's backtestsource.Source in the Paper replay mode). It only reads.
type Source interface {
	RunConfigs(ctx context.Context, period backtest.Period) ([]backtest.RunConfig, error)
}

// Recorder replays the JST days that are complete for every active profile and
// records the virtual trades. It never touches anything but the virtual_paper_*
// tables.
type Recorder struct {
	db     *sql.DB
	src    Source
	now    func() time.Time
	budget time.Duration
}

// NewRecorder returns a Recorder over db with the DefaultChunkBudget.
func NewRecorder(db *sql.DB, src Source) *Recorder {
	return &Recorder{db: db, src: src, now: time.Now, budget: DefaultChunkBudget}
}

// WithClock replaces time.Now (tests).
func (r *Recorder) WithClock(now func() time.Time) *Recorder { r.now = now; return r }

// WithBudget replaces DefaultChunkBudget (tests).
func (r *Recorder) WithBudget(d time.Duration) *Recorder { r.budget = d; return r }

// Result is what one Run did.
type Result struct {
	// Active is the number of active profiles; with 0 a Run does nothing.
	Active int
	// Day is the JST day the chunk replayed ("" when nothing was pending).
	Day string
	// Profiles are the ids that were recorded for Day.
	Profiles []int64
	// Trades / Unfinished are the rows stored (Unfinished: closed by the data's end).
	Trades, Unfinished int
	// More says work is left (the caller runs again).
	More bool
}

// Run is one bounded chunk: the oldest pending day, over every active profile
// that still lacks it. The day's configs are loaded once and each profile's
// params are laid over a copy, so all profiles see the same decisions and bars.
// A profile's day is stored with its mark in one transaction (see
// repo.RecordDay), so a crash re-replays it without duplicates. A replay that
// fails is marked failed (not retried) and reported in the returned error after
// the rest of the chunk; a failure to read or write returns at once.
func (r *Recorder) Run(ctx context.Context) (Result, error) {
	var res Result
	profiles, err := repo.ListProfiles(ctx, r.db, repo.StatusActive)
	if err != nil {
		return res, err
	}
	res.Active = len(profiles)
	if res.Active == 0 {
		return res, nil
	}
	today := r.now().In(jst).Format(time.DateOnly)
	pending, err := r.pending(ctx, profiles, today)
	if err != nil {
		return res, err
	}
	day := oldestDay(pending)
	if day == "" {
		return res, nil
	}
	res.Day = day
	bctx, cancel := context.WithTimeout(ctx, r.budget)
	defer cancel()
	cfgs, bars, err := r.load(bctx, day)
	if err != nil {
		return res, err
	}
	var failures []error
	for _, p := range profiles {
		if len(pending[p.ID]) == 0 || pending[p.ID][0] != day {
			continue
		}
		if bctx.Err() != nil && len(res.Profiles) > 0 {
			break // out of budget: the rest is for the next chunk
		}
		d, trades, err := r.replay(bctx, p, day, cfgs, bars)
		switch {
		case err != nil && bctx.Err() != nil:
			if len(res.Profiles) == 0 {
				return res, fmt.Errorf("virtualpaper: replay of %s did not finish within the chunk budget: %w", day, err)
			}
			res.More = true
			return res, errors.Join(failures...)
		case err != nil:
			d = repo.Day{ProfileID: p.ID, Day: day, Status: repo.DayFailed, Error: reason(err)}
			trades = nil
			failures = append(failures, fmt.Errorf("virtualpaper: profile %d day %s: %w", p.ID, day, err))
		}
		if err := repo.RecordDay(ctx, r.db, d, trades, r.now()); err != nil {
			return res, err
		}
		pending[p.ID] = pending[p.ID][1:]
		res.Profiles = append(res.Profiles, p.ID)
		res.Trades += d.Trades
		res.Unfinished += d.Unfinished
	}
	res.More = oldestDay(pending) != ""
	return res, errors.Join(failures...)
}

// load returns the day's configs and the number of bars (warm-up excluded).
func (r *Recorder) load(ctx context.Context, day string) ([]backtest.RunConfig, int, error) {
	period, err := dayPeriod(day)
	if err != nil {
		return nil, 0, err
	}
	cfgs, err := r.src.RunConfigs(ctx, period)
	if err != nil {
		return nil, 0, fmt.Errorf("virtualpaper: load the configs of %s: %w", day, err)
	}
	bars := 0
	for _, c := range cfgs {
		bars += len(c.Snapshots) - c.WarmupBars
	}
	return cfgs, bars, nil
}

// replay runs p's params over the day's configs. A day without a bar is empty.
func (r *Recorder) replay(ctx context.Context, p repo.Profile, day string, cfgs []backtest.RunConfig, bars int) (repo.Day, []repo.Trade, error) {
	d := repo.Day{ProfileID: p.ID, Day: day, Status: repo.DayDone, Bars: bars}
	if bars == 0 {
		d.Status = repo.DayEmpty
		return d, nil, nil
	}
	params, err := ParamsOf(p)
	if err != nil {
		return d, nil, err
	}
	if err := params.Validate(); err != nil {
		return d, nil, err
	}
	applied := params.Apply(cfgs)
	for i := range applied {
		applied[i].OnEntryEvaluation = func(backtest.EntryEvaluation) { d.DecisionsSeen++ }
	}
	period, err := dayPeriod(day)
	if err != nil {
		return d, nil, err
	}
	done, err := backtest.ShadowBacktestPortfolioTrades(ctx, applied, period)
	if err != nil {
		return d, nil, err
	}
	trades := make([]repo.Trade, 0, len(done))
	for _, t := range done {
		rt, err := tradeOf(t)
		if err != nil {
			return d, nil, err
		}
		if rt.ExitReason == repo.ExitReasonDataEnded {
			d.Unfinished++
		}
		trades = append(trades, rt)
	}
	d.Trades = len(trades)
	return d, trades, nil
}

// pending lists, per profile, the days it still lacks, oldest first: the days of
// [max(FirstDay, today - BacklogDays), today) without a mark.
func (r *Recorder) pending(ctx context.Context, profiles []repo.Profile, today string) (map[int64][]string, error) {
	end, err := time.ParseInLocation(time.DateOnly, today, jst)
	if err != nil {
		return nil, err
	}
	floor := end.AddDate(0, 0, -BacklogDays).Format(time.DateOnly)
	out := make(map[int64][]string, len(profiles))
	for _, p := range profiles {
		from := max(p.FirstDay, floor)
		marked, err := repo.MarkedDays(ctx, r.db, p.ID, from)
		if err != nil {
			return nil, err
		}
		start, err := time.ParseInLocation(time.DateOnly, from, jst)
		if err != nil {
			return nil, fmt.Errorf("virtualpaper: first day of profile %d: %w", p.ID, err)
		}
		for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
			if name := d.Format(time.DateOnly); !marked[name] {
				out[p.ID] = append(out[p.ID], name)
			}
		}
	}
	return out, nil
}

// oldestDay is the smallest first pending day of any profile, "" when none.
func oldestDay(pending map[int64][]string) string {
	oldest := ""
	for _, days := range pending {
		if len(days) > 0 && (oldest == "" || days[0] < oldest) {
			oldest = days[0]
		}
	}
	return oldest
}

// dayPeriod is the half-open UTC range of a JST day.
func dayPeriod(day string) (backtest.Period, error) {
	start, err := time.ParseInLocation(time.DateOnly, day, jst)
	if err != nil {
		return backtest.Period{}, fmt.Errorf("virtualpaper: day %q: %w", day, err)
	}
	return backtest.Period{Start: start, End: start.AddDate(0, 0, 1)}, nil
}

// tradeOf converts an engine trade; pnl_jpy is the net return on the notional
// the replayed Risk Engine sized (Quantity × entry price).
func tradeOf(t backtest.Trade) (repo.Trade, error) {
	for _, v := range []float64{t.EntryPrice, t.ExitPrice, t.GrossReturnPct, t.SlippageReturnPct, t.FeeReturnPct, t.NetReturnPct} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return repo.Trade{}, fmt.Errorf("virtualpaper: trade of %s has a non-finite value", t.Symbol)
		}
	}
	if !slices.Contains([]string{domain.JevDirectionLong, domain.JevDirectionShort}, t.Direction) {
		return repo.Trade{}, fmt.Errorf("virtualpaper: trade of %s has direction %q", t.Symbol, t.Direction)
	}
	return repo.Trade{
		InstrumentID: t.InstrumentID, Symbol: t.Symbol, Direction: t.Direction,
		EntryAt: t.EntryTimestamp, EntryPrice: t.EntryPrice, ExitAt: t.ExitTimestamp, ExitPrice: t.ExitPrice,
		ExitReason: t.ExitReason, Quantity: t.Quantity,
		Gross: t.GrossReturnPct, Slippage: t.SlippageReturnPct, Fee: t.FeeReturnPct, Net: t.NetReturnPct,
		PnLJPY: t.NetReturnPct * float64(t.Quantity) * t.EntryPrice / 100,
	}, nil
}

// reason is err's text cut to what virtual_paper_days.error holds.
func reason(err error) string {
	r := []rune(err.Error())
	if len(r) > maxErrorRunes {
		r = r[:maxErrorRunes]
	}
	return string(r)
}
