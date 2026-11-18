// Package jquantstests holds the tests of the J-Quants read adapter (issue
// #1093): no key / switch off / unsubscribed endpoint means zero requests, mock
// responses land in the research tables, the daily request cap is enforced, the
// purge deletes J-Quants rows only, and a day's research labels are re-labeled
// with the stored minute bars. Everything runs against an httptest server and a
// temporary SQLite database; nothing reaches the real J-Quants.
package jquantstests

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jquantssettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/jquants"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchlabel/jquantslabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	jqservice "github.com/ousiassllc/pitha-trador/internal/service/marketdata/jquants"
)

// testKey is an obviously fake API key; no test output may contain it.
const testKey = "test-key-xxxx"

// jst is the zone of the days the adapter works in.
var jst = time.FixedZone("JST", 9*60*60)

// fixedNow is 2026-11-17 (Tue) 10:00 JST; the day to backfill is 2026-11-16 (Mon).
func fixedNow() time.Time { return time.Date(2026, 11, 17, 10, 0, 0, 0, jst) }

const backfillDay = "2026-11-16"

// openDB opens a fresh migrated database.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return raw
}

// request is one request the fake J-Quants received.
type request struct {
	Path, Query, Key string
}

// fakeServer is an httptest J-Quants: it serves the saved sample responses and
// records every request (the counting transport of the "zero requests" tests).
type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []request
	// status, when not 0, answers every request with it (and a body that must never be echoed).
	status int
	// minuteEmpty answers the minute endpoint with no rows.
	minuteEmpty bool
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve(t)))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeServer) serve(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, request{Path: r.URL.Path, Query: r.URL.RawQuery, Key: r.Header.Get("x-api-key")})
		status, empty := f.status, f.minuteEmpty
		f.mu.Unlock()
		if status != 0 {
			http.Error(w, "body with "+testKey+" must never be echoed", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/equities/master":
			_, _ = w.Write(fixture(t, "master.json"))
		case "/v2/equities/bars/daily":
			if r.URL.Query().Get("pagination_key") == "page-2-key" {
				_, _ = w.Write(fixture(t, "daily_page2.json"))
				return
			}
			_, _ = w.Write(fixture(t, "daily_page1.json"))
		case "/v2/equities/bars/minute":
			if empty {
				_, _ = w.Write([]byte(`{"data": []}`))
				return
			}
			_, _ = w.Write(fixture(t, "minute.json"))
		default:
			http.NotFound(w, r)
		}
	}
}

func (f *fakeServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeServer) countPath(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r.Path == "/v2/equities/"+path {
			n++
		}
	}
	return n
}

func (f *fakeServer) all() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.requests...)
}

// countingTransport counts every request that reaches the network layer and
// fails it: the transport of the clients that must never send.
type countingTransport struct {
	mu sync.Mutex
	n  int
}

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return nil, errors.New("test: no request may be sent")
}

func (c *countingTransport) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// mapStore is the runtime_settings of a test.
type mapStore map[string]string

func (m mapStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := m[key]
	return v, ok, nil
}

// settingsFor returns the stored values of an enabled adapter with the plan flags
// and cap given.
func settingsFor(daily, minute bool, limit int) mapStore {
	flag := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	return mapStore{
		jquantssettings.KeyEnabled: "1", jquantssettings.KeyPlanDaily: flag(daily), jquantssettings.KeyPlanMinute: flag(minute),
		jquantssettings.KeyDailyRequestCap: strconv.Itoa(limit), jquantssettings.KeyBackfillDays: "1",
	}
}

// rig is an adapter over a database and a fake server.
type rig struct {
	db       *sql.DB
	repo     *jquants.Repository
	labels   *jquantslabel.Repository
	server   *fakeServer
	client   *jqservice.Client
	settings mapStore
	now      time.Time
}

// newRig builds the backfiller pieces. The clock is a variable the tests move.
func newRig(t *testing.T, key string, settings mapStore) *rig {
	t.Helper()
	r := &rig{db: openDB(t), server: newFakeServer(t), settings: settings, now: fixedNow()}
	r.repo, r.labels = jquants.NewRepository(r.db), jquantslabel.NewRepository(r.db)
	clock := func() time.Time { return r.now }
	ledger := jqservice.NewCapLedger(r.repo, r.capSource).WithClock(clock)
	r.client = jqservice.NewClient(jqservice.Config{APIKey: key, BaseURL: r.server.URL + "/v2", HTTP: r.server.Client(), Ledger: ledger, MinInterval: -1, Now: clock})
	return r
}

func (r *rig) capSource(ctx context.Context) (int, error) {
	s, err := jquantssettings.Load(ctx, r.settings)
	return s.DailyRequestCap, err
}

// backfiller returns the backfiller of the rig, symbols taken from the database.
func (r *rig) backfiller(maxCalls int) *jqservice.Backfiller {
	load := func(ctx context.Context) (jquantssettings.Settings, error) {
		return jquantssettings.Load(ctx, r.settings)
	}
	b := jqservice.NewBackfiller(r.client, r.repo, r.labels, nil, load, r.client.HasKey()).WithClock(func() time.Time { return r.now })
	if maxCalls > 0 {
		b.WithMaxCalls(maxCalls)
	}
	return b
}

func (r *rig) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func (r *rig) exec(t *testing.T, stmt string, args ...any) {
	t.Helper()
	if _, err := r.db.Exec(stmt, args...); err != nil {
		t.Fatalf("exec %q: %v", stmt, err)
	}
}

// seedSnapshots gives the application's own record a 7203 bar so the minute
// bars of the day are asked for.
func (r *rig) seedSnapshot(t *testing.T, at string, price, bid, ask float64) {
	t.Helper()
	r.exec(t, `INSERT OR IGNORE INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`)
	r.exec(t, `INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, spread_bps, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json)
		VALUES (1, '7203', ?, ?, ?, ?, 20, 1, 1, 1, 1, '{}')`, at, price, bid, ask)
}
