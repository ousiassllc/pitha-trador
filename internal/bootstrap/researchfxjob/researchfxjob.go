// Package researchfxjob is the composition root's wiring of the research-only FX
// 1-minute candle record (issue #1084): the daily maintenance task that fetches
// the BID and ASK 1-minute candles of the configured currency pairs (USD_JPY by
// default) of the GMO trading days that are complete and stores them in
// research_fx_candles with a per-day marker (research_fx_days). Nothing it
// writes is a trading input; see internal/service/researchfx.
//
// The task is OFF by default (researchfxsettings.KeyEnabled, on the Settings
// screen and the Paper results MCP's settings_set): while off, Run returns
// maintenance.ErrSkipped before it makes a request or writes anything, not even
// the runner's last-success date, so a default install gains neither an outbound
// request nor a write from it. When on, a Run is one bounded chunk (one pair, one
// trading day: two klines requests) and returns maintenance.ErrMore while days are
// left; the adapter keeps to at most one HTTP call per second. It uses GMO's
// public FX API only (no key, no kabu or tachibana request).
package researchfxjob

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"
	rfx "github.com/ousiassllc/pitha-trador/internal/domain/researchfx"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchfx"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/fx/gmo"
	servicefx "github.com/ousiassllc/pitha-trador/internal/service/researchfx"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// TaskName is the maintenance task's name (its last-success date is stored
// under it), Label what the notification calls it.
const (
	TaskName = "research_fx"
	Label    = "研究用の為替 1 分足の記録"
)

// RunFrom is the JST time of day from which the task runs: after GMO's 06:00
// trading-day rollover, so the previous trading day is complete on the date the
// task's success is recorded. Before it Run returns maintenance.ErrSkipped.
const RunFrom = 7 * time.Hour

// Job records the research FX candles once the GMO trading day is over.
type Job struct {
	settings researchfxsettings.Store
	record   func(ctx context.Context) (servicefx.Result, error)
	now      func() time.Time
}

// Option customises a Job (tests).
type Option func(*options)

type options struct {
	baseURL string
	gmo     []gmo.Option
	pace    *time.Duration
	timeout time.Duration
	clock   func() time.Time
}

// WithBaseURL points the GMO adapter at another server (an httptest server).
func WithBaseURL(u string) Option { return func(o *options) { o.baseURL = u } }

// WithGMOOptions passes options to every GMO client (a fake clock, an HTTP client).
func WithGMOOptions(opts ...gmo.Option) Option { return func(o *options) { o.gmo = opts } }

// WithClock overrides time.Now (tests).
func WithClock(now func() time.Time) Option { return func(o *options) { o.clock = now } }

// WithChunkTimeout overrides the per-Run fetch deadline (tests).
func WithChunkTimeout(d time.Duration) Option { return func(o *options) { o.timeout = d } }

// WithoutPace removes the recorder's pause between Runs (tests; the adapter's
// own call limit stays).
func WithoutPace() Option {
	zero := time.Duration(0)
	return func(o *options) { o.pace = &zero }
}

// New returns the Job of db. The switch and the pairs are read from settings at
// every run, so a change on the Settings screen applies to the next run.
func New(db *sql.DB, settings *system.RuntimeSettingsRepository, opts ...Option) *Job {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	j := &Job{settings: settings, now: time.Now}
	if o.clock != nil {
		j.now = o.clock
	}
	clients := map[string]*gmo.Client{}
	fetchers := func(symbol string) (servicefx.Fetcher, error) {
		if c, ok := clients[symbol]; ok {
			return c, nil
		}
		c, err := gmo.New(gmo.Config{BaseURL: o.baseURL, Symbols: []string{symbol}}, o.gmo...)
		if err != nil {
			return nil, err
		}
		clients[symbol] = c
		return c, nil
	}
	symbols := func(ctx context.Context) ([]string, error) {
		s, err := researchfxsettings.Load(ctx, settings)
		return s.Symbols, err
	}
	rec := servicefx.NewRecorder(researchfx.NewRepository(db), fetchers, symbols).WithClock(func() time.Time { return j.now() })
	if o.pace != nil {
		rec.WithPace(*o.pace, nil)
	}
	rec.WithChunkTimeout(o.timeout)
	j.record = rec.Run
	return j
}

// Task returns the maintenance task for scheduler.WithMaintenanceTask.
func (j *Job) Task() maintenance.Task {
	return maintenance.Task{Name: TaskName, Label: Label, Run: j.Run}
}

// Run does one chunk once the JST time of day has reached RunFrom and the switch
// is on; before that, or while the switch is off, it returns
// maintenance.ErrSkipped (neither a success nor a failure, tried again at the next
// catch-up; nothing is requested or written). A switch that cannot be read counts
// as off: it is skipped with a warning, never switched on and never reported as a
// failure. A chunk that leaves days behind
// returns maintenance.ErrMore, which makes the runner call again with its lock
// released in between; one chunk's fetch is bounded by servicefx.ChunkTimeout, so
// a stuck day cannot hold that lock for minutes. The record is by-product research data, so a panic in it
// is returned as an error instead of crashing the process that also trades; a
// failure (no marker is written) is returned to the maintenance runner, which
// isolates it from the other tasks and retries after its delay.
func (j *Job) Run(ctx context.Context) (err error) {
	n := j.now().In(rfx.JST)
	if time.Duration(n.Hour())*time.Hour+time.Duration(n.Minute())*time.Minute < RunFrom {
		return maintenance.ErrSkipped
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("researchfxjob: panic while recording FX candles: %v", p)
		}
	}()
	s, err := researchfxsettings.Load(ctx, j.settings)
	if err != nil {
		// An unreadable switch is not "on": nothing is requested or written, and
		// since the feature may well be off it is no failure to notify about.
		slog.WarnContext(ctx, "researchfxjob: cannot read the FX record settings; skipping this run", "error", err)
		return maintenance.ErrSkipped
	}
	if !s.Enabled {
		return maintenance.ErrSkipped
	}
	res, err := j.record(ctx)
	if err != nil {
		return fmt.Errorf("researchfxjob: %w", err)
	}
	slog.InfoContext(ctx, "researchfxjob: FX candles recorded", "symbol", res.Symbol, "day", res.Day, "candles", res.Candles, "more", res.More)
	if res.More {
		return maintenance.ErrMore
	}
	return nil
}
