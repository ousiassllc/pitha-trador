// Package researchindexjob is the composition root's wiring of the research-only
// index record (issue #1084): the daily maintenance task that copies the index
// values (日経平均 = 101 and TOPIX = 151 by default) from the 立花 quote recording
// (issue #1083) of the trading days that are complete into
// research_index_minutes, with a per-day marker (research_index_days) that says
// "recorded" or "unavailable" (立花 had no value of the index). Nothing it writes
// is a trading input; see internal/service/researchindex.
//
// The task is OFF by default (researchindexsettings.KeyEnabled, on the Settings
// screen and the Paper results MCP's settings_set): while off, Run returns
// maintenance.ErrSkipped before it reads or writes anything, not even the runner's
// last-success date. It makes no request to any service at all (it reads the local
// table), so it consumes no kabu registration slot and no 立花 request.
package researchindexjob

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchindexsettings"
	rindex "github.com/ousiassllc/pitha-trador/internal/domain/researchindex"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchindex"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	service "github.com/ousiassllc/pitha-trador/internal/service/researchindex"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// TaskName is the maintenance task's name (its last-success date is stored under
// it), Label what the notification calls it.
const (
	TaskName = "research_index"
	Label    = "研究用の指数の 1 分値の記録"
)

// RunFrom is the JST time of day from which the task runs: after the day's last
// minute is stored (service.CompleteAfter). Before it Run returns
// maintenance.ErrSkipped.
const RunFrom = service.CompleteAfter

// Job records the research index values once the trading day is over.
type Job struct {
	settings researchindexsettings.Store
	record   func(ctx context.Context) (service.Result, error)
	now      func() time.Time
}

// Option customises a Job (tests).
type Option func(*Job)

// WithClock overrides time.Now (tests).
func WithClock(now func() time.Time) Option { return func(j *Job) { j.now = now } }

// New returns the Job of db. The switch and the codes are read from settings at
// every run, so a change on the Settings screen applies to the next run.
func New(db *sql.DB, settings *system.RuntimeSettingsRepository, opts ...Option) *Job {
	j := &Job{settings: settings, now: time.Now}
	for _, opt := range opts {
		opt(j)
	}
	repo := researchindex.NewRepository(db)
	symbols := func(ctx context.Context) ([]string, error) {
		s, err := researchindexsettings.Load(ctx, settings)
		return s.Symbols, err
	}
	j.record = service.NewRecorder(repo, repo, symbols).WithClock(func() time.Time { return j.now() }).Run
	return j
}

// Task returns the maintenance task for scheduler.WithMaintenanceTask.
func (j *Job) Task() maintenance.Task {
	return maintenance.Task{Name: TaskName, Label: Label, Run: j.Run}
}

// Run does one pass once the JST time of day has reached RunFrom and the switch is
// on; before that, or while the switch is off, it returns maintenance.ErrSkipped
// (neither a success nor a failure, tried again at the next catch-up; nothing is
// read or written). A switch that cannot be read counts as off: it is skipped with
// a warning, never switched on and never reported as a failure. The record is
// by-product research data, so a panic in it is returned as an error instead of
// crashing the process that also trades; a failure (no marker is written for the
// failing unit) is returned to the maintenance runner, which isolates it from the
// other tasks and retries after its delay.
func (j *Job) Run(ctx context.Context) (err error) {
	n := j.now().In(rindex.JST)
	if time.Duration(n.Hour())*time.Hour+time.Duration(n.Minute())*time.Minute < RunFrom {
		return maintenance.ErrSkipped
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("researchindexjob: panic while recording the index values: %v", p)
		}
	}()
	s, err := researchindexsettings.Load(ctx, j.settings)
	if err != nil {
		slog.WarnContext(ctx, "researchindexjob: cannot read the index record settings; skipping this run", "error", err)
		return maintenance.ErrSkipped
	}
	if !s.Enabled {
		return maintenance.ErrSkipped
	}
	res, err := j.record(ctx)
	if err != nil {
		return fmt.Errorf("researchindexjob: %w", err)
	}
	slog.InfoContext(ctx, "researchindexjob: index values recorded", "recorded_days", res.Recorded, "unavailable_days", res.Unavailable, "minutes", res.Minutes)
	return nil
}
