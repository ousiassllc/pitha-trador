package nightlyjobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
	repomodel "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/modeltest"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// modelParams are the production hyperparameters scaled to the small fixture (6 days of
// 40 bars); TestDefaultParams in internal/service/featuremodel pins the production ones.
func modelParams() featuremodel.Params {
	p := featuremodel.DefaultParams()
	p.RidgeLambda, p.MinTrainRows = 20, 200
	return p
}

// modelFixture is a database with 6 complete label days (2026-10-05..10), the trader
// decisions (with bars) of 2026-10-11 and 2026-10-12, and the model job over it with
// the clock at now (JST). Nothing is stored in runtime_settings: the shipped state.
func modelFixture(t *testing.T, now string) (*modelJob, *sql.DB, *system.RuntimeSettingsRepository) {
	t.Helper()
	db := modeltest.HistoryDB(t, "2026-10-05", 6, 40)
	for _, day := range []string{"2026-10-11", "2026-10-12"} {
		modeltest.SeedDay(t, db, day, modeltest.Day{Rows: 3, Seed: 80})
		modeltest.SeedDecision(t, db, modeltest.At(t, day, "10:00", 1), "trader", "LONG")
	}
	at := modeltest.At(t, now[:10], now[11:], 0)
	clock := func() time.Time { return at }
	settings := system.NewRuntimeSettingsRepository(db)
	runner := featuremodel.NewRunner(repomodel.NewRepository(db)).WithParams(modelParams()).WithClock(clock)
	return newModelJob(runner, settings, clock), db, settings
}

// modelDBFingerprint hashes every row of every table: the database content, byte for byte.
func modelDBFingerprint(t *testing.T, db *sql.DB) string {
	t.Helper()
	tables, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tables.Next() {
		var n string
		if err := tables.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := tables.Err(); err != nil {
		t.Fatal(err)
	}
	_ = tables.Close()
	h := sha256.New()
	for _, name := range names {
		rows, err := db.Query(`SELECT * FROM "` + name + `" ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		_, _ = fmt.Fprintf(h, "%s|", name)
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			_, _ = fmt.Fprintf(h, "%v;", vals)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
	}
	return hex.EncodeToString(h.Sum(nil))
}

func modelCountRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// OFF by default: with nothing stored, a stored 0 or a damaged value, the task trains
// and writes nothing - the database is unchanged, not even the maintenance runner's
// last-success row - and the same holds before 16:00 JST with the switch on. Switched on
// after 16:00 the very next run trains.
func TestModelTask_IsOffByDefaultAndBeforeSixteenWritesNothing(t *testing.T) {
	ctx := context.Background()
	for name, stored := range map[string]string{"nothing stored": "", "stored 0": "0", "damaged": `"yes"`, "out of range": "2"} {
		job, db, settings := modelFixture(t, "2026-10-13 16:30")
		if stored != "" {
			if err := settings.Set(ctx, researchjobsettings.KeyModelEnabled, stored, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		before := modelDBFingerprint(t, db)
		if err := job.run(ctx); !errors.Is(err, maintenance.ErrSkipped) {
			t.Fatalf("%s: run = %v, want maintenance.ErrSkipped", name, err)
		}
		runner := maintenance.NewRunner(settings, maintenance.WithClock(job.now))
		runner.Add(job.task())
		runner.CatchUp(ctx)
		if after := modelDBFingerprint(t, db); after != before {
			t.Fatalf("%s: the database changed while the switch is off", name)
		}
		if _, ok, _ := settings.Get(ctx, "system.maintenance.research_model.last_success_date"); ok {
			t.Fatalf("%s: the runner stored a last-success date for a task that did nothing", name)
		}
	}

	job, db, settings := modelFixture(t, "2026-10-13 15:59")
	if err := settings.Set(ctx, researchjobsettings.KeyModelEnabled, "1", time.Now()); err != nil {
		t.Fatal(err)
	}
	before := modelDBFingerprint(t, db)
	if err := job.run(ctx); !errors.Is(err, maintenance.ErrSkipped) || modelDBFingerprint(t, db) != before {
		t.Fatalf("at 15:59 JST with the switch on: run = %v (or the database changed), want ErrSkipped and no write", err)
	}
	at := modeltest.At(t, "2026-10-13", "16:00", 0)
	job.now = func() time.Time { return at }
	if err := job.run(ctx); !errors.Is(err, maintenance.ErrMore) {
		t.Fatalf("at 16:00 with the switch on: run = %v, want ErrMore (two days to do)", err)
	}
	if modelCountRows(t, db, "research_model_runs") != 1 {
		t.Error("the first chunk did not write exactly one run")
	}
}

// One day per run: the first run trains 2026-10-11 and says there is more, the second
// trains 2026-10-12 and succeeds, a third has nothing to do; each run has its own
// predictions.
func TestModelTask_ChunksOneDayPerRunWithErrMore(t *testing.T) {
	ctx := context.Background()
	job, db, settings := modelFixture(t, "2026-10-12 17:00")
	if err := settings.Set(ctx, researchjobsettings.KeyModelEnabled, "1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := job.run(ctx); !errors.Is(err, maintenance.ErrMore) {
		t.Fatalf("first run = %v, want ErrMore", err)
	}
	if err := job.run(ctx); err != nil {
		t.Fatalf("second run = %v, want success", err)
	}
	var versions []string
	rows, err := db.Query(`SELECT version FROM research_model_runs ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if got := strings.Join(versions, ","); got != "rm-ridge1-30m-2026-10-11,rm-ridge1-30m-2026-10-12" {
		t.Errorf("runs = %s", got)
	}
	if modelCountRows(t, db, "research_model_predictions") != 2 {
		t.Errorf("%d predictions, want 2 (one decision per day)", modelCountRows(t, db, "research_model_predictions"))
	}
	before := modelDBFingerprint(t, db)
	if err := job.run(ctx); err != nil || modelDBFingerprint(t, db) != before {
		t.Errorf("third run = %v (or it wrote); want success with nothing new", err)
	}
}

// The retention setting reaches the purge: old model rows go, recent stay.
func TestModelTask_AppliesTheRetentionSetting(t *testing.T) {
	ctx := context.Background()
	job, db, settings := modelFixture(t, "2026-10-12 17:00")
	for k, v := range map[string]string{researchjobsettings.KeyModelEnabled: "1", researchjobsettings.KeyModelRetentionDays: "30"} {
		if err := settings.Set(ctx, k, v, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, day := range []string{"2026-08-01", "2026-10-01"} {
		if _, err := db.Exec(`INSERT INTO research_model_runs (version, algorithm, label_horizon, features_json, hyperparams_json, coefficients_json, train_from, train_to, predict_day,
	n_train, n_predicted, train_digest) VALUES (?, 'ridge', '30m', '[]', '{}', '{}', '2026-01-01', '2026-01-02', ?, 1, 0, 'x')`, "old-"+day, day); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		if err := job.run(ctx); err == nil {
			break
		}
	}
	var left []string
	rows, _ := db.Query(`SELECT predict_day FROM research_model_runs ORDER BY predict_day`)
	for rows.Next() {
		var d string
		_ = rows.Scan(&d)
		left = append(left, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if got := strings.Join(left, ","); got != "2026-10-01,2026-10-11,2026-10-12" {
		t.Errorf("runs left = %s, want the 30-day-old-or-newer ones", got)
	}
}

type modelPanicRunner struct{ err error }

func (p modelPanicRunner) Run(context.Context, int) (featuremodel.Result, error) {
	if p.err != nil {
		return featuremodel.Result{}, p.err
	}
	panic("boom")
}

// A panic in the by-product training is an error for the maintenance runner, never a
// crash of the process that also trades; a failure is returned (not skipped).
func TestModelTask_PanicAndFailureAreErrors(t *testing.T) {
	ctx := context.Background()
	_, _, settings := modelFixture(t, "2026-10-12 17:00")
	if err := settings.Set(ctx, researchjobsettings.KeyModelEnabled, "1", time.Now()); err != nil {
		t.Fatal(err)
	}
	now := modeltest.At(t, "2026-10-12", "17:00", 0)
	clock := func() time.Time { return now }
	err := newModelJob(modelPanicRunner{}, settings, clock).run(ctx)
	if err == nil || !strings.Contains(err.Error(), "panic") || errors.Is(err, maintenance.ErrSkipped) {
		t.Errorf("panic: run = %v, want a panic error", err)
	}
	boom := errors.New("boom")
	if err := newModelJob(modelPanicRunner{err: boom}, settings, clock).run(ctx); !errors.Is(err, boom) || errors.Is(err, maintenance.ErrSkipped) {
		t.Errorf("failure: run = %v, want the runner's error", err)
	}
}
