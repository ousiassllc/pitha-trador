package pushfeed_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/pushfeed"
)

func symbolsOf(regs []marketdata.RegisterSymbol) []string {
	out := make([]string, len(regs))
	for i, r := range regs {
		out[i] = r.Symbol
	}
	return out
}

// SetWatch registers the watch list as is and unregisters the symbols dropped
// since the previous call, so rotated-out symbols free their slots.
func TestSetWatch_RegistersListAndUnregistersDroppedSymbols(t *testing.T) {
	f := newFakeKabu(t, marketdata.Board{})
	feed := pushfeed.New(fakeUniverse{}, f.client, f.wsURL(), 1)
	ctx := context.Background()

	if err := feed.SetWatch(ctx, []string{"1000", "1001"}); err != nil {
		t.Fatalf("SetWatch: %v", err)
	}
	if err := feed.SetWatch(ctx, []string{"1001", "1002"}); err != nil {
		t.Fatalf("SetWatch: %v", err)
	}
	regs := f.registrations()
	if len(regs) != 2 || !slices.Equal(symbolsOf(regs[1]), []string{"1001", "1002"}) {
		t.Fatalf("registrations = %v, want the new list registered as is", regs)
	}
	f.mu.Lock()
	unregs := f.unregs
	f.mu.Unlock()
	if len(unregs) != 1 || !slices.Equal(symbolsOf(unregs[0]), []string{"1000"}) {
		t.Errorf("unregistrations = %v, want only the dropped 1000", unregs)
	}
}

// An empty watch list (empty or failed ranking, nothing held) unregisters the
// dropped symbols and registers nothing; kabu rejects an empty /register.
func TestSetWatch_EmptyListOnlyUnregisters(t *testing.T) {
	f := newFakeKabu(t, marketdata.Board{})
	feed := pushfeed.New(fakeUniverse{}, f.client, f.wsURL(), 1)
	ctx := context.Background()
	if err := feed.SetWatch(ctx, []string{"1000"}); err != nil {
		t.Fatalf("SetWatch: %v", err)
	}
	if err := feed.SetWatch(ctx, nil); err != nil {
		t.Fatalf("SetWatch(empty): %v", err)
	}
	if regs := f.registrations(); len(regs) != 1 {
		t.Errorf("registrations = %d, want 1 (nothing registered for an empty list)", len(regs))
	}
	f.mu.Lock()
	unregs := f.unregs
	f.mu.Unlock()
	if len(unregs) != 1 || !slices.Equal(symbolsOf(unregs[0]), []string{"1000"}) {
		t.Errorf("unregistrations = %v, want 1000", unregs)
	}
}

// With UseWatchlist, Run registers the remembered watch list (not the
// universe) after connecting.
func TestRun_UseWatchlistRegistersWatchListNotUniverse(t *testing.T) {
	f := newFakeKabu(t, marketdata.Board{})
	feed := pushfeed.New(fakeUniverse{stocks(10)}, f.client, f.wsURL(), 1)
	feed.UseWatchlist()
	_ = feed.SetWatch(context.Background(), []string{"1003"})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { feed.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	deadline := time.Now().Add(5 * time.Second)
	for len(f.registrations()) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("Run never registered; registrations = %v", f.registrations())
		}
		time.Sleep(10 * time.Millisecond)
	}
	regs := f.registrations()
	if got := symbolsOf(regs[len(regs)-1]); !slices.Equal(got, []string{"1003"}) {
		t.Errorf("Run registered %v, want the watch list [1003]", got)
	}
}
