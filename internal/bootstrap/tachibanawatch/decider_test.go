package tachibanawatch_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/tachibanawatch"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// seedScreenable stores 12 consecutive days up to 2026-10-08 (Thu) for a few
// symbols: RISE jumps 10% on the last day, VOL's volume spikes, QUIET does
// nothing.
func seedScreenable(t *testing.T, f *fixture) {
	t.Helper()
	first := day(2026, 9, 27) // 12 days: 09-27 .. 10-08
	flat := constant(11, 100)
	f.saveBars(t, "RISE", first, append(flat, 110), constant(12, 1000))
	f.saveBars(t, "VOL", first, append(flat, 100), append(constant(11, 1000), 9000))
	f.saveBars(t, "QUIET", first, append(flat, 100), constant(12, 1000))
	f.saveRun(t, "2026-10-08", domain.DailyBarRunSucceeded, 3)
}

func onlyIndicators(names ...string) []tachibanasource.TachibanaScreenIndicator {
	var out []tachibanasource.TachibanaScreenIndicator
	for _, n := range tachibanasource.TachibanaScreenIndicators {
		ind := tachibanasource.TachibanaScreenIndicator{Name: n, Weight: 1}
		for _, want := range names {
			if want == n {
				ind.TopN = 1
			}
		}
		out = append(out, ind)
	}
	return out
}

func TestDecide_DailyScreenFillsHeldThenManualThenScreened(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 19, 0))
	seedScreenable(t, f)
	f.held = heldList{"HELD1"}
	f.settings.ManualSymbols = []string{"MANUAL"}
	f.settings.Screen = onlyIndicators(tachibanasource.ScreenGainRate, tachibanasource.ScreenVolumeSurge)
	f.max = 4

	f.decide(t)

	list, ok := f.list(t, "2026-10-09")
	if !ok {
		t.Fatal("no list for the next 立会日 2026-10-09")
	}
	if list.Source != domain.WatchListDailyScreen || list.BasisDate != "2026-10-08" {
		t.Fatalf("list = %+v", list)
	}
	got := fmt.Sprint(list.Symbols())
	if got != "[HELD1 MANUAL RISE VOL]" && got != "[HELD1 MANUAL VOL RISE]" {
		t.Fatalf("symbols = %s, want held, manual, then the screened ones", got)
	}
	origins := []string{list.Entries[0].Origin, list.Entries[1].Origin, list.Entries[2].Origin}
	if fmt.Sprint(origins) != "[held manual screen]" {
		t.Errorf("origins = %v", origins)
	}
	for _, e := range list.Entries[2:] {
		want := map[string]string{"RISE": "[gain_rate]", "VOL": "[volume_surge]"}[e.Symbol]
		if fmt.Sprint(e.Indicators) != want {
			t.Errorf("%s indicators = %v, want %s", e.Symbol, e.Indicators, want)
		}
	}
	if len(f.notices) != 0 {
		t.Errorf("a normal list must not notify: %v", f.notices)
	}
}

func TestDecide_ListNeverExceedsTheCapAndHeldAlwaysFit(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 19, 0))
	seedScreenable(t, f)
	f.held = heldList{"H1", "H2"}
	f.settings.ManualSymbols = []string{"M1", "M2"}
	f.settings.Screen = onlyIndicators(tachibanasource.ScreenVolume)
	f.max = 3

	f.decide(t)

	list, _ := f.list(t, "2026-10-09")
	if fmt.Sprint(list.Symbols()) != "[H1 H2 M1]" {
		t.Fatalf("symbols = %v, want the held first, then manual up to the cap", list.Symbols())
	}
}

func TestDecide_KeepsAScreenedListOnceDecided(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 19, 0))
	seedScreenable(t, f)
	f.settings.Screen = onlyIndicators(tachibanasource.ScreenGainRate)
	f.decide(t)
	first, _ := f.list(t, "2026-10-09")

	f.held = heldList{"LATE"} // a position opened later must not rewrite the list
	f.clock.Advance(3 * 3600 * 1e9)
	f.decide(t)

	again, _ := f.list(t, "2026-10-09")
	if !again.DecidedAt.Equal(first.DecidedAt) || fmt.Sprint(again.Symbols()) != fmt.Sprint(first.Symbols()) {
		t.Errorf("list changed after being decided: %+v -> %+v", first, again)
	}
}

func TestDecide_WaitsForTheNightlyBatch(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 19, 0))
	f.decide(t) // no run yet
	f.saveRun(t, "2026-10-08", domain.DailyBarRunRunning, 3)
	f.decide(t) // still running
	if _, ok := f.list(t, "2026-10-09"); ok {
		t.Error("a list was decided before the batch finished")
	}
	if len(f.notices) != 0 {
		t.Errorf("notified before the night was over: %v", f.notices)
	}
}

func TestDecide_FailedBatchCarriesOverThePreviousListAndNotifiesOnce(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 19, 0))
	previous := domain.WatchList{ListDate: "2026-10-08", Source: domain.WatchListDailyScreen, DecidedAt: f.clock.Now(),
		Entries: []domain.WatchListEntry{
			{Symbol: "OLDHELD", Origin: domain.WatchOriginHeld},
			{Symbol: "OLD1", Origin: domain.WatchOriginScreen, Indicators: []string{"volume"}},
		}}
	if err := f.lists.Save(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	f.saveRun(t, "2026-10-08", domain.DailyBarRunFailed, 3)
	f.held = heldList{"NEWHELD"}

	f.decide(t) // 19:00: the batch may still retry
	if _, ok := f.list(t, "2026-10-09"); ok {
		t.Fatal("fell back while the night's retries were still possible")
	}

	f.clock.Set(tt.AtJST(2026, 10, 9, 8, 0)) // the night is over
	f.decide(t)
	f.decide(t)

	list, ok := f.list(t, "2026-10-09")
	if !ok || list.Source != domain.WatchListCarriedOver || !list.Fallback() {
		t.Fatalf("list = %+v, %v; want a carried-over list", list, ok)
	}
	if fmt.Sprint(list.Symbols()) != "[NEWHELD OLD1]" {
		t.Errorf("symbols = %v, want today's held symbols then the previous list without its old held slot", list.Symbols())
	}
	if len(f.notices) != 1 {
		t.Errorf("notices = %v, want exactly one", f.notices)
	}
}

func TestDecide_FailedBatchSwitchesToTheFixedListWhenThereIsOne(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 9, 8, 0))
	f.saveRun(t, "2026-10-08", domain.DailyBarRunFailed, 3)
	f.settings.FixedSymbols = []string{"FIX1", "FIX2"}
	f.held = heldList{"HELD"}

	f.decide(t)

	list, ok := f.list(t, "2026-10-09")
	if !ok || list.Source != domain.WatchListFixedFallback || fmt.Sprint(list.Symbols()) != "[HELD FIX1 FIX2]" {
		t.Fatalf("list = %+v, %v", list, ok)
	}
	if len(f.notices) != 1 {
		t.Errorf("notices = %v", f.notices)
	}
}

func TestDecide_NoBatchAtAllFallsBackToHeldOnly(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 9, 8, 0))
	f.held = heldList{"HELD"}
	f.decide(t)
	list, ok := f.list(t, "2026-10-09")
	if !ok || list.Source != domain.WatchListFixedFallback || fmt.Sprint(list.Symbols()) != "[HELD]" {
		t.Fatalf("list = %+v, %v", list, ok)
	}
	if len(f.notices) != 1 {
		t.Errorf("notices = %v", f.notices)
	}
}

func TestDecide_MissingBasisBarsFallBack(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 19, 0))
	f.saveBars(t, "OLD", day(2026, 9, 20), constant(10, 100), nil) // ends 09-29
	f.saveRun(t, "2026-10-08", domain.DailyBarRunSucceeded, 1)
	f.decide(t)
	list, ok := f.list(t, "2026-10-09")
	if !ok || !list.Fallback() {
		t.Fatalf("list = %+v, %v; want a fallback", list, ok)
	}
	if len(f.notices) != 1 {
		t.Errorf("notices = %v", f.notices)
	}
}

func TestDecide_MostSymbolsMissingTheBasisDayFallBack(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 19, 0))
	f.saveBars(t, "A", day(2026, 9, 27), constant(12, 100), nil) // has 10-08
	f.saveRun(t, "2026-10-08", domain.DailyBarRunSucceeded, 10)  // the batch covered ten symbols
	f.decide(t)
	if list, _ := f.list(t, "2026-10-09"); !list.Fallback() {
		t.Fatalf("list = %+v, want a fallback: 1 of 10 symbols has the basis day", list)
	}
}

func TestDecide_ListDateSkipsHolidays(t *testing.T) {
	// 2026-10-09 is Friday and 10-12 (Mon) is スポーツの日: the list decided on
	// Friday night is for Tuesday 10-13.
	f := newFixture(t, tt.AtJST(2026, 10, 9, 19, 0))
	first := day(2026, 9, 28) // 12 days: 09-28 .. 10-09
	f.saveBars(t, "RISE", first, append(constant(11, 100), 110), constant(12, 1000))
	f.saveRun(t, "2026-10-09", domain.DailyBarRunSucceeded, 1)
	f.settings.Screen = onlyIndicators(tachibanasource.ScreenGainRate)
	f.decide(t)
	if _, ok := f.list(t, "2026-10-13"); !ok {
		t.Error("no list for 2026-10-13")
	}
	if _, ok := f.list(t, "2026-10-12"); ok {
		t.Error("a list was made for the holiday 2026-10-12")
	}
}

func TestDecide_FixedModeUsesTheListAsItIsAndSkipsTheBatchOutcome(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 19, 0))
	f.settings.CandidateSource = tachibanasource.TachibanaSourceFixed
	f.settings.FixedSymbols = []string{"A1", "B2", "C3", "D4"}
	f.settings.ManualSymbols = []string{"IGNORED"}
	f.held = heldList{"HELD"}
	f.max = 3

	f.decide(t)

	list, ok := f.list(t, "2026-10-09")
	if !ok || list.Source != domain.WatchListFixed || fmt.Sprint(list.Symbols()) != "[A1 B2 C3]" {
		t.Fatalf("list = %+v, %v; want the fixed list capped at 3", list, ok)
	}
	if list.Fallback() || len(f.notices) != 0 {
		t.Errorf("a fixed list is the plan, not a fallback: %+v %v", list, f.notices)
	}
	// Switching to fixed mid-day also gives today a list; a change applies from the next 立会日.
	if today, ok := f.list(t, tachibanawatch.SessionDate(f.clock.Now())); !ok || today.ListDate != "2026-10-09" {
		t.Errorf("the list in use after the close is the next day's: %+v %v", today, ok)
	}
}

func TestDecide_FixedModeChangedListAppliesFromTheNextSessionOnly(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 10, 0)) // mid-session Thursday
	f.settings.CandidateSource = tachibanasource.TachibanaSourceFixed
	f.settings.FixedSymbols = []string{"A1"}
	f.decide(t)
	if l, ok := f.list(t, "2026-10-08"); !ok || fmt.Sprint(l.Symbols()) != "[A1]" {
		t.Fatalf("today's list = %+v, %v; want the fixed list created at once", l, ok)
	}

	f.settings.FixedSymbols = []string{"B2"}
	f.decide(t)
	today, _ := f.list(t, "2026-10-08")
	next, _ := f.list(t, "2026-10-09")
	if fmt.Sprint(today.Symbols()) != "[A1]" || fmt.Sprint(next.Symbols()) != "[B2]" {
		t.Errorf("today %v next %v; want today untouched (no swap during the day) and tomorrow changed", today.Symbols(), next.Symbols())
	}
}
