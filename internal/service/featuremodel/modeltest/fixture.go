// Package modeltest is the synthetic-data fixture of the feature model's tests (issue
// #1092): a migrated temporary SQLite with instruments, market snapshots with
// features, 30-minute research labels, completion marks and Jev decisions. Only
// tests import it (isolation_test.go pins that).
package modeltest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// JST is the zone of the fixture's days.
var JST = time.FixedZone("JST", 9*60*60)

// Signal is the planted relationship: long_net = LongSlope * return_5m + noise and
// short_net = ShortSlope * return_5m + noise.
const (
	LongSlope  = 3.0
	ShortSlope = -2.0
	// Return5mRange is the half-width of the uniform return_5m of every bar.
	Return5mRange = 10.0
)

// templates are the seeded databases of this test binary, as the bytes of a cleanly
// closed SQLite file keyed by what was seeded: building one (migrations, seeding) is
// the slow part under -race, so each is built once and every test gets its own copy.
// Held in memory, so there is nothing on disk to clean up.
var (
	templatesMu sync.Mutex
	templates   = map[string]*template{}
)

type template struct {
	once sync.Once
	raw  []byte
	err  error
}

// openTemplate returns a fresh database (in the test's temp dir) holding what build
// seeded into a migrated database with instruments 1 and 2. build runs once per key.
func openTemplate(t *testing.T, key string, build func(db *sql.DB)) *sql.DB {
	t.Helper()
	templatesMu.Lock()
	tpl, ok := templates[key]
	if !ok {
		tpl = &template{}
		templates[key] = tpl
	}
	templatesMu.Unlock()
	tpl.once.Do(func() { tpl.raw, tpl.err = buildTemplate(build) })
	if tpl.err != nil {
		t.Fatalf("build fixture %s: %v", key, tpl.err)
	}
	path := filepath.Join(t.TempDir(), "pitha.db")
	if err := os.WriteFile(path, tpl.raw, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlitedb.Open(path) // the schema is current: the migrations are a no-op
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// baseTemplate is the migrated database with instruments 1 and 2: the only place the
// (slow) migrations run; every other template starts from a copy of it.
var baseTemplate template

func buildBase() ([]byte, error) {
	dir, err := os.MkdirTemp("", "featuremodel-template-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "pitha.db")
	db, err := sqlitedb.Open(path)
	if err != nil {
		return nil, err
	}
	for _, id := range []int{1, 2} {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (?, ?, 'x', 'TSE Prime', 1, 't', 't')`,
			id, fmt.Sprintf("720%d", id)); err != nil {
			return nil, errors.Join(err, db.Close())
		}
	}
	if err := db.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func buildTemplate(build func(db *sql.DB)) ([]byte, error) {
	baseTemplate.once.Do(func() { baseTemplate.raw, baseTemplate.err = buildBase() })
	if baseTemplate.err != nil {
		return nil, baseTemplate.err
	}
	dir, err := os.MkdirTemp("", "featuremodel-template-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "pitha.db")
	if err := os.WriteFile(path, baseTemplate.raw, 0o600); err != nil {
		return nil, err
	}
	db, err := sqlitedb.Open(path)
	if err != nil {
		return nil, err
	}
	build(db)
	if err := db.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// OpenDB returns a migrated database with instruments 1 and 2.
func OpenDB(t *testing.T) *sql.DB {
	t.Helper()
	return openTemplate(t, "empty", func(*sql.DB) {})
}

// HistoryDB returns a database with n complete (marked) label days from start, each
// with rows bars, as SeedHistory seeds them. Every distinct (start, n, rows) is built
// once per test binary.
func HistoryDB(t *testing.T, start string, n, rows int) *sql.DB {
	t.Helper()
	return openTemplate(t, fmt.Sprintf("history/%s/%d/%d", start, n, rows), func(db *sql.DB) { SeedHistory(t, db, start, n, rows) })
}

// At is the time of the JST date day at hh:mm plus sec seconds.
func At(t *testing.T, day, hhmm string, sec int) time.Time {
	t.Helper()
	ts, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, JST)
	if err != nil {
		t.Fatal(err)
	}
	return ts.Add(time.Duration(sec) * time.Second)
}

// Day is one labeled day of the fixture.
type Day struct {
	// Rows is the number of bars (and labels). Bars are 1 second apart from 10:00 JST (+StartSec).
	Rows int
	// Seed makes the day's numbers reproducible.
	Seed uint64
	// Mark records the day as complete in research_label_days.
	Mark bool
	// Wild replaces the planted signal with huge values (a day that must never enter a model).
	Wild bool
	// StartSec offsets the first bar from 10:00:00 JST (to add bars to a seeded day).
	StartSec int
	// ExitAt overrides the label's exit time (zero: 30 minutes after the bar).
	ExitAt time.Time
}

// SeedDay inserts the bars of instrument 1 of a JST day with their 30-minute labels
// (sample_bucket 0) and, with Mark, the completion mark. Features: return_5m uniform in
// +-Return5mRange carries the signal; the other features are noise, with some NULLs
// (spread_bps and vwap_slope are NULL on every 7th bar). Returns the first bar's snapshot id.
func SeedDay(t *testing.T, db *sql.DB, day string, d Day) int64 {
	t.Helper()
	rng := rand.New(rand.NewPCG(d.Seed, 7)) //nolint:gosec // G404: reproducible synthetic test data, not a secret
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var first int64
	for i := 0; i < d.Rows; i++ {
		at := At(t, day, "10:00", d.StartSec+i)
		f := func() float64 { return rng.Float64()*2 - 1 }
		r5 := f() * Return5mRange
		var spread, slope any = 5 + f(), f()
		if i%7 == 0 {
			spread, slope = nil, nil
		}
		long, short := LongSlope*r5+f(), ShortSlope*r5+f()
		if d.Wild {
			r5, long, short = 1e6, 1e6, -1e6
		}
		res, err := tx.ExecContext(t.Context(), `INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, spread_bps, volume, turnover, vwap, price_vs_vwap_bps,
	return_1m, return_5m, return_15m, return_30m, volume_ratio_5m, orderbook_imbalance, realized_vol_5m, market_return_5m, sector_return_5m, vwap_slope, raw_data_json)
VALUES (1, '7201', ?, 1000, 999, 1001, ?, 1, 1, 1000, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}')`,
			sqlutil.FormatTime(at), spread, f()*3, f(), r5, f(), f(), 1+f()/2, f()/2, 2+f(), f(), f(), slope)
		if err != nil {
			t.Fatalf("seed snapshot: %v", err)
		}
		id, _ := res.LastInsertId()
		if i == 0 {
			first = id
		}
		exit := at.Add(30 * time.Minute)
		if !d.ExitAt.IsZero() {
			exit = d.ExitAt
		}
		if _, err := tx.ExecContext(t.Context(), `INSERT INTO research_horizon_labels (snapshot_id, instrument_id, symbol, snapshot_at, day, sample_bucket, horizon, exit_at, entry_bid, entry_ask,
	entry_price, exit_price, spread_bps, long_gross_bps, long_cost_bps, long_net_bps, short_gross_bps, short_cost_bps, short_net_bps, label_version)
VALUES (?, 1, '7201', ?, ?, 0, '30m', ?, 999, 1001, 1000, 1000, 5, ?, 0, ?, ?, 0, ?, 1)`,
			id, sqlutil.FormatTime(at), day, sqlutil.FormatTime(exit), long, long, short, short); err != nil {
			t.Fatalf("seed label: %v", err)
		}
	}
	if d.Mark {
		if _, err := tx.ExecContext(t.Context(), `INSERT INTO research_label_days (day, snapshots, labeled_30m, labeled_60m, labeled_close, completed_at) VALUES (?, ?, ?, 0, 0, 't')`,
			day, d.Rows, d.Rows); err != nil {
			t.Fatalf("seed mark: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return first
}

// SeedDecision inserts a Jev decision of instrument 1 (type "trader" or "scout",
// direction LONG/SHORT/NONE) at the given time and returns its id. The bar of the
// decision is the fixture bar at the same time, if there is one.
func SeedDecision(t *testing.T, db *sql.DB, at time.Time, decisionType, direction string) int64 {
	t.Helper()
	res, err := db.ExecContext(t.Context(), `INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction,
	confidence, latency_ms, model_id) VALUES (1, '7201', ?, ?, 'h', '{}', 'q1', '{}', ?, 0.5, 1, 'm1')`, sqlutil.FormatTime(at), decisionType, direction)
	if err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// Days returns n consecutive JST dates starting at start.
func Days(t *testing.T, start string, n int) []string {
	t.Helper()
	d, err := time.ParseInLocation(time.DateOnly, start, JST)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, n)
	for i := range out {
		out[i] = d.AddDate(0, 0, i).Format(time.DateOnly)
	}
	return out
}

// SeedHistory seeds n marked days from start, each with rows bars, and returns the dates.
func SeedHistory(t *testing.T, db *sql.DB, start string, n, rows int) []string {
	t.Helper()
	days := Days(t, start, n)
	for i, day := range days {
		SeedDay(t, db, day, Day{Rows: rows, Seed: uint64(i + 1), Mark: true})
	}
	return days
}
