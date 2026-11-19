package nightlyjobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	servicerecord "github.com/ousiassllc/pitha-trador/internal/service/evalrecord"
	fx "github.com/ousiassllc/pitha-trador/internal/service/evalrecord/evalfixture"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

func evalCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func evalClock(s string) func() time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, evalJST)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return t }
}

// newEvalJob is a job over a migrated database holding three JST days of decisions
// (2026-10-05..07) with nothing stored in runtime_settings: the shipped state (off).
func newEvalJob(t *testing.T) (*evaluationJob, *sql.DB, *system.RuntimeSettingsRepository) {
	t.Helper()
	db := fx.OpenSeeded(t, "three-days", func(db *sql.DB) {
		for day := 5; day <= 7; day++ {
			at := time.Date(2026, 10, day, 9, 0, 0, 0, evalJST)
			fx.Seed(t, db, fx.Spec{ID: int64(day), At: at, Direction: "LONG", Model: "m1", Version: "v3", Scenario: fx.Up, Passed: day == 5})
		}
	})
	settings := system.NewRuntimeSettingsRepository(db)
	return newEvaluationJob(db, settings), db, settings
}

func switchEval(t *testing.T, s *system.RuntimeSettingsRepository, key, value string) {
	t.Helper()
	if err := s.Set(context.Background(), key, value, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// OFF by default (nothing stored, a stored 0, a damaged value): ErrSkipped and nothing is
// written to any table, not even the maintenance runner's last-success date; switching on is
// picked up by the very next run.
func TestEvaluationJob_OffByDefaultWritesNothing(t *testing.T) {
	ctx := context.Background()
	tables := []string{"jev_evaluation_daily", "runtime_settings", "jev_decisions", "market_snapshots", "research_model_runs"}
	snapshot := func(db *sql.DB) string {
		out := ""
		for _, tb := range tables {
			out += fmt.Sprintf("%s=%d ", tb, evalCount(t, db, tb))
		}
		return out
	}
	for name, stored := range map[string]string{"nothing stored": "", "stored 0": "0", "damaged": `"yes"`} {
		job, db, settings := newEvalJob(t)
		if stored != "" {
			switchEval(t, settings, researchjobsettings.KeyEvalEnabled, stored)
		}
		job.now = evalClock("2026-10-07 16:30")
		before := snapshot(db)
		if err := job.run(ctx); !errors.Is(err, maintenance.ErrSkipped) {
			t.Fatalf("%s: run = %v, want ErrSkipped", name, err)
		}
		now := job.now()
		runner := maintenance.NewRunner(settings, maintenance.WithClock(func() time.Time { return now }))
		runner.Add(job.task())
		runner.CatchUp(ctx)
		if after := snapshot(db); after != before {
			t.Fatalf("%s: tables changed while off: %s -> %s", name, before, after)
		}
		if _, ok, _ := settings.Get(ctx, "system.maintenance."+EvaluationTaskName+".last_success_date"); ok {
			t.Fatalf("%s: the runner stored a last-success date for a task that did nothing", name)
		}
		switchEval(t, settings, researchjobsettings.KeyEvalEnabled, "1")
		if err := job.run(ctx); !errors.Is(err, maintenance.ErrMore) || evalCount(t, db, "jev_evaluation_daily") != 1 {
			t.Fatalf("%s: after switching on run = %v, rows %d; want ErrMore and one row", name, err, evalCount(t, db, "jev_evaluation_daily"))
		}
	}
}

// Before 16:00 JST nothing is recorded even when on (today is not complete yet).
func TestEvaluationJob_BeforeSixteenIsSkipped(t *testing.T) {
	job, db, settings := newEvalJob(t)
	switchEval(t, settings, researchjobsettings.KeyEvalEnabled, "1")
	for _, at := range []string{"2026-10-07 00:00", "2026-10-07 15:59"} {
		job.now = evalClock(at)
		if err := job.run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
			t.Fatalf("%s: run = %v, want ErrSkipped", at, err)
		}
	}
	if n := evalCount(t, db, "jev_evaluation_daily"); n != 0 {
		t.Fatalf("rows = %d before 16:00", n)
	}
	job.now = evalClock("2026-10-07 16:00")
	if err := job.run(context.Background()); !errors.Is(err, maintenance.ErrMore) {
		t.Fatalf("at 16:00: run = %v, want ErrMore", err)
	}
}

// On, the oldest pending day is one chunk: ErrMore twice, then nil; a further run does
// nothing. Through the real runner the task is only marked done after the last chunk.
func TestEvaluationJob_ChunksOneDayPerRunThenDone(t *testing.T) {
	ctx := context.Background()
	job, db, settings := newEvalJob(t)
	switchEval(t, settings, researchjobsettings.KeyEvalEnabled, "1")
	job.now = evalClock("2026-10-07 16:30")
	for i, want := range []struct {
		err  error
		rows int
	}{{maintenance.ErrMore, 1}, {maintenance.ErrMore, 2}, {nil, 3}, {nil, 3}} {
		if err := job.run(ctx); !errors.Is(err, want.err) || evalCount(t, db, "jev_evaluation_daily") != want.rows {
			t.Fatalf("run %d = %v, rows %d; want %v and %d rows", i+1, err, evalCount(t, db, "jev_evaluation_daily"), want.err, want.rows)
		}
	}

	job2, db2, settings2 := newEvalJob(t)
	switchEval(t, settings2, researchjobsettings.KeyEvalEnabled, "1")
	job2.now = evalClock("2026-10-07 16:30")
	now := job2.now()
	var rowsAtGap []int
	runner := maintenance.NewRunner(settings2, maintenance.WithClock(func() time.Time { return now }), maintenance.WithChunkWait(func(context.Context) {
		rowsAtGap = append(rowsAtGap, evalCount(t, db2, "jev_evaluation_daily"))
		if _, ok, _ := settings2.Get(ctx, "system.maintenance."+EvaluationTaskName+".last_success_date"); ok {
			t.Error("recorded as done before the backlog was finished")
		}
	}))
	runner.Add(job2.task())
	runner.CatchUp(ctx)
	if fmt.Sprint(rowsAtGap) != "[1 2]" || evalCount(t, db2, "jev_evaluation_daily") != 3 {
		t.Fatalf("rows at the gaps %v, final %d; want [1 2] and 3", rowsAtGap, evalCount(t, db2, "jev_evaluation_daily"))
	}
	if _, ok, _ := settings2.Get(ctx, "system.maintenance."+EvaluationTaskName+".last_success_date"); !ok {
		t.Error("the finished task has no last-success date")
	}
}

// The retention comes from research.eval.retention_days (default 365) and only the record's
// rows are purged; switching off afterwards stops the purge too.
func TestEvaluationJob_RetentionFromSettings(t *testing.T) {
	ctx := context.Background()
	job, db, settings := newEvalJob(t)
	switchEval(t, settings, researchjobsettings.KeyEvalEnabled, "1")
	job.now = evalClock("2026-10-07 16:30")
	for range 3 {
		if err := job.run(ctx); err != nil && !errors.Is(err, maintenance.ErrMore) {
			t.Fatal(err)
		}
	}
	decisions := evalCount(t, db, "jev_decisions")
	// Default 365 days: a year later 10-05..10-07 are still kept on 2027-10-07 (cutoff 2026-10-07 deletes 10-05, 10-06).
	job.now = evalClock("2027-10-06 16:30") // cutoff 2026-10-06: only 10-05 goes
	if err := job.run(ctx); err != nil || evalCount(t, db, "jev_evaluation_daily") != 2 {
		t.Fatalf("run = %v, rows %d; want 2 rows left", err, evalCount(t, db, "jev_evaluation_daily"))
	}
	switchEval(t, settings, researchjobsettings.KeyEvalRetentionDays, "30")
	job.now = evalClock("2026-10-30 16:30") // cutoff 2026-09-30: nothing to purge, nothing pending (older than the window)
	if err := job.run(ctx); err != nil || evalCount(t, db, "jev_evaluation_daily") != 2 {
		t.Fatalf("30 days: run = %v, rows %d", err, evalCount(t, db, "jev_evaluation_daily"))
	}
	job.now = evalClock("2026-11-08 16:30") // cutoff 2026-10-09: all gone
	if err := job.run(ctx); err != nil || evalCount(t, db, "jev_evaluation_daily") != 0 {
		t.Fatalf("run = %v, rows %d; want 0", err, evalCount(t, db, "jev_evaluation_daily"))
	}
	if evalCount(t, db, "jev_decisions") != decisions {
		t.Error("jev_decisions changed")
	}
	// Off again: nothing is purged although everything is old.
	testRow(t, db)
	switchEval(t, settings, researchjobsettings.KeyEvalEnabled, "0")
	job.now = evalClock("2030-01-01 16:30")
	if err := job.run(ctx); !errors.Is(err, maintenance.ErrSkipped) || evalCount(t, db, "jev_evaluation_daily") != 1 {
		t.Fatalf("off: run = %v, rows %d; want ErrSkipped and the old row kept", err, evalCount(t, db, "jev_evaluation_daily"))
	}
}

func testRow(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO jev_evaluation_daily (day, model_id, question_version, horizon_minutes, take_profit_bps, stop_loss_bps, barrier_cost_bps,
	decisions, evaluated, entered, wins, llm_sum_bps, random_sum_bps, momentum_sum_bps, contrarian_sum_bps) VALUES ('2020-01-01', 'm', 'v', 10, 30, 30, 10, 0, 0, 0, 0, 0, 0, 0, 0)`); err != nil {
		t.Fatal(err)
	}
}

// A failure and a panic of the recording are returned as errors (the runner isolates them),
// never crash the caller; the evaluation's ErrLimitExceeded is such an error.
func TestEvaluationJob_FailureAndPanicBecomeErrors(t *testing.T) {
	ctx := context.Background()
	job, _, settings := newEvalJob(t)
	switchEval(t, settings, researchjobsettings.KeyEvalEnabled, "1")
	job.now = evalClock("2026-10-07 16:30")
	boom := errors.New("boom")
	job.record = func(context.Context) (servicerecord.Result, error) { return servicerecord.Result{}, boom }
	if err := job.run(ctx); !errors.Is(err, boom) || errors.Is(err, maintenance.ErrMore) || errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("failure: run = %v, want the failure", err)
	}
	job.record = func(context.Context) (servicerecord.Result, error) { panic("kaboom") }
	if err := job.run(ctx); err == nil || errors.Is(err, maintenance.ErrMore) || errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("panic: run = %v, want an error", err)
	}
}

// The shipped constructor returns the registered task with the documented name.
func TestEvaluationTask_Shape(t *testing.T) {
	db := fx.OpenSeeded(t, "empty", func(*sql.DB) {})
	task := EvaluationTask(db, system.NewRuntimeSettingsRepository(db))
	if task.Name != "jev_evaluation_daily" || task.Run == nil {
		t.Fatalf("task = %+v", task)
	}
}
