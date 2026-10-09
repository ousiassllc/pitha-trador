package dailybars_test

// Shared fixtures of the batch tests: a fake clock, a fake broker source and
// a fixture over a real migrated SQLite database.

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/dailybars"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

// stepClock is a clock whose timers fire at once and advance it by their
// duration, so waits cost nothing and Now shows how long the batch "took".
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

func (c *stepClock) NewTimer(d time.Duration) tachibana.Timer {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	c.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- now
	return firedTimer{ch}
}

type firedTimer struct{ ch chan time.Time }

func (t firedTimer) C() <-chan time.Time { return t.ch }
func (firedTimer) Stop() bool            { return false }

// fakeSource serves a fixed master and per-symbol histories, recording every
// request with the clock reading at that moment.
type fakeSource struct {
	clock     *stepClock
	targets   []domain.DailyBarTarget
	targetErr error
	history   map[string][]domain.DailyBar
	errs      map[string]error

	mu    sync.Mutex
	calls []string
	times []time.Time
}

func (f *fakeSource) DailyBarTargets(context.Context) ([]domain.DailyBarTarget, error) {
	return f.targets, f.targetErr
}

func (f *fakeSource) DailyBars(_ context.Context, symbol string) ([]domain.DailyBar, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, symbol)
	f.times = append(f.times, f.clock.Now())
	if err := f.errs[symbol]; err != nil {
		return nil, err
	}
	return f.history[symbol], nil
}

func bar(symbol, date string, closing float64) domain.DailyBar {
	return domain.DailyBar{Symbol: symbol, TradeDate: date, Open: closing, High: closing, Low: closing, Close: closing, Volume: 1000,
		AdjOpen: closing, AdjHigh: closing, AdjLow: closing, AdjClose: closing, AdjVolume: 1000}
}

type fixture struct {
	clock  *stepClock
	source *fakeSource
	bars   *market.DailyBarRepository
	runs   *market.DailyBarRunRepository
	cfg    dailybars.Config
}

func newFixture(t *testing.T, now time.Time) *fixture {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	clk := &stepClock{now: now}
	src := &fakeSource{clock: clk, history: map[string][]domain.DailyBar{}, errs: map[string]error{}}
	f := &fixture{clock: clk, source: src, bars: market.NewDailyBarRepository(conn), runs: market.NewDailyBarRunRepository(conn)}
	f.cfg = dailybars.Config{
		Source: src, Bars: f.bars, Runs: f.runs, Clock: clk,
		Settings: tachibanasource.TachibanaNightlySettings{
			RunTime: "18:00", MaxPerSecond: 1, Markets: []string{"prime", "standard"},
		},
	}
	return f
}

func (f *fixture) addSymbol(symbol, mkt string, prevClose float64, bars ...domain.DailyBar) {
	f.source.targets = append(f.source.targets, domain.DailyBarTarget{Symbol: symbol, Market: mkt, PrevClose: prevClose})
	f.source.history[symbol] = bars
}

func (f *fixture) run(t *testing.T) {
	t.Helper()
	if err := dailybars.New(f.cfg).RunDue(context.Background()); err != nil {
		t.Fatalf("RunDue: %v", err)
	}
}

func (f *fixture) storedRun(t *testing.T, night string) domain.DailyBarRun {
	t.Helper()
	run, ok, err := f.runs.Get(context.Background(), night)
	if err != nil || !ok {
		t.Fatalf("run %s: ok=%v err=%v", night, ok, err)
	}
	return run
}
