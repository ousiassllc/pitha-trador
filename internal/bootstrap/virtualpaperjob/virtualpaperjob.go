// Package virtualpaperjob is the composition root's wiring of the "virtual
// Paper" record (issue #1087): the daily maintenance task that replays every
// complete JST day for each active virtual Paper profile over the stored
// market_snapshots / jev_decisions and records the virtual trades in
// virtual_paper_trades. Nothing it writes is a trading input; see
// internal/service/virtualpaper.
//
// It does NOT hook into the market-data job, the Policy Engine, Risk or
// execution: it runs from the maintenance scheduler after the close and only
// reads the history the live flow already stored, so the real Paper and the
// market-data processing cannot be slowed or broken by it. With no active
// profile (the default) Run returns maintenance.ErrSkipped before it writes
// anything, not even the runner's last-success date, so a default install gains
// no write from it. A Run is one bounded chunk (one JST day, with a wall-time
// budget) and returns maintenance.ErrMore while work is left: the runner frees
// its lock between the chunks, so a backlog never keeps the database backup or
// the retention purge waiting.
package virtualpaperjob

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/backtestsource"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
	"github.com/ousiassllc/pitha-trador/internal/service/virtualpaper"
)

// TaskName is the maintenance task's name (its last-success date is stored under
// it), Label what the notification calls it.
const (
	TaskName = "virtual_paper"
	Label    = "仮想 Paper の記録"
)

// RunFrom is the JST time of day from which the task runs: the market has closed
// (15:30) and the day's last bars are stored. Before it Run returns
// maintenance.ErrSkipped.
const RunFrom = 16 * time.Hour

// jst is the zone of RunFrom; a fixed zone keeps the result independent of the
// host's time zone.
var jst = time.FixedZone("JST", 9*60*60)

// Job records the virtual Paper trades once the JST market day is over.
type Job struct {
	record func(ctx context.Context) (virtualpaper.Result, error)
	now    func() time.Time
}

// New returns the Job of db. The baseline the profiles' params are laid over is
// the live decision flow replayed in the Paper mode (the Paper min_entry_quality,
// the thresholds, costs and Risk limits in force when a day is processed), built
// exactly as bootstrap builds its backtest source, over the repositories alone.
func New(db *sql.DB, strategy *config.StrategyConfig, risk *config.RiskConfig) *Job {
	if strategy == nil || risk == nil {
		return NewWithSource(db, unavailable{})
	}
	return NewWithSource(db, backtestsource.NewForDB(db, strategy, risk, backtestsource.ReplayPaper))
}

// NewWithSource is New with the replay config source given (tests).
func NewWithSource(db *sql.DB, src virtualpaper.Source) *Job {
	j := &Job{now: time.Now}
	j.record = virtualpaper.NewRecorder(db, src).WithClock(func() time.Time { return j.now() }).Run
	return j
}

// WithClock replaces time.Now (tests); the recorder follows it.
func (j *Job) WithClock(now func() time.Time) *Job { j.now = now; return j }

// Task returns the maintenance task for scheduler.WithMaintenanceTask.
func (j *Job) Task() maintenance.Task {
	return maintenance.Task{Name: TaskName, Label: Label, Run: j.Run}
}

// Run does one chunk once the JST time of day has reached RunFrom and a profile
// is active; before that, or with no active profile, it returns
// maintenance.ErrSkipped (neither a success nor a failure; nothing is written). A
// chunk that leaves work behind returns maintenance.ErrMore. The recording is
// by-product research data, so a panic in it is returned as an error instead of
// crashing the process that also trades; a failure is returned to the
// maintenance runner, which isolates it from the other tasks and retries after
// its delay.
func (j *Job) Run(ctx context.Context) (err error) {
	n := j.now().In(jst)
	if time.Duration(n.Hour())*time.Hour+time.Duration(n.Minute())*time.Minute < RunFrom {
		return maintenance.ErrSkipped
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("virtualpaperjob: panic while recording virtual Paper trades: %v", p)
		}
	}()
	res, err := j.record(ctx)
	if res.Active == 0 && err == nil {
		return maintenance.ErrSkipped
	}
	if res.Day != "" {
		slog.InfoContext(ctx, "virtualpaperjob: virtual Paper day recorded", "day", res.Day, "profiles", len(res.Profiles), "trades", res.Trades, "unfinished", res.Unfinished, "more", res.More)
	}
	if err != nil {
		return fmt.Errorf("virtualpaperjob: %w", err)
	}
	if res.More {
		return maintenance.ErrMore
	}
	return nil
}

// unavailable is the Source of a composition without the strategy / risk
// configuration: a Run with an active profile fails (loudly) instead of
// recording nothing.
type unavailable struct{}

func (unavailable) RunConfigs(context.Context, backtest.Period) ([]backtest.RunConfig, error) {
	return nil, errors.New("the strategy / risk configuration is not available")
}
