// Package tachibanawatch is the 立花 監視銘柄ソース (issue #730, child of
// #726): the broker has no ranking API, so the symbols to watch are decided
// the night before, after the daily-bar batch (bootstrap/dailybars) has
// stored the day's 日足. The decision is one watch list of at most
// Config.Max symbols per 立会日 - the held symbols' fixed slots (as the kabu
// ranking watch gives them), the operator's manual symbols and the 日足
// screening's top symbols (daily_screen), or the operator's list as it is
// (fixed) - saved to watch_lists so the morning uses it unchanged, before the
// 9:00 open included. When the daily bars are missing or too incomplete, the
// list falls back to the operator's fixed list, else to the previous
// 営業日's list, and Activity and the banner say so.
//
// ActiveList serves the saved list as the broker.CandidateSource of the
// rankingwatch.Watcher, which publishes it to the stream subscription,
// market-data ingestion and the Fast Screener. Nothing here calls the broker:
// the screening reads only the local daily_bars, and no price is logged.
package tachibanawatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/dailybars"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

const (
	// pollInterval is how often Run checks whether a list is due.
	pollInterval = time.Minute
	// historyDays is how far back (calendar days) the screening reads bars:
	// the surge window of 20 営業日 plus the longest holiday stretch.
	historyDays = 45
	// minCoverage is the share of the night's universe that must have the
	// newest day's bar for the screening to be trusted.
	minCoverage = 0.5
)

// Bars reads the stored daily bars (*market.DailyBarRepository).
type Bars interface {
	Recent(ctx context.Context, since string, perSymbol int) ([]domain.DailyBar, error)
}

// Runs reads the nightly batch's run records (*market.DailyBarRunRepository).
type Runs interface {
	Get(ctx context.Context, runDate string) (domain.DailyBarRun, bool, error)
}

// Lists is the watch list store (*market.WatchListRepository).
type Lists interface {
	Get(ctx context.Context, listDate string) (domain.WatchList, bool, error)
	AtOrBefore(ctx context.Context, date string) (domain.WatchList, bool, error)
	Save(ctx context.Context, list domain.WatchList) error
}

// HeldSource lists the symbols with an open position or pending order.
type HeldSource interface {
	HeldSymbols(ctx context.Context) ([]string, error)
}

// Config configures a Decider.
type Config struct {
	// Settings reads the effective settings; Decide calls it every cycle, so
	// a saved change applies from the next decision (opsettings.LoadTachibanaSource).
	Settings func(ctx context.Context) (tachibanasource.TachibanaSourceSettings, error)
	Bars     Bars
	Runs     Runs
	Lists    Lists
	Held     HeldSource
	// Max is the list size cap (broker.Capabilities.MaxStreamSymbols); zero
	// means tachibanasource.MaxTachibanaWatchSymbols.
	Max int
	// Notify receives the operator notice of a fallback list (Activity's
	// broker_notice); nil drops it.
	Notify func(message string)
	// Clock defaults to the wall clock (tests inject a fake).
	Clock tachibana.Clock
}

// Decider decides and saves the watch lists.
type Decider struct {
	cfg   Config
	clock tachibana.Clock
	max   int
	set   tachibanasource.TachibanaSourceSettings // loaded by the running Decide
}

// New returns a Decider; nothing runs until Run (or Decide) is called.
func New(cfg Config) *Decider {
	max := cfg.Max
	if max <= 0 {
		max = tachibanasource.MaxTachibanaWatchSymbols
	}
	return &Decider{cfg: cfg, clock: tachibana.OrReal(cfg.Clock), max: max}
}

// Run calls Decide every minute until ctx is done. A panic or error in one
// cycle is logged and the next cycle still runs.
func (d *Decider) Run(ctx context.Context) {
	safego.Loop(ctx, "tachibana watch list", func() time.Duration { return pollInterval }, d.Decide)
}

// Decide saves the list the clock's night is waiting for, if there is one to
// decide now (see decideNight); it is cheap when everything is decided.
func (d *Decider) Decide(ctx context.Context) error {
	set, err := d.cfg.Settings(ctx)
	if err != nil {
		return fmt.Errorf("tachibanawatch: read the settings: %w", err)
	}
	d.set = set
	now := d.clock.Now()
	if d.set.CandidateSource == tachibanasource.TachibanaSourceFixed {
		return d.decideFixed(ctx, now)
	}
	night := dailybars.NightOf(now)
	// The night that has just ended is the last chance of its bars.
	if err := d.decideNight(ctx, now, previousDay(night), true); err != nil {
		return err
	}
	return d.decideNight(ctx, now, night, false)
}

// decideFixed keeps the list of the next 立会日 equal to the operator's fixed
// list (re-saved only when it changed) and makes sure the list in use now
// exists, so switching to fixed in the middle of a day takes effect at once.
// A list already in use is not swapped during the day (the EVENT connection
// budget): a changed fixed list applies from the next 立会日.
func (d *Decider) decideFixed(ctx context.Context, now time.Time) error {
	entries := planFixed(d.max, d.set.FixedSymbols)
	next := nextTradingDay(dailybars.NightOf(now))
	for _, date := range []string{SessionDate(now), next} {
		existing, ok, err := d.cfg.Lists.Get(ctx, date)
		if err != nil {
			return err
		}
		if ok && (date != next || existing.Source == domain.WatchListFixed && sameEntries(existing.Entries, entries)) {
			continue
		}
		err = d.save(ctx, domain.WatchList{
			ListDate: date, Source: domain.WatchListFixed, DecidedAt: now, Entries: entries,
			Reason: fmt.Sprintf("運用者指定の固定リスト（%d 銘柄）", len(entries)),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// decideNight decides the list that night's bars make: the screening once the
// night's batch succeeded, or - when final (the night is over for good) - the
// fallback. A list already decided by the screening is left alone, and so is
// a fallback list that is still the answer.
func (d *Decider) decideNight(ctx context.Context, now time.Time, night string, final bool) error {
	target := nextTradingDay(night)
	if target < SessionDate(now) {
		return nil // that 立会日 is over: no list is needed any more
	}
	existing, has, err := d.cfg.Lists.Get(ctx, target)
	if err != nil {
		return err
	}
	if has && existing.Source == domain.WatchListDailyScreen {
		return nil
	}
	run, found, err := d.cfg.Runs.Get(ctx, night)
	if err != nil {
		return err
	}
	switch {
	case found && run.Status == domain.DailyBarRunSucceeded:
		picked, reason, err := d.screen(ctx, night, run)
		if err != nil {
			return err
		}
		if reason == "" {
			return d.saveScreened(ctx, now, target, picked)
		}
		return d.fallback(ctx, now, target, reason, has && existing.Fallback())
	case !final:
		return nil
	case !found:
		return d.fallback(ctx, now, target, "夜間の日足取得が実行されませんでした", has && existing.Fallback())
	default:
		return d.fallback(ctx, now, target, "夜間の日足取得が完了しませんでした", has && existing.Fallback())
	}
}

// screened is the outcome of a usable screening.
type screened struct {
	basis    string
	universe int
	picks    []Pick
}

// screen runs the screening over the stored bars of night. reason is "" on
// success; otherwise it says why the bars are unusable.
func (d *Decider) screen(ctx context.Context, night string, run domain.DailyBarRun) (screened, string, error) {
	basis := lastTradingDay(night)
	since := dayBefore(basis, historyDays)
	bars, err := d.cfg.Bars.Recent(ctx, since, barsPerSymbol)
	if err != nil {
		return screened{}, "", err
	}
	bySymbol := make(map[string][]domain.DailyBar)
	for _, b := range bars {
		bySymbol[b.Symbol] = append(bySymbol[b.Symbol], b)
	}
	for _, symbol := range d.set.Nightly.ExcludeSymbols {
		delete(bySymbol, symbol)
	}
	covered := 0
	for _, bs := range bySymbol {
		if bs[len(bs)-1].TradeDate == basis {
			covered++
		}
	}
	switch {
	case covered == 0:
		return screened{}, fmt.Sprintf("基準日（%s）の日足がありません", basis), nil
	case run.Symbols > 0 && float64(covered) < minCoverage*float64(run.Symbols):
		return screened{}, fmt.Sprintf("基準日（%s）の日足が欠けています（%d / %d 銘柄）", basis, covered, run.Symbols), nil
	}
	picks := Screen(bySymbol, basis, d.set.Screen)
	slog.Info("tachibana: daily screening done", "basis_date", basis, "universe", covered, "picked", len(picks))
	return screened{basis: basis, universe: covered, picks: picks}, "", nil
}
