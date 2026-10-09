package tachibanawatch_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/tachibanawatch"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

type heldList []string

func (h heldList) HeldSymbols(context.Context) ([]string, error) { return h, nil }

type fixture struct {
	clock    *tt.ManualClock
	bars     *market.DailyBarRepository
	runs     *market.DailyBarRunRepository
	lists    *market.WatchListRepository
	settings tachibanasource.TachibanaSourceSettings
	held     heldList
	max      int
	notices  []string
}

func newFixture(t *testing.T, now time.Time) *fixture {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &fixture{
		clock: tt.NewManualClock(now),
		bars:  market.NewDailyBarRepository(conn), runs: market.NewDailyBarRunRepository(conn), lists: market.NewWatchListRepository(conn),
		settings: tachibanasource.BuildTachibanaSource(nil),
		max:      120,
	}
}

func (f *fixture) decider() *tachibanawatch.Decider {
	return tachibanawatch.New(tachibanawatch.Config{
		Settings: func(context.Context) (tachibanasource.TachibanaSourceSettings, error) { return f.settings, nil },
		Bars:     f.bars, Runs: f.runs, Lists: f.lists, Held: f.held, Max: f.max, Clock: f.clock,
		Notify: func(m string) { f.notices = append(f.notices, m) },
	})
}

func (f *fixture) decide(t *testing.T) {
	t.Helper()
	if err := f.decider().Decide(context.Background()); err != nil {
		t.Fatalf("Decide: %v", err)
	}
}

// saveBars stores one symbol's bars: closes[i] is the close of the i-th day
// ending at lastDate (consecutive calendar days are fine for the tests),
// volumes[i] its volume.
func (f *fixture) saveBars(t *testing.T, symbol string, firstDate time.Time, closes, volumes []float64) {
	t.Helper()
	var bars []domain.DailyBar
	for i, c := range closes {
		v := 1000.0
		if i < len(volumes) {
			v = volumes[i]
		}
		bars = append(bars, domain.DailyBar{
			Symbol: symbol, TradeDate: firstDate.AddDate(0, 0, i).Format("2006-01-02"),
			Open: c, High: c * 1.01, Low: c * 0.99, Close: c, Volume: v,
			AdjOpen: c, AdjHigh: c * 1.01, AdjLow: c * 0.99, AdjClose: c, AdjVolume: v,
		})
	}
	if _, err := f.bars.Save(context.Background(), symbol, bars, false); err != nil {
		t.Fatalf("Save bars %s: %v", symbol, err)
	}
}

func (f *fixture) saveRun(t *testing.T, night, status string, symbols int) {
	t.Helper()
	run := domain.DailyBarRun{RunDate: night, Status: status, StartedAt: f.clock.Now(), Symbols: symbols}
	if err := f.runs.Save(context.Background(), run); err != nil {
		t.Fatalf("Save run: %v", err)
	}
}

func (f *fixture) list(t *testing.T, date string) (domain.WatchList, bool) {
	t.Helper()
	l, ok, err := f.lists.Get(context.Background(), date)
	if err != nil {
		t.Fatalf("Get %s: %v", date, err)
	}
	return l, ok
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
