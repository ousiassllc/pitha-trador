package researchindexjob

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchindexsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

var jst = time.FixedZone("JST", 9*3600)

// countingTransport counts every outbound HTTP request the process makes through
// the default transport; the job must make none, on or off.
type countingTransport struct{ n atomic.Int64 }

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.n.Add(1)
	return nil, errors.New("no outbound request is allowed in this test")
}

type env struct {
	db       *sql.DB
	settings *system.RuntimeSettingsRepository
	job      *Job
	now      time.Time
	requests *countingTransport
}

// newEnv is a fresh database with the clock at 2026-11-20 (Fri) 16:00 JST: the
// day's session is complete, so the window is the trading days 11-16 .. 11-20.
func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rt := &countingTransport{}
	prev := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = prev })
	e := &env{db: db, settings: system.NewRuntimeSettingsRepository(db), now: time.Date(2026, 11, 20, 16, 0, 0, 0, jst), requests: rt}
	e.job = New(db, e.settings, WithClock(func() time.Time { return e.now }))
	return e
}

func (e *env) set(t *testing.T, key, value string) {
	t.Helper()
	if err := e.settings.Set(context.Background(), key, value, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func (e *env) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// sample stores what the 立花 quote recording (research_quote_samples) holds: the
// value of symbol for the minute at JST time hm of day.
func (e *env) sample(t *testing.T, source, symbol, day, hm string, price float64) {
	t.Helper()
	at, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hm, jst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO research_quote_samples (timestamp, source, symbol, price, via, session_phase) VALUES (?, ?, ?, ?, 'event', 'morning')`,
		sqlutil.FormatTime(at), source, symbol, price); err != nil {
		t.Fatal(err)
	}
}

// With the switch off (nothing stored, 0, a damaged or out-of-range value) the job
// makes no request, reads and writes nothing: no minute, no marker, not even the
// maintenance runner's last-success row. Turning it on is picked up by the next run.
func TestJob_IsOffByDefaultAndTouchesNothingWhileOff(t *testing.T) {
	tables := []string{"research_index_minutes", "research_index_days", "runtime_settings"}
	for name, stored := range map[string]string{"nothing stored": "", "stored 0": "0", "damaged": `"yes"`, "out of range": "2"} {
		e := newEnv(t)
		e.sample(t, "tachibana", "101", "2026-11-20", "09:00", 38000)
		if stored != "" {
			e.set(t, researchindexsettings.KeyEnabled, stored)
		}
		before := map[string]int{}
		for _, tbl := range tables {
			before[tbl] = e.count(t, `SELECT COUNT(*) FROM `+tbl)
		}
		if err := e.job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
			t.Fatalf("%s: Run = %v, want maintenance.ErrSkipped", name, err)
		}
		runner := maintenance.NewRunner(e.settings, maintenance.WithClock(func() time.Time { return e.now }))
		runner.Add(e.job.Task())
		runner.CatchUp(context.Background())

		for _, tbl := range tables {
			if got := e.count(t, `SELECT COUNT(*) FROM `+tbl); got != before[tbl] {
				t.Errorf("%s: %s rows %d -> %d while the switch was off", name, tbl, before[tbl], got)
			}
		}
		if _, ok, _ := e.settings.Get(context.Background(), "system.maintenance."+TaskName+".last_success_date"); ok {
			t.Errorf("%s: a last-success date was written for a task that did nothing", name)
		}
		if n := e.requests.n.Load(); n != 0 {
			t.Errorf("%s: %d outbound requests, want 0", name, n)
		}
	}
}

// With the switch on, the values 立花 stored for the index codes are copied
// minute by minute; a code with no value on a trading day is marked
// 'unavailable' (not obtained); a sample outside the session, another source's
// row, another code and a market holiday add nothing; a rerun adds nothing; and
// no outbound request is made.
func TestJob_CopiesTheStoredValuesAndMarksTheDaysWithoutValues(t *testing.T) {
	e := newEnv(t)
	e.set(t, researchindexsettings.KeyEnabled, "1")
	e.sample(t, "tachibana", "101", "2026-11-20", "09:00", 38000)
	e.sample(t, "tachibana", "101", "2026-11-20", "09:01", 38010)
	e.sample(t, "tachibana", "101", "2026-11-20", "12:30", 38020)
	e.sample(t, "tachibana", "101", "2026-11-20", "08:55", 37990) // before the open: not a session minute
	e.sample(t, "tachibana", "101", "2026-11-19", "15:29", 37900)
	e.sample(t, "kabu", "101", "2026-11-20", "09:02", 1)       // the other source
	e.sample(t, "tachibana", "7203", "2026-11-20", "09:00", 3) // not a configured code
	e.sample(t, "tachibana", "151", "2026-11-20", "09:00", 2700)

	if err := e.job.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_index_minutes WHERE symbol = '101'`); got != 4 {
		t.Errorf("101 minutes = %d, want 4 (09:00, 09:01, 12:30 on 11-20 and 15:29 on 11-19)", got)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_index_minutes WHERE symbol = '151'`); got != 1 {
		t.Errorf("151 minutes = %d, want 1", got)
	}
	var price float64
	var minuteTime, tradingDay string
	if err := e.db.QueryRow(`SELECT minute_time, trading_day, price FROM research_index_minutes WHERE symbol = '101' ORDER BY minute_time LIMIT 1`).Scan(&minuteTime, &tradingDay, &price); err != nil {
		t.Fatal(err)
	}
	if minuteTime != "2026-11-19T06:29:00.000000000Z" || tradingDay != "2026-11-19" || price != 37900 {
		t.Errorf("first minute = %s %s %v, want 15:29 JST of 11-19 as UTC with 9 fractional digits", minuteTime, tradingDay, price)
	}
	// 5 trading days (11-16 .. 11-20) x 2 codes are marked.
	if got := e.count(t, `SELECT COUNT(*) FROM research_index_days`); got != 10 {
		t.Fatalf("markers = %d, want 10 (5 trading days x 2 codes)", got)
	}
	for _, c := range []struct{ symbol, day, status string }{
		{"101", "2026-11-20", "recorded"}, {"101", "2026-11-19", "recorded"}, {"101", "2026-11-18", "unavailable"},
		{"151", "2026-11-20", "recorded"}, {"151", "2026-11-19", "unavailable"},
	} {
		var status string
		if err := e.db.QueryRow(`SELECT status FROM research_index_days WHERE symbol = ? AND trading_day = ?`, c.symbol, c.day).Scan(&status); err != nil || status != c.status {
			t.Errorf("marker %s %s = %q (%v), want %s", c.symbol, c.day, status, err, c.status)
		}
	}
	var n int
	if err := e.db.QueryRow(`SELECT minute_count FROM research_index_days WHERE symbol = '101' AND trading_day = '2026-11-20'`).Scan(&n); err != nil || n != 3 {
		t.Errorf("minute_count of 101 on 11-20 = %d (%v), want 3", n, err)
	}

	before := e.count(t, `SELECT COUNT(*) FROM research_index_minutes`)
	e.sample(t, "tachibana", "101", "2026-11-20", "09:03", 38030) // arrives after the day was marked
	if err := e.job.Run(context.Background()); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_index_minutes`); got != before {
		t.Errorf("a rerun changed the minutes %d -> %d: a marked day is final", before, got)
	}
	if got := e.requests.n.Load(); got != 0 {
		t.Errorf("%d outbound requests, want 0", got)
	}
}

// Before 15:35 JST today's session is not complete: the job is skipped, and the
// window of the recorder ends at yesterday's trading day.
func TestJob_WaitsForTheSessionToComplete(t *testing.T) {
	e := newEnv(t)
	e.set(t, researchindexsettings.KeyEnabled, "1")
	e.now = time.Date(2026, 11, 20, 15, 34, 0, 0, jst)
	if err := e.job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Run at 15:34 = %v, want ErrSkipped", err)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_index_days`); got != 0 {
		t.Fatalf("markers = %d before the session is complete", got)
	}
}

// The codes come from research.index.symbols; a damaged value falls back to the defaults.
func TestJob_RecordsTheConfiguredCodes(t *testing.T) {
	e := newEnv(t)
	e.set(t, researchindexsettings.KeyEnabled, "1")
	e.set(t, researchindexsettings.KeySymbols, `"101"`)
	if err := e.job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_index_days WHERE symbol = '151'`); got != 0 {
		t.Errorf("151 markers = %d with only 101 configured", got)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_index_days WHERE symbol = '101'`); got != 5 {
		t.Errorf("101 markers = %d, want 5", got)
	}
}
