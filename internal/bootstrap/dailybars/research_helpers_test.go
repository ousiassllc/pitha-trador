package dailybars_test

// Fixtures of the research collection tests: the shared fake clock and
// source over a real migrated SQLite database, with the research stores.

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/dailybars"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchbars"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchstatus"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// hookSource runs before before each DailyBars request of the fake source.
type hookSource struct {
	*fakeSource
	before func(symbol string)
}

func (h *hookSource) DailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	if h.before != nil {
		h.before(symbol)
	}
	if err := ctx.Err(); err != nil { // a stopped process ends the request in flight
		return nil, err
	}
	return h.fakeSource.DailyBars(ctx, symbol)
}

type researchFixture struct {
	conn     *sql.DB
	clock    *stepClock
	source   *hookSource
	bars     *market.DailyBarRepository
	nightly  *market.DailyBarRunRepository
	store    *researchbars.Repository
	status   *researchstatus.Repository
	settings tachibanasource.TachibanaSourceSettings

	mu         sync.Mutex
	acquires   int
	acquireErr error
}

// newResearchFixture is a fixture with the switch ON and the run time 18:00.
func newResearchFixture(t *testing.T, now time.Time) *researchFixture {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	clk := &stepClock{now: now}
	src := &hookSource{fakeSource: &fakeSource{clock: clk, history: map[string][]domain.DailyBar{}, errs: map[string]error{}}}
	s := tachibanasource.BuildTachibanaSource(nil)
	s.Research.BarsEnabled = true
	s.Nightly.RunTime, s.Nightly.MaxPerSecond, s.Nightly.Markets = "18:00", 1, []string{"prime", "standard"}
	return &researchFixture{conn: conn, clock: clk, source: src, bars: market.NewDailyBarRepository(conn),
		nightly: market.NewDailyBarRunRepository(conn), store: researchbars.New(conn), status: researchstatus.New(conn), settings: s}
}

func (f *researchFixture) addSymbol(symbol string, bars ...domain.DailyBar) {
	f.source.targets = append(f.source.targets, domain.DailyBarTarget{Symbol: symbol, Market: "prime"})
	f.source.history[symbol] = bars
}

// newResearch is a fresh Research (as after a restart) over the same stores.
func (f *researchFixture) newResearch() *dailybars.Research {
	return dailybars.NewResearch(dailybars.ResearchConfig{
		Settings: func(context.Context) (tachibanasource.TachibanaSourceSettings, error) { return f.settings, nil },
		Acquire: func(context.Context) (dailybars.Source, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.acquires++
			if f.acquireErr != nil {
				return nil, f.acquireErr
			}
			return f.source, nil
		},
		Bars: f.bars, Store: f.store, Status: f.status, Clock: f.clock,
	})
}

func (f *researchFixture) runDue(t *testing.T, r *dailybars.Research) {
	t.Helper()
	f.runDueCtx(t, context.Background(), r)
}

func (f *researchFixture) runDueCtx(t *testing.T, ctx context.Context, r *dailybars.Research) {
	t.Helper()
	if err := r.RunDue(ctx); err != nil {
		t.Fatalf("RunDue: %v", err)
	}
}

func (f *researchFixture) acquireCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acquires
}

// state is the recorded state; "" when nothing was recorded (a collection that
// is off and never was on writes no status row).
func (f *researchFixture) state(t *testing.T) string {
	t.Helper()
	s, _, err := f.status.Get(context.Background(), researchstatus.ComponentBars)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return s.State
}

func (f *researchFixture) run(t *testing.T, night string) domain.DailyBarRun {
	t.Helper()
	run, ok, err := f.store.Get(context.Background(), night)
	if err != nil || !ok {
		t.Fatalf("research run %s: ok=%v err=%v", night, ok, err)
	}
	return run
}

// stored is every stored bar of the symbols, as "symbol/date/close" strings.
func (f *researchFixture) stored(t *testing.T, symbols ...string) []string {
	t.Helper()
	var out []string
	for _, s := range symbols {
		bars, err := f.bars.ListBySymbol(context.Background(), s)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range bars {
			out = append(out, s+"/"+b.TradeDate+"/"+strconv.Itoa(int(b.Close)))
		}
	}
	return out
}

type progressRow struct {
	first           string
	saved, failures int
}

func (f *researchFixture) progressRow(t *testing.T, symbol string) progressRow {
	t.Helper()
	var (
		row   progressRow
		first sql.NullString
	)
	err := f.conn.QueryRow(`SELECT first_ok_night, bars_saved, failures FROM research_daily_bar_progress WHERE symbol = ?`, symbol).
		Scan(&first, &row.saved, &row.failures)
	if err != nil {
		t.Fatalf("progress of %s: %v", symbol, err)
	}
	row.first = first.String
	return row
}
