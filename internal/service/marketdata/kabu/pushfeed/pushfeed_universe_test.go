package pushfeed_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu/pushfeed"
)

// An empty active universe still empties kabu's registration list (issue
// #1212), so registrations left by a previous run do not occupy REST slots;
// nothing is registered because kabu rejects an empty /register.
func TestRegisterUniverse_EmptyUniverseUnregistersAllAndRegistersNothing(t *testing.T) {
	f := newFakeKabu(t, marketdata.Board{})
	feed := pushfeed.New(fakeUniverse{}, f.client, f.wsURL(), 1)
	if err := feed.RegisterUniverse(context.Background()); err != nil {
		t.Fatalf("RegisterUniverse: %v", err)
	}
	if got := f.unregisterAllCalls(); got != 1 {
		t.Errorf("UnregisterAll calls = %d, want 1", got)
	}
	if regs := f.registrations(); len(regs) != 0 {
		t.Errorf("registrations = %v, want none", regs)
	}
}

func TestRegisterUniverse_NonEmptyUniverseUnregistersAllThenRegisters(t *testing.T) {
	f := newFakeKabu(t, marketdata.Board{})
	feed := pushfeed.New(fakeUniverse{stocks(2)}, f.client, f.wsURL(), 1)
	if err := feed.RegisterUniverse(context.Background()); err != nil {
		t.Fatalf("RegisterUniverse: %v", err)
	}
	if got := f.unregisterAllCalls(); got != 1 {
		t.Errorf("UnregisterAll calls = %d, want 1", got)
	}
	if regs := f.registrations(); len(regs) != 1 || len(regs[0]) != 2 {
		t.Errorf("registrations = %v, want one call with 2 symbols", regs)
	}
}
