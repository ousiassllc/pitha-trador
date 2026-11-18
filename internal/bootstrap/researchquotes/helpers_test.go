package researchquotes

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchquote"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchstatus"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// thursday is a trading day: 2026-10-08 at hh:mm:ss JST.
func thursday(hh, mm, ss int) time.Time { return tt.AtJST(2026, 10, 8, hh, mm, ss) }

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// settings is a switched-on research setting with the given extras.
func settings(extras ...string) tachibanasource.TachibanaSourceSettings {
	s := tachibanasource.BuildTachibanaSource(nil)
	s.Research.QuotesEnabled = true
	s.Research.QuotesExtraSymbols = extras
	return s
}

// fakeFeed is a Feed with scripted values; it records every call.
type fakeFeed struct {
	mu          sync.Mutex
	values      map[string]broker.Quote
	updated     map[string]time.Time
	rest        map[string]bool
	latest      map[string]broker.Quote // what a REST refill answers per symbol
	latestErr   error                   // the answer for a symbol not in latest
	watches     [][]string
	latestCalls []string
}

func newFakeFeed() *fakeFeed {
	return &fakeFeed{values: map[string]broker.Quote{}, updated: map[string]time.Time{}, rest: map[string]bool{}, latest: map[string]broker.Quote{}}
}

func (f *fakeFeed) put(symbol string, price float64, at time.Time, viaREST bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[symbol] = broker.Quote{Symbol: symbol, Price: price}
	f.updated[symbol], f.rest[symbol] = at, viaREST
}

func (f *fakeFeed) SetWatch(_ context.Context, symbols []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watches = append(f.watches, append([]string(nil), symbols...))
	return nil
}

func (f *fakeFeed) Peek(symbol string) (broker.Quote, time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q, ok := f.values[symbol]
	return q, f.updated[symbol], ok
}

func (f *fakeFeed) ViaREST(symbol string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rest[symbol]
}

func (f *fakeFeed) Latest(_ context.Context, symbol string) (broker.Quote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.latestCalls = append(f.latestCalls, symbol)
	if q, ok := f.latest[symbol]; ok {
		return q, nil
	}
	return broker.Quote{}, f.latestErr
}

func (f *fakeFeed) ConnectionsToday() int { return 1 }

// rig is a Recorder over a real database, a switched settings source and a
// counting Start.
type rig struct {
	t      *testing.T
	db     *sql.DB
	feed   *fakeFeed
	rec    *Recorder
	set    tachibanasource.TachibanaSourceSettings
	kabu   []string
	starts int
	startE error
	status *researchstatus.Repository
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, db: openDB(t), feed: newFakeFeed(), set: settings()}
	r.status = researchstatus.New(r.db)
	r.rec = New(Config{
		Settings:    func(context.Context) (tachibanasource.TachibanaSourceSettings, error) { return r.set, nil },
		Start:       func(context.Context) (Feed, error) { r.starts++; return r.feed, r.startE },
		KabuSymbols: func() []string { return r.kabu },
		Store:       researchquote.New(r.db),
		Status:      r.status,
	})
	return r
}

func (r *rig) tick(now time.Time) {
	r.t.Helper()
	if err := r.rec.Tick(context.Background(), now); err != nil {
		r.t.Fatalf("Tick(%v): %v", now, err)
	}
}

func (r *rig) state() (string, string) {
	r.t.Helper()
	s, _, err := r.status.Get(context.Background(), researchstatus.ComponentQuotes)
	if err != nil {
		r.t.Fatalf("status = %+v, err=%v", s, err)
	}
	return s.State, s.Detail
}

func (r *rig) rows() int {
	r.t.Helper()
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM research_quote_samples`).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

func symbolsN(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%03d", prefix, i)
	}
	return out
}
