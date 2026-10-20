// Package metalabeljob is the composition root's meta-labeling wiring (issue
// #764/#765, parent #752). Job is the nightly retraining: the daily maintenance task that
// trains from the stored triple-barrier labels and saves the model, from the
// Settings retrain time on, with its settings read from the Settings screen's
// runtime_settings. PolicyOption wires the live entry rule from the same
// settings.
package metalabeljob

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperbootstrapsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	servicemetalabel "github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
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
	// The adoption honours the freeze after a policy.* change of the
	// self-improvement flow (issue #780).
	store := servicemetalabel.NewStore(repo)
	// A retraining skipped for lack of data is recorded for the Calibration
	// screen and the MCP (issue #799).
	j.trainer = training.NewTrainer(repo, store, j.load,
		training.WithPolicyChanges(judgement.NewProposalRepository(db)), training.WithSkipRecorder(store))
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

// PolicyOption wires the live entry rule (issue #765): the Policy Engine enters
// a candidate only when the active model's calibrated EV clears the Settings
// minimum EV (read at every decision, so a change applies without a restart),
// and stores each verdict. The model and predictions live in db.
func PolicyOption(db *sql.DB, settings *system.RuntimeSettingsRepository) policy.Option {
	store := servicemetalabel.NewStore(metalabel.NewRepository(db))
	return policy.WithMetaModel(store, store, func(ctx context.Context) (servicemetalabel.DecideConfig, error) {
		s, err := metalabelsettings.Load(ctx, settings)
		if err != nil {
			return servicemetalabel.DecideConfig{}, err
		}
		cfg := servicemetalabel.DefaultDecideConfig()
		cfg.MinEVBps = s.MinEVBps
		return cfg, nil
	})
}

// PaperBootstrapOption wires the Paper-only bootstrap entry (issue #798): while
// no model is adopted and the Settings switch is on, the Policy Engine enters
// on the legacy threshold rule. This build runs Paper Trading only
// (internal/bootstrap/risk.go), so the trading mode is Paper; the switch has
// no effect in any other mode, and must keep that when a Live build is added.
func PaperBootstrapOption(settings *system.RuntimeSettingsRepository) policy.Option {
	return policy.WithPaperBootstrap(metalabelsettings.RuntimeMode, paperbootstrapsettings.Source{Store: settings})
}

// ChangeGuard limits the self-improvement flow's policy.* changes against the
// meta-labeling model adoptions in db (issue #780, FR-SELFIMPROVE-10): the
// rate limit between changes and the freeze around a model adoption.
func ChangeGuard(db *sql.DB) selfimprove.Option {
	return selfimprove.WithChangeGuard(changeguard.DefaultLimits(), servicemetalabel.NewStore(metalabel.NewRepository(db)))
}
