package researchfxjob

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/fx/gmo"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// fakeClock is a virtual clock for the adapter: Sleep advances time instantly, so
// the one-call-per-second spacing costs no real time. It records the times the
// mock server saw requests at.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	return nil
}

// mockGMO plays GMO's klines endpoint: a weekday trading day (Mon-Fri) has three
// minutes of BID candles and two of ASK (the third minute is BID only); Saturday
// and Sunday trading days are closed (no candle).
type mockGMO struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
	times    []time.Time
	failDate string
}

func newMockGMO(t *testing.T, clk *fakeClock) *mockGMO {
	t.Helper()
	m := &mockGMO{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.requests = append(m.requests, r.URL.RequestURI())
		m.times = append(m.times, clk.Now())
		fail := m.failDate
		m.mu.Unlock()
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/klines" || q.Get("symbol") == "" || q.Get("interval") != "1min" {
			http.Error(w, `{"status":1,"messages":[{"message_code":"ERR-5003","message_string":"unexpected request"}]}`, http.StatusBadRequest)
			return
		}
		date := q.Get("date")
		if date == fail {
			http.Error(w, `{"status":1,"messages":[{"message_code":"ERR-5003","message_string":"invalid parameter"}]}`, http.StatusBadRequest)
			return
		}
		day, err := time.ParseInLocation("20060102", date, time.FixedZone("JST", 9*3600))
		if err != nil {
			http.Error(w, `{"status":1}`, http.StatusBadRequest)
			return
		}
		n := 3
		if q.Get("priceType") == "ASK" {
			n = 2
		}
		if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
			n = 0
		}
		body := `{"status":0,"data":[`
		for i := 0; i < n; i++ {
			if i > 0 {
				body += ","
			}
			ms := day.Add(6*time.Hour + time.Duration(i)*time.Minute).UnixMilli()
			body += `{"openTime":"` + strconv.FormatInt(ms, 10) + `","open":"150.100","high":"150.200","low":"150.000","close":"150.150"}`
		}
		_, _ = w.Write([]byte(body + `],"responsetime":"2026-10-16T00:00:00.000Z"}`))
	}))
	t.Cleanup(m.Close)
	return m
}

func (m *mockGMO) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

type env struct {
	job      *Job
	db       *sql.DB
	settings *system.RuntimeSettingsRepository
	gmo      *mockGMO
	now      time.Time
}

// newEnv is a Job over a fresh database and a mock GMO server, at Friday
// 2026-10-16 10:00 JST, nothing stored in runtime_settings (the shipped state).
func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := &fakeClock{now: time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC)}
	e := &env{db: db, settings: system.NewRuntimeSettingsRepository(db), gmo: newMockGMO(t, clk), now: clk.now}
	e.job = New(db, e.settings, WithBaseURL(e.gmo.URL), WithGMOOptions(gmo.WithClock(clk)), WithClock(func() time.Time { return e.now }), WithoutPace())
	return e
}

func (e *env) set(t *testing.T, key, value string) {
	t.Helper()
	if err := e.settings.Set(context.Background(), key, value, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// runAll calls Run until it stops returning ErrMore (as the maintenance runner does).
func (e *env) runAll(t *testing.T) int {
	t.Helper()
	runs := 0
	for {
		runs++
		err := e.job.Run(context.Background())
		if errors.Is(err, maintenance.ErrMore) {
			if runs > 50 {
				t.Fatal("Run keeps returning ErrMore")
			}
			continue
		}
		if err != nil {
			t.Fatalf("Run #%d: %v", runs, err)
		}
		return runs
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

// With the switch on, USD/JPY 1-minute candles (BID and ASK merged by minute)
// land in research_fx_candles, every completed day of the window is marked, a
// weekend day is marked 'empty' (the market was closed, not a gap), today's
// trading day is not fetched, and a rerun neither duplicates nor requests again.
func TestJob_RecordsMergedCandlesMarksDaysAndIsIdempotent(t *testing.T) {
	e := newEnv(t)
	e.set(t, researchfxsettings.KeyEnabled, "1")
	runs := e.runAll(t)

	// Window at 2026-10-16 10:00: trading days 10-09 (Fri) .. 10-15 (Thu); 7 runs, 2 requests each.
	if runs != 7 || e.gmo.count() != 14 {
		t.Fatalf("runs = %d, requests = %d; want 7 runs and 14 requests (BID + ASK per day)", runs, e.gmo.count())
	}
	// Weekdays 09, 12, 13, 14, 15 have 3 candles; 10 and 11 (Sat, Sun) none.
	if got := e.count(t, `SELECT COUNT(*) FROM research_fx_candles WHERE symbol = 'USD_JPY'`); got != 15 {
		t.Fatalf("candles = %d, want 15", got)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_fx_candles WHERE bid_open IS NOT NULL AND ask_open IS NOT NULL`); got != 10 {
		t.Errorf("merged candles = %d, want 10 (two per weekday)", got)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_fx_candles WHERE ask_open IS NULL AND bid_open IS NOT NULL`); got != 5 {
		t.Errorf("BID-only candles = %d, want 5 (one per weekday)", got)
	}
	var open, day string
	if err := e.db.QueryRow(`SELECT open_time, trading_day FROM research_fx_candles ORDER BY open_time LIMIT 1`).Scan(&open, &day); err != nil {
		t.Fatal(err)
	}
	if open != "2026-10-08T21:00:00.000000000Z" || day != "2026-10-09" {
		t.Errorf("first candle = %s on trading day %s, want 06:00 JST of 10-09 in UTC with 9 fractional digits", open, day)
	}
	for date, want := range map[string]string{"2026-10-09": "recorded", "2026-10-10": "empty", "2026-10-11": "empty", "2026-10-12": "recorded", "2026-10-15": "recorded"} {
		var status string
		var n int
		if err := e.db.QueryRow(`SELECT status, candle_count FROM research_fx_days WHERE symbol = 'USD_JPY' AND trading_day = ?`, date).Scan(&status, &n); err != nil {
			t.Fatalf("marker of %s: %v", date, err)
		}
		if status != want || (want == "empty") != (n == 0) {
			t.Errorf("marker of %s = %s/%d, want %s", date, status, n, want)
		}
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_fx_days`); got != 7 {
		t.Errorf("markers = %d, want 7 (today's trading day 10-16 is not fetched)", got)
	}
	// The adapter's pacing is intact: never two requests less than a second apart.
	for i := 1; i < len(e.gmo.times); i++ {
		if d := e.gmo.times[i].Sub(e.gmo.times[i-1]); d < time.Second {
			t.Fatalf("requests %d and %d were %v apart, want >= 1s", i-1, i, d)
		}
	}

	// Rerun: nothing is pending, no request, no new row.
	before := e.gmo.count()
	if runs := e.runAll(t); runs != 1 || e.gmo.count() != before {
		t.Fatalf("rerun: runs = %d, new requests = %d; want 1 run and none", runs, e.gmo.count()-before)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_fx_candles`); got != 15 {
		t.Errorf("candles after the rerun = %d, want 15", got)
	}
	// A marker lost (as if the day had never completed) is refetched without duplicating candles.
	if _, err := e.db.Exec(`DELETE FROM research_fx_days WHERE trading_day = '2026-10-13'`); err != nil {
		t.Fatal(err)
	}
	e.runAll(t)
	if got := e.count(t, `SELECT COUNT(*) FROM research_fx_candles`); got != 15 {
		t.Errorf("candles after refetching one day = %d, want 15 (INSERT OR IGNORE)", got)
	}
	if got := e.count(t, `SELECT candle_count FROM research_fx_days WHERE trading_day = '2026-10-13'`); got != 3 {
		t.Errorf("candle_count of the refetched day = %d, want 3", got)
	}
}

// The next day, one more trading day completes and is fetched with two requests.
func TestJob_ARunTheNextDayFetchesOnlyTheNewDay(t *testing.T) {
	e := newEnv(t)
	e.set(t, researchfxsettings.KeyEnabled, "1")
	e.runAll(t)
	before := e.gmo.count()
	e.now = e.now.Add(24 * time.Hour) // Saturday 10:00 JST: trading day 10-16 (Fri) is complete
	e.runAll(t)
	if got := e.gmo.count() - before; got != 2 {
		t.Fatalf("requests the next day = %d, want 2", got)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_fx_days WHERE trading_day = '2026-10-16' AND status = 'recorded'`); got != 1 {
		t.Fatal("the newly completed day was not recorded")
	}
}

// Failures leave no marker and do not stop the other days; the process is not crashed.
func TestJob_AFailedDayStaysUnmarkedAndTheRestProceed(t *testing.T) {
	e := newEnv(t)
	e.set(t, researchfxsettings.KeyEnabled, "1")
	e.gmo.failDate = "20261012"
	failures, runs := 0, 0
	for ; runs < 30; runs++ {
		err := e.job.Run(context.Background())
		if errors.Is(err, maintenance.ErrMore) {
			continue
		}
		if err != nil {
			failures++
			continue
		}
		break
	}
	if failures == 0 {
		t.Fatal("the refused day was not reported as a failure")
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_fx_days WHERE trading_day = '2026-10-12'`); got != 0 {
		t.Fatal("the refused day got a marker")
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_fx_days`); got != 6 {
		t.Fatalf("markers = %d, want the other 6 days", got)
	}
	e.gmo.failDate = ""
	e.runAll(t)
	if got := e.count(t, `SELECT COUNT(*) FROM research_fx_days WHERE trading_day = '2026-10-12'`); got != 1 {
		t.Fatal("the day was not retried after GMO recovered")
	}
}

type panicFetcherSettings struct{}

func (panicFetcherSettings) Get(context.Context, string) (string, bool, error) { panic("boom") }

// A panic inside the recording is returned as an error, not a crash.
func TestJob_APanicIsReturnedAsAnError(t *testing.T) {
	e := newEnv(t)
	e.job.settings = panicFetcherSettings{}
	if err := e.job.Run(context.Background()); err == nil || errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Run = %v, want a panic error", err)
	}
}

// Before 07:00 JST the task is skipped (the previous trading day may not be complete).
func TestJob_IsSkippedBeforeRunFrom(t *testing.T) {
	e := newEnv(t)
	e.set(t, researchfxsettings.KeyEnabled, "1")
	e.now = time.Date(2026, 10, 16, 6, 59, 0, 0, time.FixedZone("JST", 9*3600))
	if err := e.job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Run = %v, want ErrSkipped", err)
	}
	if e.gmo.count() != 0 {
		t.Fatal("a request was made before RunFrom")
	}
}
