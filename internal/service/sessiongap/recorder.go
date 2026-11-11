package sessiongap

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/healthalert"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/retention"
)

const (
	// CheckInterval is how often the Recorder looks (and, in session, observes).
	CheckInterval = time.Minute
	// SettleDelay is how long after the close a day waits before its verdict is
	// recorded, so the last minute's market-data jobs have finished.
	SettleDelay = 5 * time.Minute
)

// Record is what is persisted for one finished day (migration 000052).
type Record struct {
	Observation
	// JobsSucceeded and JobsFailed count the market-data jobs that finished on
	// the day; nil when the jobs table no longer holds the whole day
	// (retention.DefaultSucceededJobDays), so a 0 never means "purged".
	JobsSucceeded, JobsFailed *int
	RecordedAt                time.Time
}

// Store persists the daily records. Finalize must never overwrite a finished
// row (the first verdict stands) but must keep the alert codes and checks
// Touch accumulated for an in-progress one.
type Store interface {
	// Finalized reports whether date has a finished (not in-progress) row.
	Finalized(ctx context.Context, date string) (bool, error)
	// Unfinished lists the dates before beforeDate that have only an
	// in-progress row (the app stopped before the day was finished).
	Unfinished(ctx context.Context, beforeDate string) ([]string, error)
	// Touch records, for the in-progress day date, the health alert codes
	// observed now and, when inSession, one more watch check.
	Touch(ctx context.Context, now time.Time, date string, alertCodes []string, inSession bool) error
	// Observe classifies day from market_snapshots (see Observe).
	Observe(ctx context.Context, day time.Time) (Observation, error)
	// MarketDataJobs counts the market-data jobs that finished on day.
	MarketDataJobs(ctx context.Context, day time.Time) (succeeded, failed int, err error)
	Finalize(ctx context.Context, r Record) error
}

// Config wires a Recorder. Now defaults to time.Now.
type Config struct {
	Store Store
	// Alerts returns the health alerts to show now (healthalert.Detector.Active).
	Alerts func(ctx context.Context) ([]string, error)
	Now    func() time.Time
}

// Recorder keeps the daily session diagnostics: while the app runs through a
// session it accumulates the health alert codes it sees, after the close it
// records the day's verdict, and after a start it records the previous trading
// day (and any day it left in progress) if the app was not running at its
// close. It only writes session_diagnostics_daily; it changes no setting and
// places no order.
type Recorder struct{ cfg Config }

// NewRecorder returns a Recorder.
func NewRecorder(cfg Config) *Recorder {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Recorder{cfg: cfg}
}

// Check runs one pass. It is idempotent: a day that already has its verdict is
// left as it is.
func (r *Recorder) Check(ctx context.Context) error {
	now := r.cfg.Now()
	today := now.In(marketcalendar.JST).Format(time.DateOnly)
	var errs []error
	if _, inWindow := healthalert.KabuWindow(now); inWindow {
		alerts, err := r.cfg.Alerts(ctx)
		if err == nil {
			err = r.cfg.Store.Touch(ctx, now, today, alerts, marketcalendar.TSE.IsOpen(now))
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("sessiongap: observe session: %w", err))
		}
	}

	days, err := r.due(ctx, now, today)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, day := range days {
		if err := r.finalize(ctx, now, day); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// due lists the days (00:00 JST) whose verdict is to be recorded: days left in
// progress, the previous trading day and, once settled, today; each only when
// it has none yet.
func (r *Recorder) due(ctx context.Context, now time.Time, today string) ([]time.Time, error) {
	var candidates []string
	left, err := r.cfg.Store.Unfinished(ctx, today)
	if err != nil {
		return nil, fmt.Errorf("sessiongap: unfinished days: %w", err)
	}
	candidates = append(candidates, left...)
	if prev, ok := marketcalendar.TSE.PreviousTradingDay(now); ok {
		candidates = append(candidates, prev.Format(time.DateOnly))
	}
	if closeAt, ok := marketcalendar.TSE.CloseAt(now); ok && !now.Before(closeAt.Add(SettleDelay)) {
		candidates = append(candidates, today)
	}
	var days []time.Time
	seen := map[string]bool{}
	for _, date := range candidates {
		if seen[date] {
			continue
		}
		seen[date] = true
		day, err := time.ParseInLocation(time.DateOnly, date, marketcalendar.JST)
		if err != nil || !marketcalendar.TSE.IsTradingDay(day) {
			continue
		}
		done, err := r.cfg.Store.Finalized(ctx, date)
		if err != nil {
			return nil, fmt.Errorf("sessiongap: finalized %s: %w", date, err)
		}
		if !done {
			days = append(days, day)
		}
	}
	return days, nil
}

func (r *Recorder) finalize(ctx context.Context, now, day time.Time) error {
	o, err := r.cfg.Store.Observe(ctx, day)
	if err != nil {
		return fmt.Errorf("sessiongap: observe %s: %w", day.Format(time.DateOnly), err)
	}
	rec := Record{Observation: o, RecordedAt: now}
	// The jobs table keeps succeeded jobs for a few days only: an older day is
	// partly purged, so its counts would read as zero.
	if !day.Before(now.AddDate(0, 0, -retention.DefaultSucceededJobDays)) {
		ok, failed, err := r.cfg.Store.MarketDataJobs(ctx, day)
		if err != nil {
			return fmt.Errorf("sessiongap: market-data jobs of %s: %w", o.Date, err)
		}
		rec.JobsSucceeded, rec.JobsFailed = &ok, &failed
	}
	if err := r.cfg.Store.Finalize(ctx, rec); err != nil {
		return fmt.Errorf("sessiongap: record %s: %w", o.Date, err)
	}
	return nil
}

// Run checks every CheckInterval, the first time at once (the start-up pass
// records the previous trading day), until ctx is done.
func (r *Recorder) Run(ctx context.Context) {
	wait := time.Duration(0)
	safego.Loop(ctx, "session gap record", func() time.Duration {
		w := wait
		wait = CheckInterval
		return w
	}, r.Check)
}
