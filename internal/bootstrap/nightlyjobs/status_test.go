package nightlyjobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/researchjobstatus"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

type failingModelRunner struct{ err error }

func (r failingModelRunner) Run(context.Context, int) (featuremodel.Result, error) {
	return featuremodel.Result{}, r.err
}

// Both tasks are registered with failure tracking, and the names the Settings card reads are the tasks' own.
func TestTasksTrackTheirFailuresUnderTheNamesTheStatusReads(t *testing.T) {
	if EvaluationTaskName != researchjobstatus.EvalTask || ModelTaskName != researchjobstatus.ModelTask {
		t.Fatalf("task names differ from the status service's: %q %q", EvaluationTaskName, ModelTaskName)
	}
	for _, task := range []maintenance.Task{(&evaluationJob{}).task(), (&modelJob{}).task()} {
		if !task.TrackFailure {
			t.Errorf("%s must track its failure for the Settings card", task.Name)
		}
	}
}

// A failing model run is stored by the real runner and read back as a failure; off stays off.
func TestModelFailureReachesTheStatusAndOffIsNotAFailure(t *testing.T) {
	_, _, settings := modelFixture(t, "2026-10-12 17:00")
	at := time.Date(2026, 10, 12, 17, 0, 0, 0, modelJST)
	job := newModelJob(failingModelRunner{errors.New("train: not enough rows")}, settings, func() time.Time { return at })
	r := maintenance.NewRunner(settings, maintenance.WithClock(func() time.Time { return at }))
	r.Add(job.task())

	// Off: the task is skipped, nothing is written, the status says off.
	r.CatchUp(t.Context())
	got, err := researchjobstatus.New(settings).Tasks(t.Context())
	if err != nil || got[1].State != researchjobstatus.Off {
		t.Fatalf("off: %+v %v", got, err)
	}
	if _, ok, _ := settings.Get(t.Context(), "system.maintenance.research_model.last_failure"); ok {
		t.Fatal("an off task must not record a failure")
	}

	if err := settings.Set(t.Context(), researchjobsettings.KeyModelEnabled, "1", at); err != nil {
		t.Fatal(err)
	}
	r.CatchUp(t.Context())
	got, err = researchjobstatus.New(settings).Tasks(t.Context())
	if err != nil || got[1].State != researchjobstatus.Failed || got[1].FailureReason != "nightlyjobs: research model: train: not enough rows" {
		t.Fatalf("on+failure: %+v %v", got, err)
	}
}
