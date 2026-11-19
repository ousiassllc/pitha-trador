// Package jquantswire is the composition root's wiring of the J-Quants read
// adapter (issue #1093): the daily maintenance task `jquants` that backfills the
// daily and minute bars the application is missing, re-labels the research labels
// of the days whose minute bars it completed, and, when asked on the Settings
// screen, deletes everything J-Quants-sourced (the clean-up after cancelling).
//
// The task is OFF by default (jquantssettings.KeyEnabled, on the Settings screen
// only; the Paper results MCP cannot write it). While off, with no API key, or
// with no endpoint subscribed, Run returns maintenance.ErrSkipped before it sends
// a request or writes anything. It never touches a kabu adapter, the live market
// data path, Risk, orders, Paper guards or the Jev; what it stores is research
// data, private-use under J-Quants' terms. The key (secret JQUANTS_API_KEY) is read
// once at start-up like every other secret (a change needs a restart) and is held
// only by the client.
package jquantswire

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jquantssettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/jquants"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchlabel/jquantslabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	jqservice "github.com/ousiassllc/pitha-trador/internal/service/marketdata/jquants"
	"github.com/ousiassllc/pitha-trador/internal/service/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/service/researchlabel/jquantsrelabel"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// TaskName is the maintenance task's name (its last-success date is stored under
// it), Label what the notification calls it.
const (
	TaskName = "jquants"
	Label    = "J-Quants の日足・分足の取り込み"
)

// SettingsStore is the runtime_settings access the Job needs;
// *system.RuntimeSettingsRepository implements it.
type SettingsStore interface {
	jquantssettings.Store
	// Delete removes a key (the purge request, once done).
	Delete(ctx context.Context, key string) error
}

// Job runs the J-Quants adapter as one maintenance task.
type Job struct {
	settings SettingsStore
	backfill func(ctx context.Context) (jqservice.Result, error)
	purge    func(ctx context.Context) (jqservice.PurgeResult, error)
}

// Options are the injectable parts of a Job (tests).
type Options struct {
	// Client overrides the HTTP client and base URL of the J-Quants client.
	Client jqservice.Config
	// Now overrides the clock; nil means time.Now.
	Now func() time.Time
}

// New returns the Job of db. apiKey is the secret JQUANTS_API_KEY ("" = no key,
// the Job never sends a request).
func New(db *sql.DB, settings *system.RuntimeSettingsRepository, apiKey string) *Job {
	return NewWithOptions(db, settings, apiKey, Options{})
}

// NewWithOptions is New with injected parts.
func NewWithOptions(db *sql.DB, settings SettingsStore, apiKey string, opts Options) *Job {
	repo := jquants.NewRepository(db)
	labels := jquantslabel.NewRepository(db)
	loader := func(ctx context.Context) (jquantssettings.Settings, error) {
		return jquantssettings.Load(ctx, settings)
	}
	capSource := func(ctx context.Context) (int, error) {
		s, err := loader(ctx)
		return s.DailyRequestCap, err
	}
	ledger := jqservice.NewCapLedger(repo, capSource)
	cfg := opts.Client
	cfg.APIKey, cfg.Ledger = apiKey, ledger
	if opts.Now != nil {
		cfg.Now = opts.Now
		ledger.WithClock(opts.Now)
	}
	client := jqservice.NewClient(cfg)
	costs := func(ctx context.Context) (researchlabel.Costs, error) {
		return researchlabel.CostsFromSettings(ctx, settings)
	}
	relabeler := jquantsrelabel.New(labels, repo, costs)
	relabel := func(ctx context.Context, day string) (int, bool, error) {
		res, err := relabeler.Relabel(ctx, day)
		if errors.Is(err, jquantsrelabel.ErrDayNotRecorded) {
			return 0, false, nil
		}
		return res.NewRows, err == nil, err
	}
	backfiller := jqservice.NewBackfiller(client, repo, labels, relabel, loader, client.HasKey())
	if opts.Now != nil {
		backfiller.WithClock(opts.Now)
		relabeler.WithClock(opts.Now)
	}
	return &Job{
		settings: settings,
		backfill: func(ctx context.Context) (jqservice.Result, error) { return backfiller.Run(ctx, jqservice.Request{}) },
		purge:    func(ctx context.Context) (jqservice.PurgeResult, error) { return jqservice.Purge(ctx, repo, relabeler) },
	}
}

// Task returns the maintenance task for scheduler.WithMaintenanceTask.
func (j *Job) Task() maintenance.Task {
	return maintenance.Task{Name: TaskName, Label: Label, Run: j.Run}
}

// Run does one chunk. A purge request comes first (it needs neither the switch nor
// the key, and nothing is fetched in the same run); otherwise, with the switch
// off, no key or no endpoint subscribed it returns maintenance.ErrSkipped having
// sent nothing and written nothing. A run that stopped at the call bound with
// progress returns maintenance.ErrMore; one that stopped at the day's request cap
// returns nil (the rest continues tomorrow). The data is by-product research data,
// so a panic is returned as an error instead of crashing the process that also
// trades; a refused key or a 429 is returned to the runner, which retries after
// its delay.
func (j *Job) Run(ctx context.Context) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("jquantswire: panic in the J-Quants task: %v", p)
		}
	}()
	s, err := jquantssettings.Load(ctx, j.settings)
	if err != nil {
		return fmt.Errorf("jquantswire: %w", err)
	}
	if s.PurgeRequested {
		return j.runPurge(ctx, s.Enabled)
	}
	if !s.Enabled {
		return maintenance.ErrSkipped
	}
	res, err := j.backfill(ctx)
	if err != nil {
		return fmt.Errorf("jquantswire: %w", err)
	}
	if res.Skipped != "" {
		return maintenance.ErrSkipped
	}
	slog.InfoContext(ctx, "jquantswire: J-Quants backfill chunk done", "calls", res.Calls, "daily_days", len(res.DailyDays), "minute_days", len(res.MinuteDays),
		"daily_rows", res.DailyRows, "minute_rows", res.MinuteRows, "relabeled_days", len(res.Relabeled), "relabel_rows", res.RelabelRows, "cap_reached", res.CapReached, "more", res.More)
	if res.More && !res.CapReached {
		return maintenance.ErrMore
	}
	return nil
}

// runPurge deletes the J-Quants data, then the request itself. A failure keeps
// the request, so the next check deletes the rest.
func (j *Job) runPurge(ctx context.Context, enabled bool) error {
	if _, err := j.purge(ctx); err != nil {
		return fmt.Errorf("jquantswire: purge: %w", err)
	}
	if err := j.settings.Delete(ctx, jquantssettings.KeyPurgeRequested); err != nil {
		return fmt.Errorf("jquantswire: clear the purge request: %w", err)
	}
	if !enabled {
		return maintenance.ErrSkipped
	}
	return nil
}
