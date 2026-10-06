package rankingwatch_test

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingwatch"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// fakeSource answers every ranking type with symbols, or fails/panics.
type fakeSource struct {
	mu      sync.Mutex
	symbols []string
	err     error
	panics  bool
	calls   int
}

func (f *fakeSource) RankingSymbols(_ context.Context, _ int, _ string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.panics {
		panic("ranking boom")
	}
	return f.symbols, f.err
}

func (f *fakeSource) set(symbols []string, err error, panics bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.symbols, f.err, f.panics = symbols, err, panics
}

type fakeHeld struct {
	symbols []string
	err     error
}

func (f *fakeHeld) HeldSymbols(context.Context) ([]string, error) { return f.symbols, f.err }

type fakeUniverse []domain.Instrument

func (u fakeUniverse) ListActiveByKind(context.Context, string) ([]domain.Instrument, error) {
	return u, nil
}

type fakeRegistrar struct {
	sets   [][]string
	err    error
	panics bool
}

func (f *fakeRegistrar) SetWatch(_ context.Context, symbols []string) error {
	if f.panics {
		panic("register boom")
	}
	if f.err != nil {
		return f.err
	}
	f.sets = append(f.sets, slices.Clone(symbols))
	return nil
}

type fakeIngester struct{ enqueued [][]string }

func (f *fakeIngester) EnqueueMarketData(_ context.Context, instruments []domain.Instrument, _ time.Time) (int, error) {
	syms := make([]string, len(instruments))
	for i, inst := range instruments {
		syms[i] = inst.Symbol
	}
	f.enqueued = append(f.enqueued, syms)
	return len(syms), nil
}

type rig struct {
	w    *rankingwatch.Watcher
	src  *fakeSource
	held *fakeHeld
	reg  *fakeRegistrar
	ing  *fakeIngester
	list *rankingwatch.Watchlist
	now  time.Time
	logs *bytes.Buffer
}

func newRig(t *testing.T, universe ...string) *rig {
	t.Helper()
	insts := make(fakeUniverse, len(universe))
	for i, sym := range universe {
		insts[i] = domain.Instrument{ID: int64(i + 1), Symbol: sym, Kind: domain.InstrumentKindStock, IsActive: true}
	}
	r := &rig{
		src: &fakeSource{}, held: &fakeHeld{}, reg: &fakeRegistrar{}, ing: &fakeIngester{},
		list: &rankingwatch.Watchlist{}, now: t0, logs: &bytes.Buffer{},
	}
	r.w = &rankingwatch.Watcher{
		Source: r.src, Held: r.held, Universe: insts, Registrar: r.reg, Ingester: r.ing, List: r.list,
		Types: []int{1, 2}, Now: func() time.Time { return r.now },
	}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(r.logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return r
}

func (r *rig) cycle() {
	r.w.Cycle(context.Background())
	r.now = r.now.Add(time.Minute)
}

func (r *rig) lastEnqueued() []string {
	if len(r.ing.enqueued) == 0 {
		return nil
	}
	return r.ing.enqueued[len(r.ing.enqueued)-1]
}
