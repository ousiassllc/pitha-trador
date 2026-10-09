package dailybars_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/dailybars"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func TestNeverRunsInTheDaytime(t *testing.T) {
	for _, now := range []time.Time{
		tt.AtJST(2026, 10, 8, 8, 0), tt.AtJST(2026, 10, 8, 12, 0), tt.AtJST(2026, 10, 8, 15, 29, 59),
	} {
		f := newFixture(t, now)
		f.cfg.Settings.RunTime = "18:00"
		f.addSymbol("7203", "prime", 0, bar("7203", "2026-10-07", 100))
		f.run(t)
		if len(f.source.calls) != 0 {
			t.Errorf("at %s: %d requests sent in the daytime, want 0", now.Format("15:04:05"), len(f.source.calls))
		}
	}
}

func TestWaitsForTheConfiguredTime(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 17, 59))
	f.addSymbol("7203", "prime", 0, bar("7203", "2026-10-08", 100))
	f.run(t)
	if len(f.source.calls) != 0 {
		t.Fatalf("started before the run time: %v", f.source.calls)
	}
	f.clock.set(tt.AtJST(2026, 10, 8, 18, 0))
	f.run(t)
	if len(f.source.calls) != 1 {
		t.Fatalf("requests at 18:00 = %v, want one", f.source.calls)
	}
}

func TestEarlyMorningRunTimeBelongsToThePreviousNight(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 9, 0, 30))
	f.cfg.Settings.RunTime = "01:00"
	f.addSymbol("7203", "prime", 0, bar("7203", "2026-10-08", 100))
	f.run(t)
	if len(f.source.calls) != 0 {
		t.Fatalf("started before 01:00: %v", f.source.calls)
	}
	f.clock.set(tt.AtJST(2026, 10, 9, 1, 0))
	f.run(t)
	if run := f.storedRun(t, "2026-10-08"); run.Status != domain.DailyBarRunSucceeded {
		t.Fatalf("night 2026-10-08 = %+v", run)
	}
}

func TestSpacesRequestsByTheConfiguredRate(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 18, 0))
	f.cfg.Settings.MaxPerSecond = 0.5 // one request every two seconds
	for _, s := range []string{"1001", "1002", "1003"} {
		f.addSymbol(s, "prime", 0, bar(s, "2026-10-08", 100))
	}
	f.run(t)
	if fmt.Sprint(f.source.calls) != "[1001 1002 1003]" {
		t.Fatalf("requests = %v, want one at a time in symbol order", f.source.calls)
	}
	for i := 1; i < len(f.source.times); i++ {
		if gap := f.source.times[i].Sub(f.source.times[i-1]); gap < 2*time.Second {
			t.Errorf("gap before request %d = %s, want >= 2s", i, gap)
		}
	}
}

func TestStopsWhenTheDaytimeBeginsMidRun(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 9, 7, 59, 58))
	f.cfg.Settings.RunTime = "01:00"
	for _, s := range []string{"1001", "1002", "1003", "1004"} {
		f.addSymbol(s, "prime", 0, bar(s, "2026-10-08", 100))
	}
	f.run(t)
	if fmt.Sprint(f.source.calls) != "[1001 1002]" {
		t.Fatalf("requests = %v, want to stop at 08:00", f.source.calls)
	}
	run := f.storedRun(t, "2026-10-08")
	if run.Status != domain.DailyBarRunFailed || run.Cursor != "1002" {
		t.Fatalf("run = %+v, want failed at cursor 1002", run)
	}
}

func TestSavesHistoryThenOnlyTheNewDays(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 7, 18, 0))
	f.addSymbol("7203", "prime", 0, bar("7203", "2026-10-05", 100), bar("7203", "2026-10-06", 101), bar("7203", "2026-10-07", 102))
	f.run(t)
	if run := f.storedRun(t, "2026-10-07"); run.SavedBars != 3 || run.Requests != 1 || run.Status != domain.DailyBarRunSucceeded {
		t.Fatalf("first night = %+v, want 3 bars saved by one request", run)
	}

	f.source.history["7203"] = append(f.source.history["7203"], bar("7203", "2026-10-08", 103))
	f.clock.set(tt.AtJST(2026, 10, 8, 18, 0))
	f.run(t)
	if run := f.storedRun(t, "2026-10-08"); run.SavedBars != 1 {
		t.Fatalf("second night saved %d bars, want only the new day", run.SavedBars)
	}
	stored, _ := f.bars.ListBySymbol(context.Background(), "7203")
	if len(stored) != 4 || stored[3].TradeDate != "2026-10-08" || stored[3].Close != 103 {
		t.Fatalf("stored = %+v", stored)
	}

	// The same night is not run twice.
	calls := len(f.source.calls)
	f.run(t)
	if len(f.source.calls) != calls {
		t.Fatalf("a succeeded night sent %d more requests", len(f.source.calls)-calls)
	}
}

func TestReplacesHistoryWhenASplitChangedTheAdjustedValues(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 7, 18, 0))
	f.addSymbol("7203", "prime", 0, bar("7203", "2026-10-06", 200), bar("7203", "2026-10-07", 210))
	f.run(t)

	// A 1:2 split: the adjusted values of the old days halve.
	split := func(b domain.DailyBar) domain.DailyBar {
		b.AdjOpen, b.AdjHigh, b.AdjLow, b.AdjClose, b.AdjVolume = b.Open/2, b.High/2, b.Low/2, b.Close/2, b.Volume*2
		return b
	}
	f.source.history["7203"] = []domain.DailyBar{split(bar("7203", "2026-10-06", 200)), split(bar("7203", "2026-10-07", 210)), bar("7203", "2026-10-08", 106)}
	f.clock.set(tt.AtJST(2026, 10, 8, 18, 0))
	f.run(t)

	stored, _ := f.bars.ListBySymbol(context.Background(), "7203")
	if len(stored) != 3 || stored[1].AdjClose != 105 || stored[1].Close != 210 {
		t.Fatalf("stored = %+v, want the history rewritten with the new adjustment", stored)
	}
}

func TestUniverseFollowsTheSettings(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 18, 0))
	f.cfg.Settings.MinPriceJPY = 100
	f.cfg.Settings.ExcludeSymbols = []string{"1004"}
	f.addSymbol("1001", "prime", 500)
	f.addSymbol("1002", "growth", 500)  // market not selected
	f.addSymbol("1003", "standard", 50) // below the price floor
	f.addSymbol("1004", "prime", 500)   // excluded
	f.addSymbol("1005", "standard", 0)  // price unknown: kept
	f.addSymbol("1006", "other", 500)   // market not selected
	f.run(t)
	if fmt.Sprint(f.source.calls) != "[1001 1005]" {
		t.Fatalf("requests = %v, want [1001 1005]", f.source.calls)
	}
	if run := f.storedRun(t, "2026-10-08"); run.Symbols != 2 {
		t.Fatalf("run.Symbols = %d, want 2", run.Symbols)
	}
}

func TestCountsFailuresAndKeepsGoing(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 18, 0))
	f.addSymbol("1001", "prime", 0, bar("1001", "2026-10-08", 100))
	f.addSymbol("1002", "prime", 0)
	f.addSymbol("1003", "prime", 0, bar("1003", "2026-10-08", 100))
	f.source.errs["1002"] = errors.New("boom")
	f.run(t)
	run := f.storedRun(t, "2026-10-08")
	if run.Status != domain.DailyBarRunSucceeded || run.Failed != 1 || run.Requests != 3 || run.SavedBars != 2 || run.DurationMS <= 0 {
		t.Fatalf("run = %+v", run)
	}
}

func TestLostSessionFailsTheNightAndResumesAfterTheCursor(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 18, 0))
	for _, s := range []string{"1001", "1002", "1003"} {
		f.addSymbol(s, "prime", 0, bar(s, "2026-10-08", 100))
	}
	f.source.errs["1002"] = broker.ErrNoSession
	runner := dailybars.New(f.cfg)
	if err := runner.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := f.storedRun(t, "2026-10-08")
	if run.Status != domain.DailyBarRunFailed || run.Cursor != "1001" || run.Requests != 1 {
		t.Fatalf("run = %+v, want failed after 1001", run)
	}

	// Within the backoff nothing is retried; after it the night resumes at 1002.
	if err := runner.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.source.calls) != "[1001 1002]" {
		t.Fatalf("requests within the backoff = %v", f.source.calls)
	}
	delete(f.source.errs, "1002")
	f.clock.set(f.clock.Now().Add(11 * time.Minute))
	if err := runner.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.source.calls) != "[1001 1002 1002 1003]" {
		t.Fatalf("requests = %v, want the retry to start at 1002", f.source.calls)
	}
	run = f.storedRun(t, "2026-10-08")
	if run.Status != domain.DailyBarRunSucceeded || run.Requests != 3 || run.SavedBars != 3 {
		t.Fatalf("resumed run = %+v", run)
	}
}

func TestMasterNotLoadedFailsTheNight(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 18, 0))
	f.source.targetErr = errors.New("master not loaded")
	f.run(t)
	run := f.storedRun(t, "2026-10-08")
	if run.Status != domain.DailyBarRunFailed || run.Error == "" || len(f.source.calls) != 0 {
		t.Fatalf("run = %+v", run)
	}
}
