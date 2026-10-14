package metalabeljob

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

func newJob(t *testing.T) (*Job, *system.RuntimeSettingsRepository) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := system.NewRuntimeSettingsRepository(db)
	return New(db, settings), settings
}

// clockAt returns a clock fixed at the given JST wall time.
func clockAt(hhmm string) func() time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", "2026-10-14 "+hhmm, jst)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return t }
}

func TestJob_RunsOnlyFromTheSettingsRetrainTime(t *testing.T) {
	for name, tc := range map[string]struct {
		at, now string
		due     bool
	}{
		"before the retrain time":     {"18:00", "17:59", false},
		"exactly at the retrain time": {"18:00", "18:00", true},
		"after the retrain time":      {"18:00", "23:30", true},
		"early morning time, passed":  {"02:30", "03:00", true},
		"early morning time, early":   {"02:30", "01:00", false},
	} {
		job, settings := newJob(t)
		if err := settings.Set(context.Background(), metalabelsettings.KeyRetrainTime, `"`+tc.at+`"`, time.Now()); err != nil {
			t.Fatalf("%s: Set: %v", name, err)
		}
		job.now = clockAt(tc.now)
		err := job.Run(context.Background())
		if tc.due && err != nil {
			t.Errorf("%s: Run = %v, want a (no-history) success", name, err)
		}
		if !tc.due && !errors.Is(err, maintenance.ErrSkipped) {
			t.Errorf("%s: Run = %v, want maintenance.ErrSkipped", name, err)
		}
	}
}

func TestJob_RetrainTimeIsJSTNotTheHostZone(t *testing.T) {
	job, _ := newJob(t) // default retrain time 18:00
	// 09:30 UTC is 18:30 JST, 08:30 UTC is 17:30 JST.
	job.now = func() time.Time { return time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC) }
	if err := job.Run(context.Background()); err != nil {
		t.Errorf("18:30 JST: Run = %v, want to run", err)
	}
	job.now = func() time.Time { return time.Date(2026, 10, 14, 8, 30, 0, 0, time.UTC) }
	if err := job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Errorf("17:30 JST: Run = %v, want ErrSkipped", err)
	}
}

// A retraining with no labeled decisions is the normal early-days no-op: it
// succeeds and stores no model, so inference stays a skip.
func TestJob_NoHistoryStoresNoModel(t *testing.T) {
	job, _ := newJob(t)
	job.now = clockAt("20:00")
	if err := job.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, found, err := metalabel.NewRepository(job.db).LatestActiveModel(context.Background()); err != nil || found {
		t.Fatalf("LatestActiveModel found = %v, err %v, want no model", found, err)
	}
}

func TestJob_BadSettingsFailWithoutRunning(t *testing.T) {
	job, settings := newJob(t)
	job.now = clockAt("20:00")
	// A stored retrain time that no longer parses makes Load fall back to the
	// defaults, so the job still runs; an unreadable settings store fails it.
	if err := settings.Set(context.Background(), metalabelsettings.KeyRetrainTime, `"not a time"`, time.Now()); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := job.Run(context.Background()); err != nil {
		t.Errorf("Run with a bad stored retrain time = %v, want the default to apply", err)
	}
	if _, err := retrainDue(time.Now(), "6pm"); err == nil {
		t.Error("retrainDue accepted a malformed time")
	}
}

func TestJob_TaskIsRegisteredUnderItsName(t *testing.T) {
	job, _ := newJob(t)
	task := job.Task()
	if task.Name != TaskName || task.Label == "" || task.Run == nil {
		t.Fatalf("Task = %+v", task)
	}
}
