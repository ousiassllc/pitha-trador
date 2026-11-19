package nightlyjobs

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/modeltest"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// allModelsFixture is modelFixture with the 60m and close labels, and the job over a
// runner that trains every model of the nightly task (small fixture settings).
func allModelsFixture(t *testing.T, now string) (*modelJob, *system.RuntimeSettingsRepository, func() int, func() int) {
	t.Helper()
	db := modeltest.AllHorizonsDB(t, "2026-10-05", 6, 40)
	modeltest.SeedDay(t, db, "2026-10-11", modeltest.Day{Rows: 3, Seed: 80})
	modeltest.SeedDecision(t, db, modeltest.At(t, "2026-10-11", "10:00", 1), "trader", "LONG")
	at := modeltest.At(t, now[:10], now[11:], 0)
	clock := func() time.Time { return at }
	tp := featuremodel.DefaultTreeParams()
	tp.Trees, tp.MinLeaf, tp.Bins = 10, 20, 16
	runner := newFeatureRunner(db).WithParams(modelParams()).WithTreeParams(tp).WithClock(clock)
	settings := system.NewRuntimeSettingsRepository(db)
	rows := func(table string) func() int { return func() int { return modelCountRows(t, db, table) } }
	return newModelJob(runner, settings, clock), settings, rows("research_model_runs"), rows("research_model_predictions")
}

// The nightly task is wired to the ridge and the tree model of the 30m, 60m and close
// labels (featuremodel.Specs): with the production settings and a history too short for
// them, the runner lists exactly those six versions as skipped and writes nothing.
func TestModelTask_RunnerTrainsEverySpec(t *testing.T) {
	db := modeltest.AllHorizonsDB(t, "2026-10-05", 6, 40)
	modeltest.SeedDay(t, db, "2026-10-11", modeltest.Day{Rows: 3, Seed: 80})
	modeltest.SeedDecision(t, db, modeltest.At(t, "2026-10-11", "10:00", 1), "trader", "LONG")
	at := modeltest.At(t, "2026-10-12", "16:30", 0)
	res, err := newFeatureRunner(db).WithClock(func() time.Time { return at }).Run(context.Background(), 90)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"rm-ridge1-30m-2026-10-11", "rm-gbt1-30m-2026-10-11", "rm-ridge1-60m-2026-10-11", "rm-gbt1-60m-2026-10-11", "rm-ridge1-close-2026-10-11", "rm-gbt1-close-2026-10-11",
	}
	if !slices.Equal(res.Skipped, want) || res.Day != "" {
		t.Errorf("skipped %v (day %q), want %v", res.Skipped, res.Day, want)
	}
	if modelCountRows(t, db, "research_model_runs") != 0 || modelCountRows(t, db, "research_model_predictions") != 0 {
		t.Error("rows were written without enough history")
	}
}

// Switched on, the nightly task records the tree model and the 60m and close horizons:
// one model per run, ErrMore until the six of the day are done, then success.
func TestModelTask_RecordsTheTreeModelAndAllHorizonsWhenOn(t *testing.T) {
	ctx := context.Background()
	job, settings, runs, preds := allModelsFixture(t, "2026-10-12 17:00")
	if err := settings.Set(ctx, researchjobsettings.KeyModelEnabled, "1", time.Now()); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 6; i++ {
		err := job.run(ctx)
		if i < 6 && !errors.Is(err, maintenance.ErrMore) || i == 6 && err != nil {
			t.Fatalf("run %d = %v", i, err)
		}
		if runs() != i || preds() != i {
			t.Fatalf("after run %d: %d runs and %d predictions, want %d of each", i, runs(), preds(), i)
		}
	}
	if err := job.run(ctx); err != nil || runs() != 6 {
		t.Errorf("seventh run = %v with %d runs; want success and still 6", err, runs())
	}
}

// OFF (the default): the same runner trains nothing and writes nothing - not a run, not
// a prediction - whether nothing is stored, 0 is, or the time is before 16:00 JST.
func TestModelTask_AllModelsWriteNothingWhileOff(t *testing.T) {
	ctx := context.Background()
	job, settings, runs, preds := allModelsFixture(t, "2026-10-12 17:00")
	for _, stored := range []string{"", "0"} {
		if stored != "" {
			if err := settings.Set(ctx, researchjobsettings.KeyModelEnabled, stored, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		if err := job.run(ctx); !errors.Is(err, maintenance.ErrSkipped) || runs() != 0 || preds() != 0 {
			t.Fatalf("stored %q: run = %v, %d runs, %d predictions; want ErrSkipped and no row", stored, err, runs(), preds())
		}
	}
	if err := settings.Set(ctx, researchjobsettings.KeyModelEnabled, "1", time.Now()); err != nil {
		t.Fatal(err)
	}
	early := modeltest.At(t, "2026-10-12", "15:59", 0)
	job.now = func() time.Time { return early }
	if err := job.run(ctx); !errors.Is(err, maintenance.ErrSkipped) || runs() != 0 || preds() != 0 {
		t.Fatalf("at 15:59 on: run = %v, %d runs, %d predictions; want ErrSkipped and no row", err, runs(), preds())
	}
}
