package tachibanawatch_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingwatch"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/tachibanawatch"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

type fakeSource struct {
	symbols []string
	err     error
}

func (f *fakeSource) Candidates(context.Context) ([]string, error) { return f.symbols, f.err }

type fakeUniverse []domain.Instrument

func (u fakeUniverse) ListActiveByKind(_ context.Context, kind string) ([]domain.Instrument, error) {
	var out []domain.Instrument
	for _, inst := range u {
		if inst.Kind == kind {
			out = append(out, inst)
		}
	}
	return out, nil
}

type fakeRegistrar struct{ calls [][]string }

func (r *fakeRegistrar) SetWatch(_ context.Context, symbols []string) error {
	r.calls = append(r.calls, slices.Clone(symbols))
	return nil
}

type fakeIngester struct{ batches [][]string }

func (i *fakeIngester) EnqueueMarketData(_ context.Context, instruments []domain.Instrument, _ time.Time) (int, error) {
	var symbols []string
	for _, inst := range instruments {
		symbols = append(symbols, inst.Symbol)
	}
	i.batches = append(i.batches, symbols)
	return len(instruments), nil
}

type monitorRig struct {
	clock     *tt.ManualClock
	source    *fakeSource
	held      heldList
	registrar *fakeRegistrar
	ingester  *fakeIngester
	list      *rankingwatch.Watchlist
	monitor   *tachibanawatch.Monitor
}

func stock(symbol string) domain.Instrument {
	return domain.Instrument{ID: int64(len(symbol)) + 100, Symbol: symbol, Kind: domain.InstrumentKindStock}
}

func newMonitorRig(now time.Time, max int, universe ...string) *monitorRig {
	r := &monitorRig{
		clock: tt.NewManualClock(now), source: &fakeSource{},
		registrar: &fakeRegistrar{}, ingester: &fakeIngester{}, list: &rankingwatch.Watchlist{},
	}
	u := fakeUniverse{{ID: 1, Symbol: "TOPIX", Kind: domain.InstrumentKindMarketIndex}}
	for _, s := range universe {
		u = append(u, stock(s))
	}
	r.monitor = tachibanawatch.NewMonitor(tachibanawatch.MonitorConfig{
		Source: r.source, Held: &r.held, Universe: u, Registrar: r.registrar, Ingester: r.ingester,
		List: r.list, Max: max, Clock: r.clock,
	})
	return r
}

func (r *monitorRig) cycle(t *testing.T) {
	t.Helper()
	if err := r.monitor.Cycle(context.Background()); err != nil {
		t.Fatalf("Cycle: %v", err)
	}
}

// The saved list becomes the EVENT subscription, the market-data jobs (with
// the market index rows) and the Fast Screener's universe; an unchanged list
// is not registered again (no extra EVENT reconnect).
func TestMonitorRegistersIngestsAndSharesTheSavedList(t *testing.T) {
	r := newMonitorRig(tt.AtJST(2026, 10, 8, 9, 5), 120, "7203", "6758", "9984")
	r.source.symbols = []string{"7203", "6758", "0000"} // 0000 is not in the universe
	r.cycle(t)
	r.cycle(t)

	if len(r.registrar.calls) != 1 || !slices.Equal(r.registrar.calls[0], []string{"7203", "6758"}) {
		t.Fatalf("SetWatch calls = %v, want one with 7203, 6758", r.registrar.calls)
	}
	if len(r.ingester.batches) != 2 || !slices.Equal(r.ingester.batches[0], []string{"7203", "6758", "TOPIX"}) {
		t.Errorf("ingested = %v, want the watched stocks and the index each cycle", r.ingester.batches)
	}
	if !r.list.Contains("7203") || !r.list.Contains("6758") || r.list.Contains("9984") || r.list.Len() != 2 {
		t.Errorf("Fast Screener list has %d symbols, want exactly 7203 and 6758", r.list.Len())
	}
}

// A symbol that becomes held after the list was decided is added in one
// SetWatch (one batched reconnect); the list itself is never swapped.
func TestMonitorAddsNewlyHeldSymbolsInOneBatchAndKeepsTheList(t *testing.T) {
	r := newMonitorRig(tt.AtJST(2026, 10, 8, 10, 0), 120, "7203", "6758", "9984", "8306")
	r.source.symbols = []string{"7203", "6758"}
	r.cycle(t)
	r.held = heldList{"9984", "8306"}
	r.clock.Advance(time.Minute)
	r.cycle(t)
	r.clock.Advance(time.Minute)
	r.cycle(t)

	if len(r.registrar.calls) != 2 {
		t.Fatalf("SetWatch calls = %v, want the initial one and exactly one batched addition", r.registrar.calls)
	}
	if got := r.registrar.calls[1]; !slices.Equal(got, []string{"9984", "8306", "7203", "6758"}) {
		t.Errorf("second SetWatch = %v, want held first then the list", got)
	}
}

// Held symbols keep their slots when the list is full: the cap is never
// exceeded and the list's tail gives way.
func TestMonitorCapsTheWatchAndHeldSymbolsKeepTheirSlots(t *testing.T) {
	r := newMonitorRig(tt.AtJST(2026, 10, 8, 10, 0), 3, "A1", "A2", "A3", "H1")
	r.source.symbols = []string{"A1", "A2", "A3"}
	r.held = heldList{"H1"}
	r.cycle(t)

	if got := r.registrar.calls[0]; !slices.Equal(got, []string{"H1", "A1", "A2"}) {
		t.Errorf("watch = %v, want H1, A1, A2 (cap 3)", got)
	}
}

// No SetWatch and no market-data job outside the 立会日's window, so the
// evening's list for the next day does not cost an extra EVENT reconnect and
// nothing is fetched outside the session's day.
func TestMonitorDoesNothingOutsideTheWatchWindow(t *testing.T) {
	for name, at := range map[string]time.Time{
		"after the close":             tt.AtJST(2026, 10, 8, 15, 30),
		"evening":                     tt.AtJST(2026, 10, 8, 19, 0),
		"before the broker's 閉局 ends": tt.AtJST(2026, 10, 8, 3, 29),
		"weekend":                     tt.AtJST(2026, 10, 10, 10, 0),
	} {
		t.Run(name, func(t *testing.T) {
			r := newMonitorRig(at, 120, "7203")
			r.source.symbols = []string{"7203"}
			r.cycle(t)
			if len(r.registrar.calls) != 0 || len(r.ingester.batches) != 0 || r.list.Len() != 0 {
				t.Errorf("registered %v, ingested %v, list %d; want nothing", r.registrar.calls, r.ingester.batches, r.list.Len())
			}
		})
	}
}

// Before the first list (fresh install) only the held symbols are watched; a
// failed read changes nothing and is reported.
func TestMonitorWithoutAListWatchesHeldOnlyAndAReadFailureChangesNothing(t *testing.T) {
	r := newMonitorRig(tt.AtJST(2026, 10, 8, 10, 0), 120, "7203", "6758")
	r.source.err = tachibanawatch.ErrNoList
	r.held = heldList{"6758"}
	r.cycle(t)
	if got := r.registrar.calls; len(got) != 1 || !slices.Equal(got[0], []string{"6758"}) {
		t.Fatalf("SetWatch calls = %v, want the held symbol only", got)
	}

	r.source.err = fmt.Errorf("db: %w", errors.New("locked"))
	if err := r.monitor.Cycle(context.Background()); err == nil {
		t.Fatal("Cycle with a failed list read = nil, want the error")
	}
	if len(r.registrar.calls) != 1 {
		t.Errorf("SetWatch calls = %v, want unchanged after a failed read", r.registrar.calls)
	}
}
