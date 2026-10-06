package rankingwatch_test

import (
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func (r *rig) addIndex(id int64, symbol, kind, sector string) {
	inst := domain.Instrument{ID: id, Symbol: symbol, Kind: kind, IsActive: true}
	if sector != "" {
		inst.Sector = &sector
	}
	r.w.Universe = append(r.w.Universe.(fakeUniverse), inst)
}

func (r *rig) setSector(symbol, sector string) {
	u := r.w.Universe.(fakeUniverse)
	for i := range u {
		if u[i].Symbol == symbol {
			u[i].Sector = &sector
		}
	}
}

// The market context (market_return_*, sector_return_5m, FR-FE-4) comes from
// index rows the watch list does not contain, so every cycle ingests them
// too (issue #670): all market indexes, and the sector indexes of the
// watched stocks' sectors only.
func TestWatcher_EnqueuesTheMarketContextIndexRows(t *testing.T) {
	r := newRig(t, "7203", "6758", "9984")
	r.setSector("7203", "auto")
	r.setSector("9984", "telecom")
	r.addIndex(101, "101", domain.InstrumentKindMarketIndex, "")
	r.addIndex(102, "102", domain.InstrumentKindMarketIndex, "")
	r.addIndex(201, "201", domain.InstrumentKindSectorIndex, "auto")
	r.addIndex(202, "202", domain.InstrumentKindSectorIndex, "telecom") // no watched stock in it
	r.addIndex(203, "203", domain.InstrumentKindSectorIndex, "")        // invalid: no sector
	r.src.set([]string{"7203", "6758"}, nil, false)

	r.cycle()

	want := []string{"101", "102", "201", "6758", "7203"}
	if got := sorted(r.lastEnqueued()); !slices.Equal(got, want) {
		t.Errorf("enqueued = %v, want %v", got, want)
	}
	if len(r.reg.sets) != 1 || !slices.Equal(sorted(r.reg.sets[0]), []string{"6758", "7203"}) {
		t.Errorf("registered = %v, want the stocks only (index rows are REST-polled, never PUSH)", r.reg.sets)
	}
	if r.list.Contains("101") || r.list.Contains("201") {
		t.Error("index rows must not become screening candidates")
	}
}

func TestWatcher_IndexRowsAreEnqueuedEvenWithAnEmptyWatchList(t *testing.T) {
	r := newRig(t, "7203")
	r.addIndex(101, "101", domain.InstrumentKindMarketIndex, "")
	r.src.set(nil, nil, false)
	r.cycle()
	if got := r.lastEnqueued(); !slices.Equal(got, []string{"101"}) {
		t.Errorf("enqueued = %v, want the market index even with no watched stock", got)
	}
}

func TestWatcher_IndexListingFailureStillIngestsTheWatchList(t *testing.T) {
	r := newRig(t, "7203")
	r.w.Universe = failingKind{fakeUniverse: r.w.Universe.(fakeUniverse), kind: domain.InstrumentKindMarketIndex}
	r.src.set([]string{"7203"}, nil, false)
	r.cycle()
	if got := r.lastEnqueued(); !slices.Equal(got, []string{"7203"}) {
		t.Errorf("enqueued = %v, want the watched stock despite the index listing failure", got)
	}
}
