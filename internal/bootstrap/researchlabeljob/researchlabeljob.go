// Package researchlabeljob is the composition root's wiring of the research-only
// horizon labels (issue #1074): the daily maintenance task that records the
// 30-minute / 60-minute / to-the-close labels of every market snapshot of the
// JST days that are complete (research_horizon_labels). Nothing it writes is a
// trading input; see internal/service/researchlabel.
package researchlabeljob

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	servicelabel "github.com/ousiassllc/pitha-trador/internal/service/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// TaskName is the maintenance task's name (its last-success date is stored
// under it), Label what the notification calls it.
const (
	TaskName = "research_labels"
	Label    = "研究用ホライズンラベルの記録"
)

// RunFrom is the JST time of day from which the task runs: the market has
// closed (15:30) and the day's last bars are stored. Before it Run returns
// maintenance.ErrSkipped, so a day is labeled only once it is complete and
// "today" is never half-labeled.
const RunFrom = 16 * time.Hour

// jst is the zone of RunFrom; a fixed zone keeps the result independent of the
// host's time zone.
var jst = time.FixedZone("JST", 9*60*60)

// Job records the research labels once the JST market day is over.
type Job struct {
	record func(ctx context.Context) (servicelabel.Result, error)
	now    func() time.Time
}

// New returns the Job of db, reading the label costs from settings at every
// run, so a cost change on the Settings screen applies to the next day recorded.
func New(db *sql.DB, settings *system.RuntimeSettingsRepository) *Job {
	j := &Job{now: time.Now}
	rec := servicelabel.NewRecorder(researchlabel.NewRepository(db), func(ctx context.Context) (servicelabel.Costs, error) {
		return servicelabel.CostsFromSettings(ctx, settings)
	}).WithClock(func() time.Time { return j.now() })
	j.record = rec.Run
	return j
}

// Task returns the maintenance task for scheduler.WithMaintenanceTask.
func (j *Job) Task() maintenance.Task {
	return maintenance.Task{Name: TaskName, Label: Label, Run: j.Run}
}

// Run records the pending days once the JST time of day has reached RunFrom;
// before that it returns maintenance.ErrSkipped (neither a success nor a failure,
// tried again at the next catch-up). The recording is by-product research data,
// so a panic in it is returned as an error instead of crashing the process that
// also trades; a failure is returned to the maintenance runner, which isolates
// it from the other tasks and retries after its delay.
func (j *Job) Run(ctx context.Context) (err error) {
	n := j.now().In(jst)
	if time.Duration(n.Hour())*time.Hour+time.Duration(n.Minute())*time.Minute < RunFrom {
		return maintenance.ErrSkipped
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("researchlabeljob: panic while recording research labels: %v", p)
		}
	}()
	res, err := j.record(ctx)
	if err != nil {
		return fmt.Errorf("researchlabeljob: %w", err)
	}
	slog.InfoContext(ctx, "researchlabeljob: research labels recorded", "days", res.Days, "snapshots", res.Snapshots, "new_rows", res.Rows)
	return nil
}
