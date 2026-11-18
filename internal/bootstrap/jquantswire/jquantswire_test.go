package jquantswire_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/jquantswire"
	"github.com/ousiassllc/pitha-trador/internal/config/jquantssettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	jqservice "github.com/ousiassllc/pitha-trador/internal/service/marketdata/jquants"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

const testKey = "test-key-xxxx"

var jst = time.FixedZone("JST", 9*60*60)

func fixedNow() time.Time { return time.Date(2026, 11, 17, 10, 0, 0, 0, jst) }

// failTransport counts the requests that reach the network layer and fails them.
type failTransport struct{ n atomic.Int64 }

func (f *failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.n.Add(1)
	return nil, errors.New("test: no request may be sent")
}

type env struct {
	db       *sql.DB
	settings *system.RuntimeSettingsRepository
}

func newEnv(t *testing.T) env {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return env{db: conn, settings: system.NewRuntimeSettingsRepository(conn)}
}

func (e env) set(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if err := e.settings.Set(context.Background(), k, v, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

func (e env) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e env) job(key string, cfg jqservice.Config) *jquantswire.Job {
	return jquantswire.NewWithOptions(e.db, e.settings, key, jquantswire.Options{Client: cfg, Now: fixedNow})
}

var allTables = []string{"research_jquants_symbols", "research_jquants_daily_bars", "research_jquants_minute_bars", "research_jquants_coverage",
	"research_jquants_fetch_log", "runtime_settings", "research_horizon_labels", "market_snapshots"}

func (e env) snapshot(t *testing.T) string {
	t.Helper()
	var parts []string
	for _, table := range allTables {
		parts = append(parts, fmt.Sprint(table, "=", e.count(t, table)))
	}
	return strings.Join(parts, " ")
}

// The task is OFF by default and inert: with nothing stored, a stored 0, no key, or
// no endpoint subscribed, Run returns ErrSkipped, sends no request and writes no row
// (not even the runner's last-success date).
func TestJob_IsInertByDefaultAndWithoutKeyOrPlan(t *testing.T) {
	cases := map[string]struct {
		key    string
		stored map[string]string
	}{
		"nothing stored":  {testKey, nil},
		"stored 0":        {testKey, map[string]string{jquantssettings.KeyEnabled: "0", jquantssettings.KeyPlanDaily: "1"}},
		"on, no key":      {"", map[string]string{jquantssettings.KeyEnabled: "1", jquantssettings.KeyPlanDaily: "1", jquantssettings.KeyPlanMinute: "1"}},
		"on, no plan set": {testKey, map[string]string{jquantssettings.KeyEnabled: "1"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.set(t, c.stored)
			transport := &failTransport{}
			job := e.job(c.key, jqservice.Config{HTTP: &http.Client{Transport: transport}})
			before := e.snapshot(t)
			if err := job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
				t.Fatalf("Run = %v, want maintenance.ErrSkipped", err)
			}
			// Through the real runner too: a skipped task is not recorded as done.
			runner := maintenance.NewRunner(e.settings, maintenance.WithClock(fixedNow))
			runner.Add(job.Task())
			runner.CatchUp(context.Background())
			if n := transport.n.Load(); n != 0 {
				t.Fatalf("%d requests reached the transport, want 0", n)
			}
			if after := e.snapshot(t); after != before {
				t.Fatalf("tables changed while inert: %s -> %s", before, after)
			}
			if _, ok, _ := e.settings.Get(context.Background(), "system.maintenance."+jquantswire.TaskName+".last_success_date"); ok {
				t.Error("the runner stored a last-success date for a task that did nothing")
			}
		})
	}
}

// fakeJQuants serves one daily row and counts requests; status, when set, fails them.
func fakeJQuants(t *testing.T, status int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if status != 0 {
			http.Error(w, "body with "+testKey, status)
			return
		}
		switch r.URL.Path {
		case "/v2/equities/master":
			_, _ = w.Write([]byte(`{"data":[{"Date":"2026-11-17","Code":"72030","CoName":"x"}]}`))
		case "/v2/equities/bars/daily":
			_, _ = w.Write([]byte(`{"data":[{"Date":"2026-11-16","Code":"72030","O":1.0,"H":2.0,"L":1.0,"C":2.0,"Vo":10.0,"Va":20.0}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func enabled(daily, minute, limit string) map[string]string {
	return map[string]string{jquantssettings.KeyEnabled: "1", jquantssettings.KeyPlanDaily: daily, jquantssettings.KeyPlanMinute: minute,
		jquantssettings.KeyDailyRequestCap: limit, jquantssettings.KeyBackfillDays: "1"}
}

// Switched on with a key and a plan, one Run backfills yesterday's daily bars from the mock.
func TestJob_BackfillsWhenOn(t *testing.T) {
	e := newEnv(t)
	e.set(t, enabled("1", "0", "100"))
	srv, n := fakeJQuants(t, 0)
	job := e.job(testKey, jqservice.Config{BaseURL: srv.URL + "/v2", HTTP: srv.Client(), MinInterval: -1})
	if err := job.Run(context.Background()); err != nil && !errors.Is(err, maintenance.ErrMore) {
		t.Fatalf("Run = %v", err)
	}
	if got := e.count(t, "research_jquants_daily_bars"); got != 1 || n.Load() != 2 {
		t.Fatalf("daily bars %d after %d requests, want 1 after 2 (master + daily)", got, n.Load())
	}
}

// At the day's request cap the task ends with nil (the rest continues tomorrow), not
// ErrMore: the runner would otherwise call it again at once, with no progress.
func TestJob_AtTheCapReturnsNilNotMore(t *testing.T) {
	e := newEnv(t)
	e.set(t, enabled("1", "0", "1"))
	srv, n := fakeJQuants(t, 0)
	job := e.job(testKey, jqservice.Config{BaseURL: srv.URL + "/v2", HTTP: srv.Client(), MinInterval: -1})
	if err := job.Run(context.Background()); err != nil {
		t.Fatalf("Run = %v, want nil at the cap", err)
	}
	if n.Load() != 1 {
		t.Fatalf("%d requests with a cap of 1", n.Load())
	}
	if err := job.Run(context.Background()); err != nil || n.Load() != 1 {
		t.Fatalf("second Run = %v after %d requests, want nil and no new request", err, n.Load())
	}
}

// A refused key is returned (the runner notifies and retries later) without the key or the body.
func TestJob_RefusedKeyIsAnErrorWithoutSecrets(t *testing.T) {
	e := newEnv(t)
	e.set(t, enabled("1", "0", "100"))
	srv, _ := fakeJQuants(t, http.StatusUnauthorized)
	err := e.job(testKey, jqservice.Config{BaseURL: srv.URL + "/v2", HTTP: srv.Client(), MinInterval: -1}).Run(context.Background())
	if !errors.Is(err, jqservice.ErrAuth) {
		t.Fatalf("Run = %v, want ErrAuth", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("the error contains the key: %v", err)
	}
}

// The purge request is served even with the switch off and no key, makes no request,
// deletes J-Quants rows only (the application's own rows stay) and clears the request.
func TestJob_PurgeRequestDeletesOnlyJQuantsRowsAndClearsItself(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`,
		`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json) VALUES (1, '7203', 't', 1, 1, 1, 1, 1, '{}')`,
		`INSERT INTO daily_bars (symbol, trade_date, open, high, low, close, volume, adj_open, adj_high, adj_low, adj_close, adj_volume) VALUES ('7203', '2026-11-16', 1, 1, 1, 1, 1, 1, 1, 1, 1, 1)`,
		`INSERT INTO research_jquants_daily_bars (day, code, symbol, open, fetched_at) VALUES ('2026-11-16', '72030', '7203', 1, 't')`,
		`INSERT INTO research_jquants_coverage (dataset, day, row_count, completed_at) VALUES ('daily', '2026-11-16', 1, 't')`,
		`INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_price, exit_price, label_version, long_gross_bps, long_cost_bps, long_net_bps, exit_source)
			VALUES (1, 1, '7203', 't', '2026-11-16', 1, '30m', 't', 1, 1, 1, 1.0, 1.0, 0.0, 'jquants'), (1, 1, '7203', 't', '2026-11-16', 1, '60m', 't', 1, 1, 1, 1.0, 1.0, 0.0, 'snapshot')`,
	} {
		if _, err := e.db.Exec(stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	e.set(t, map[string]string{jquantssettings.KeyPurgeRequested: "1"}) // the switch is off
	transport := &failTransport{}

	err := e.job("", jqservice.Config{HTTP: &http.Client{Transport: transport}}).Run(ctx)
	if !errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Run = %v, want ErrSkipped (the switch is off) after serving the purge", err)
	}
	for table, want := range map[string]int{"research_jquants_daily_bars": 0, "research_jquants_coverage": 0, "market_snapshots": 1, "daily_bars": 1, "research_horizon_labels": 1} {
		if got := e.count(t, table); got != want {
			t.Errorf("%s has %d rows, want %d", table, got, want)
		}
	}
	var source string
	if err := e.db.QueryRow(`SELECT exit_source FROM research_horizon_labels`).Scan(&source); err != nil || source != "snapshot" {
		t.Errorf("the surviving label's exit_source = %q (%v), want snapshot", source, err)
	}
	if _, ok, _ := e.settings.Get(ctx, jquantssettings.KeyPurgeRequested); ok {
		t.Error("the purge request was not cleared")
	}
	if transport.n.Load() != 0 {
		t.Errorf("%d requests during the purge, want 0", transport.n.Load())
	}
}
