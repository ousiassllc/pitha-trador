package researchfxjob

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// The record is opt-in: with nothing stored in runtime_settings (a default
// install), a stored 0 or a damaged value the task makes ZERO external requests
// and writes nothing: no candle, no marker, not even the maintenance runner's
// last-success row. Turning the switch on is picked up by the next run.
func TestJob_IsOffByDefaultMakesNoRequestAndWritesNothingWhileOff(t *testing.T) {
	tables := []string{"research_fx_candles", "research_fx_days", "runtime_settings"}
	for name, stored := range map[string]string{"nothing stored": "", "stored 0": "0", "damaged": `"yes"`, "out of range": "2"} {
		e := newEnv(t)
		if stored != "" {
			e.set(t, researchfxsettings.KeyEnabled, stored)
		}
		before := map[string]int{}
		for _, tbl := range tables {
			before[tbl] = e.count(t, `SELECT COUNT(*) FROM `+tbl)
		}

		if err := e.job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
			t.Fatalf("%s: Run = %v, want maintenance.ErrSkipped", name, err)
		}
		// Through the real runner: a skipped task is not recorded as done either.
		runner := maintenance.NewRunner(e.settings, maintenance.WithClock(func() time.Time { return e.now }))
		runner.Add(e.job.Task())
		runner.CatchUp(context.Background())

		if n := e.gmo.count(); n != 0 {
			t.Fatalf("%s: the mock GMO server saw %d requests while the switch was off, want exactly 0", name, n)
		}
		for _, tbl := range tables {
			if got := e.count(t, `SELECT COUNT(*) FROM `+tbl); got != before[tbl] {
				t.Errorf("%s: %s rows %d -> %d while the switch was off", name, tbl, before[tbl], got)
			}
		}
		if _, ok, _ := e.settings.Get(context.Background(), "system.maintenance."+TaskName+".last_success_date"); ok {
			t.Errorf("%s: a last-success date was written for a task that did nothing", name)
		}
	}
}

func TestJob_TurningTheSwitchOnIsPickedUpWithoutARestart(t *testing.T) {
	e := newEnv(t)
	if err := e.job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("off: Run = %v, want ErrSkipped", err)
	}
	e.set(t, researchfxsettings.KeyEnabled, "1")
	e.runAll(t)
	if e.gmo.count() == 0 {
		t.Fatal("no request after the switch was turned on")
	}
	// ...and off again: no more requests.
	e.set(t, researchfxsettings.KeyEnabled, "0")
	e.now = e.now.Add(24 * time.Hour)
	before := e.gmo.count()
	if err := e.job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("off again: Run = %v, want ErrSkipped", err)
	}
	if e.gmo.count() != before {
		t.Fatal("a request was made after the switch was turned off")
	}
}

// research.fx.symbols picks the pairs: a pair other than the default is
// requested (and only the configured ones), and a damaged value falls back to USD_JPY.
func TestJob_RecordsTheConfiguredPairs(t *testing.T) {
	e := newEnv(t)
	e.set(t, researchfxsettings.KeyEnabled, "1")
	e.set(t, researchfxsettings.KeySymbols, `"USD_JPY,EUR_USD"`)
	e.runAll(t)
	if e.gmo.count() != 28 {
		t.Fatalf("requests = %d, want 28 (7 days x 2 pairs x BID/ASK)", e.gmo.count())
	}
	for _, sym := range []string{"USD_JPY", "EUR_USD"} {
		if got := e.count(t, `SELECT COUNT(*) FROM research_fx_days WHERE symbol = ?`, sym); got != 7 {
			t.Errorf("%s markers = %d, want 7", sym, got)
		}
	}
}
