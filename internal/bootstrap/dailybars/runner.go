// Package dailybars is the 立花 nightly daily-bar batch (issue #729, child of
// #726): after the close it fetches every universe symbol's 日足 through the
// adapter's serial queue, at the Settings' speed and never between 8:00 and
// 15:30 JST, and keeps them in daily_bars (first run: the whole history; later
// runs: only the new days). One daily_bar_runs row per night records the
// outcome the next steps of #726 (screening, the fallback on a failed night)
// read. The package is wired in bootstrap alone, with the 立花 adapter as its
// Source.
package dailybars

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

const (
	// pollInterval is how often Run checks whether tonight's batch is due.
	pollInterval = time.Minute
	// retryBackoff is the wait before an aborted night is picked up again.
	retryBackoff = 10 * time.Minute
	// nightOffset shifts the JST clock so that 00:00〜07:59 still belongs to
	// the previous evening's night (the batch window ends at 08:00).
	nightOffset = 8 * time.Hour
)

// Source is the broker side of the batch (the 立花 adapter).
type Source interface {
	// DailyBarTargets are the symbols of the master with their 市場区分 and
	// 前日終値; market.ErrMasterNotLoaded until the morning master is loaded.
	DailyBarTargets(ctx context.Context) ([]domain.DailyBarTarget, error)
	// DailyBars is the whole 日足 history of symbol, oldest first.
	DailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error)
}

// Bars is the daily_bars store.
type Bars interface {
	Latest(ctx context.Context, symbol string) (*domain.DailyBar, error)
	Save(ctx context.Context, symbol string, bars []domain.DailyBar, replace bool) (int, error)
}

// Runs is the daily_bar_runs store.
type Runs interface {
	Get(ctx context.Context, runDate string) (domain.DailyBarRun, bool, error)
	Save(ctx context.Context, run domain.DailyBarRun) error
}

// Config configures a Runner.
type Config struct {
	Source   Source
	Bars     Bars
	Runs     Runs
	Settings tachibanasource.TachibanaNightlySettings
	// Clock defaults to the wall clock (tests inject a fake).
	Clock tachibana.Clock
}

// Runner runs the nightly batch.
type Runner struct {
	cfg        Config
	clock      tachibana.Clock
	retryAfter time.Time // set after an aborted night
}

// New returns a Runner; nothing runs until Run (or RunDue) is called.
func New(cfg Config) *Runner {
	return &Runner{cfg: cfg, clock: tachibana.OrReal(cfg.Clock)}
}

// Run checks every minute until ctx is done whether tonight's batch is due
// and runs it. A panic or error in one cycle is logged and the next cycle
// still runs.
func (r *Runner) Run(ctx context.Context) {
	safego.Loop(ctx, "tachibana nightly daily bars", func() time.Duration { return pollInterval }, r.RunDue)
}

// NightOf is the 立会日 key (YYYY-MM-DD) of the night now belongs to: the JST
// date of the evening it started in, so 00:00〜07:59 is still the previous
// date's night.
func NightOf(now time.Time) string {
	return now.In(tachibana.JST).Add(-nightOffset).Format("2006-01-02")
}

// startAt is the earliest instant of night's batch: the configured time on
// the night's date for an evening run time, on the following date for an
// early-morning one (the daytime window 8:00〜15:30 is never a start time).
func startAt(night string, runTime string) time.Time {
	day, err := time.ParseInLocation("2006-01-02", night, tachibana.JST)
	if err != nil {
		return time.Time{}
	}
	offset := tachibana.ParseClockOfDay(runTime)
	if offset < nightOffset {
		return tachibana.AtClock(day.Add(24*time.Hour), offset)
	}
	return tachibana.AtClock(day, offset)
}

// due reports whether the batch of the night now belongs to may be started at
// now: the configured time has come and now is outside 8:00〜15:30 JST. This
// is the time guard: the batch is never started in the daytime.
func (r *Runner) due(now time.Time) bool {
	if tachibana.InDaytime(now) {
		return false
	}
	return !now.Before(startAt(NightOf(now), r.cfg.Settings.RunTime))
}

// RunDue starts or resumes tonight's batch if it is due: the time has come,
// it is not daytime, the night has no succeeded run yet and the last abort is
// more than retryBackoff ago. It returns when the night ends or aborts.
func (r *Runner) RunDue(ctx context.Context) error {
	now := r.clock.Now()
	if !r.due(now) || now.Before(r.retryAfter) {
		return nil
	}
	night := NightOf(now)
	prev, found, err := r.cfg.Runs.Get(ctx, night)
	if err != nil {
		return err
	}
	if found && prev.Status == domain.DailyBarRunSucceeded {
		return nil
	}
	if !found {
		prev = domain.DailyBarRun{RunDate: night}
	}
	run := r.night(ctx, prev)
	if run.Status != domain.DailyBarRunSucceeded && ctx.Err() == nil {
		r.retryAfter = r.clock.Now().Add(retryBackoff)
	}
	if run.Status == domain.DailyBarRunFailed {
		slog.Warn("tachibana: nightly daily bars did not finish; they resume from the cursor",
			"run_date", run.RunDate, "symbols", run.Symbols, "requests", run.Requests, "saved_bars", run.SavedBars,
			"failed", run.Failed, "duration_ms", run.DurationMS, "reason", run.Error)
	}
	return nil
}
