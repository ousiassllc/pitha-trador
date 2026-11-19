package nightlyjobs

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	servicerecord "github.com/ousiassllc/pitha-trador/internal/service/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/researchjobstatus"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// EvaluationTaskName is the maintenance task's name (its last-success date is stored under it).
const EvaluationTaskName = researchjobstatus.EvalTask

// EvaluationRunFrom is the JST time of day from which the task runs: the market has closed
// (15:30) and the day's last bars are stored, so a day is recorded once it is complete.
const EvaluationRunFrom = 16 * time.Hour

// evalJST is the zone of EvaluationRunFrom; a fixed zone keeps the result independent of the host's.
var evalJST = time.FixedZone("JST", 9*60*60)

// evaluationJob records the daily Jev evaluation (issue #1089).
type evaluationJob struct {
	settings researchjobsettings.Store
	record   func(ctx context.Context) (servicerecord.Result, error)
	now      func() time.Time
}

// EvaluationTask is the daily evaluation record's maintenance task (issue #1089): once the
// JST market day is over it stores the main numbers of the day's Jev evaluation per cohort
// in jev_evaluation_daily and deletes the rows older than research.eval.retention_days.
//
// It is OFF by default (research.eval.enabled): while off, Run returns
// maintenance.ErrSkipped before it reads or writes anything else, not even the runner's
// last-success date. When on, a Run is one chunk (the oldest unrecorded JST day with
// trader decisions of the last seven days, or one purge chunk) and returns
// maintenance.ErrMore while work is left, so the runner frees its lock between the chunks.
// The record is by-product research data: a panic in it is returned as an error instead of
// crashing the process that also trades.
func EvaluationTask(db *sql.DB, settings *system.RuntimeSettingsRepository) maintenance.Task {
	return newEvaluationJob(db, settings).task()
}

func newEvaluationJob(db *sql.DB, settings *system.RuntimeSettingsRepository) *evaluationJob {
	j := &evaluationJob{settings: settings, now: time.Now}
	rec := servicerecord.New(evalrecord.NewRepository(db), db, func(ctx context.Context) (int, error) {
		s, err := researchjobsettings.Eval(ctx, settings)
		return s.RetentionDays, err
	}).WithClock(func() time.Time { return j.now() })
	j.record = rec.Run
	return j
}

func (j *evaluationJob) task() maintenance.Task {
	return maintenance.Task{Name: EvaluationTaskName, Label: "Jev 評価の日次記録", Run: j.run, TrackFailure: true}
}

// run does one chunk once the JST time of day has reached EvaluationRunFrom and the switch is on;
// before that, or while the switch is off, it returns maintenance.ErrSkipped (nothing is written).
func (j *evaluationJob) run(ctx context.Context) (err error) {
	n := j.now().In(evalJST)
	if time.Duration(n.Hour())*time.Hour+time.Duration(n.Minute())*time.Minute < EvaluationRunFrom {
		return maintenance.ErrSkipped
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("nightlyjobs: panic while recording the Jev evaluation: %v", p)
		}
	}()
	s, err := researchjobsettings.Eval(ctx, j.settings)
	if err != nil {
		return fmt.Errorf("nightlyjobs: %w", err)
	}
	if !s.Enabled {
		return maintenance.ErrSkipped
	}
	res, err := j.record(ctx)
	if err != nil {
		return fmt.Errorf("nightlyjobs: record the Jev evaluation: %w", err)
	}
	slog.InfoContext(ctx, "nightlyjobs: Jev evaluation recorded", "day", res.Day, "new_rows", res.Rows, "purged_rows", res.Purged, "more", res.More)
	if res.More {
		return maintenance.ErrMore
	}
	return nil
}
