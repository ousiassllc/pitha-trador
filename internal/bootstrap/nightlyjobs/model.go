package nightlyjobs

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
	repomodel "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/researchjobstatus"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// ModelRunFrom is the JST time of day from which the model task runs: the market has
// closed (15:30) and the day's bars, decisions and labels are stored. Before it Run
// returns maintenance.ErrSkipped, so today is never trained half-way.
const ModelRunFrom = 16 * time.Hour

// modelJST is the zone of ModelRunFrom; a fixed zone keeps the result independent of
// the host's time zone.
var modelJST = time.FixedZone("JST", 9*60*60)

// ModelTaskName is the maintenance task's name (its last-success date and last failure are stored under it).
const ModelTaskName = researchjobstatus.ModelTask

// ModelTask is the research model's nightly training task (issues #1092, #1123). It is
// OFF by default (research.model.enabled): while off - or before 16:00 JST - Run returns
// maintenance.ErrSkipped before it reads or writes anything, not even the runner's
// last-success date. When on, one Run trains and records one model (ridge or tree, of
// the 30m, 60m or close label) of one JST day and returns maintenance.ErrMore while
// runs or old rows are left. See internal/service/featuremodel.
func ModelTask(db *sql.DB, settings *system.RuntimeSettingsRepository) maintenance.Task {
	return newModelJob(newFeatureRunner(db), settings, time.Now).task()
}

// newFeatureRunner is the runner of the nightly task: the ridge and the tree model of
// every label horizon (featuremodel.Specs).
func newFeatureRunner(db *sql.DB) *featuremodel.Runner {
	return featuremodel.NewRunner(repomodel.NewRepository(db)).WithSpecs(featuremodel.Specs()...)
}

// modelRunner is what the job needs of featuremodel.Runner.
type modelRunner interface {
	Run(ctx context.Context, retentionDays int) (featuremodel.Result, error)
}

type modelJob struct {
	runner   modelRunner
	settings researchjobsettings.Store
	now      func() time.Time
}

func newModelJob(runner modelRunner, settings researchjobsettings.Store, now func() time.Time) *modelJob {
	return &modelJob{runner: runner, settings: settings, now: now}
}

func (j *modelJob) task() maintenance.Task {
	return maintenance.Task{Name: ModelTaskName, Label: "研究用モデルの夜間の学習", Run: j.run, TrackFailure: true}
}

// run does one chunk once the JST time of day has reached ModelRunFrom and the switch
// is on. The model is by-product research data, so a panic in it is returned as an
// error instead of crashing the process that also trades.
func (j *modelJob) run(ctx context.Context) (err error) {
	n := j.now().In(modelJST)
	if time.Duration(n.Hour())*time.Hour+time.Duration(n.Minute())*time.Minute < ModelRunFrom {
		return maintenance.ErrSkipped
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("nightlyjobs: panic while training the research model: %v", p)
		}
	}()
	s, err := researchjobsettings.Model(ctx, j.settings)
	if err != nil {
		return fmt.Errorf("nightlyjobs: %w", err)
	}
	if !s.Enabled {
		return maintenance.ErrSkipped
	}
	res, err := j.runner.Run(ctx, s.RetentionDays)
	if err != nil {
		return fmt.Errorf("nightlyjobs: research model: %w", err)
	}
	slog.InfoContext(ctx, "nightlyjobs: research model run", "day", res.Day, "version", res.Version, "n_train", res.NTrain, "predictions", res.NPredict, "skipped_days", len(res.Skipped), "purged_rows", res.Purged, "more", res.More)
	if res.More {
		return maintenance.ErrMore
	}
	return nil
}
