// Package metalabeljob is the composition root's nightly meta-labeling
// retraining job (issue #764, parent #752): the daily maintenance task that
// trains from the stored triple-barrier labels and saves the model, from the
// Settings retrain time on, with its settings read from the Settings screen's
// runtime_settings.
package metalabeljob

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	servicemetalabel "github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// TaskName is the maintenance task's name (its last-success date is stored
// under it), Label what the notification calls it.
const (
	TaskName = "metalabel_retrain"
	Label    = "メタラベリング夜間再学習"
)

// jst is the zone of the Settings retrain time; a fixed zone keeps the result
// independent of the host's time zone.
var jst = time.FixedZone("JST", 9*60*60)

// Job retrains the model once the Settings retrain time of day has passed.
type Job struct {
	db       *sql.DB
	trainer  *training.Trainer
	settings *system.RuntimeSettingsRepository
	now      func() time.Time
}

// New returns the Job of db reading its settings (model, calibration, retrain
// time, minimum EV, LLM cutoff) at every run, so a change on the Settings
// screen applies without a restart.
func New(db *sql.DB, settings *system.RuntimeSettingsRepository) *Job {
	j := &Job{db: db, settings: settings, now: time.Now}
	repo := metalabel.NewRepository(db)
	j.trainer = training.NewTrainer(repo, servicemetalabel.NewStore(repo), j.load)
	return j
}

func (j *Job) load(ctx context.Context) (metalabelsettings.Settings, error) {
	return metalabelsettings.Load(ctx, j.settings)
}

// Task returns the maintenance task for scheduler.WithMaintenanceTask.
func (j *Job) Task() maintenance.Task {
	return maintenance.Task{Name: TaskName, Label: Label, Run: j.Run}
}

// Run trains and stores one model once the Settings retrain time (JST) has
// passed today. Before that it returns maintenance.ErrSkipped, so the day's
// task is neither a success nor a failure yet and is tried again at the next
// catch-up. A failure (unreadable settings, a training or save error) is
// returned and the previously stored model stays in force; too little labeled
// history is the normal early-days condition and succeeds without storing
// anything (training.Trainer.Retrain).
func (j *Job) Run(ctx context.Context) error {
	s, err := j.load(ctx)
	if err != nil {
		return fmt.Errorf("metalabeljob: read settings: %w", err)
	}
	due, err := retrainDue(j.now(), s.RetrainTime)
	if err != nil {
		return err
	}
	if !due {
		return maintenance.ErrSkipped
	}
	return j.trainer.Retrain(ctx)
}

// retrainDue reports whether the JST time of day of now has reached at ("HH:MM").
func retrainDue(now time.Time, at string) (bool, error) {
	t, err := time.Parse("15:04", at)
	if err != nil {
		return false, fmt.Errorf("metalabeljob: retrain time %q is not HH:MM: %w", at, err)
	}
	n := now.In(jst)
	return n.Hour()*60+n.Minute() >= t.Hour()*60+t.Minute(), nil
}
