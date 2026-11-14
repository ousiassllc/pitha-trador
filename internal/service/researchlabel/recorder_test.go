package researchlabel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Per instrument and day the fixture bars run 14:00..15:29 (one per minute,
// the price rising 1 yen per minute, quote price -/+ 1). By hand: a 30m row
// needs a bar within 2 minutes of the target, so entries 14:00..15:01 (62); a
// 60m row entries 14:00..14:31 (32); a close row every entry before 15:25, the
// last bar 15:29 being the exit (14:00..15:24 = 85).
const rowsPerInstrumentDay = 62 + 32 + 85

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, id := range []int{1, 2} {
		if _, err := db.Exec(`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (?, ?, 'x', 'TSE Prime', 1, 't', 't')`,
			id, fmt.Sprintf("720%d", id)); err != nil {
			t.Fatalf("seed instrument: %v", err)
		}
	}
	return db
}

// seedDay inserts the fixture bars of both instruments on the JST date d
// (instrument 2 has no ask, so only its SHORT side has results).
func seedDay(t *testing.T, db *sql.DB, d string) {
	t.Helper()
	start, err := time.ParseInLocation("2006-01-02 15:04", d+" 14:00", testJST)
	if err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 2; id++ {
		for i := 0; i < 90; i++ {
			price := 1000 + float64(i)
			var ask any = price + 1
			if id == 2 {
				ask = nil
			}
			if _, err := db.Exec(`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, spread_bps, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json)
				VALUES (?, ?, ?, ?, ?, ?, 20, 1, 1, 1, 1, '{}')`,
				id, fmt.Sprintf("720%d", id), sqlutil.FormatTime(start.Add(time.Duration(i)*time.Minute)), price, price-1, ask); err != nil {
				t.Fatalf("seed snapshot: %v", err)
			}
		}
	}
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func fixedNow(s string) func() time.Time {
	ts, err := time.ParseInLocation("2006-01-02 15:04", s, testJST)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return ts }
}

func fixedCosts(c Costs) CostSource {
	return func(context.Context) (Costs, error) { return c, nil }
}

func newRecorder(db *sql.DB, now string) *Recorder {
	return NewRecorder(researchlabel.NewRepository(db), fixedCosts(Costs{Long: 19, Short: 27})).WithClock(fixedNow(now))
}

func TestRecorder_LabelsOnlyCompleteDaysAndRerunIsIdempotent(t *testing.T) {
	db := openDB(t)
	for _, d := range []string{"2026-10-13", "2026-10-14", "2026-10-15"} {
		seedDay(t, db, d)
	}
	rec := newRecorder(db, "2026-10-15 16:30") // 10-15 is today: still the day being collected

	res, err := rec.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Days, ",") != "2026-10-13,2026-10-14" {
		t.Fatalf("days = %v, want 10-13 and 10-14 (never today's)", res.Days)
	}
	if want := 2 * 2 * rowsPerInstrumentDay; res.Rows != want || count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`) != want {
		t.Fatalf("rows = %d (table %d), want %d", res.Rows, count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`), want)
	}
	if res.Snapshots != 2*2*90 {
		t.Errorf("snapshots read = %d, want %d", res.Snapshots, 2*2*90)
	}
	for horizon, want := range map[string]int{"30m": 2 * 2 * 62, "60m": 2 * 2 * 32, "close": 2 * 2 * 85} {
		if got := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels WHERE horizon = ?`, horizon); got != want {
			t.Errorf("%s rows = %d, want %d", horizon, got, want)
		}
	}
	// Today's JST day (10-15 00:00 JST = 10-14T15:00Z) is untouched.
	if n := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels WHERE snapshot_at >= ?`, sqlutil.FormatTime(time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC))); n != 0 {
		t.Errorf("%d rows of today's JST day", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_label_days WHERE day IN ('2026-10-13', '2026-10-14')`); n != 2 {
		t.Errorf("done marks = %d, want 2", n)
	}
	// Instrument 2 has no ask: only the SHORT side has a result.
	if n := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels WHERE instrument_id = 2 AND (long_net_bps IS NOT NULL OR short_net_bps IS NULL)`); n != 0 {
		t.Errorf("%d one-sided rows with the wrong side filled", n)
	}
	// The cost columns carry the costs of the run, per side.
	if n := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels WHERE (long_cost_bps IS NOT NULL AND long_cost_bps <> 19) OR (short_cost_bps <> 27)`); n != 0 {
		t.Errorf("%d rows with another cost than the run's", n)
	}
	// A hand-computed row: instrument 1, 14:00 entry (ask 1001, bid 999), 30m exit at 14:30 (price 1030).
	var longGross, longNet, shortGross float64
	err = db.QueryRow(`SELECT long_gross_bps, long_net_bps, short_gross_bps FROM research_horizon_labels
		WHERE instrument_id = 1 AND horizon = '30m' AND snapshot_at = ?`,
		sqlutil.FormatTime(time.Date(2026, 10, 13, 5, 0, 0, 0, time.UTC))).Scan(&longGross, &longNet, &shortGross)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "long gross", longGross, (1030.0-1001.0)/1001.0*10000)
	near(t, "long net", longNet, (1030.0-1001.0)/1001.0*10000-19)
	near(t, "short gross", shortGross, -(1030.0-999.0)/999.0*10000)

	// A second run finds nothing to do and writes nothing.
	again, err := rec.Run(context.Background())
	if err != nil || len(again.Days) != 0 || again.Rows != 0 {
		t.Fatalf("second run = %+v, %v; want nothing", again, err)
	}

	// Even with the marks gone (a day redone), the rows are not duplicated.
	if _, err := db.Exec(`DELETE FROM research_label_days`); err != nil {
		t.Fatal(err)
	}
	redo, err := rec.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(redo.Days) != 2 || redo.Rows != 0 {
		t.Fatalf("redo = %+v, want 2 days and 0 new rows", redo)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`); n != 2*2*rowsPerInstrumentDay {
		t.Fatalf("rows after a redo = %d, want unchanged", n)
	}
}

// failingStore fails the Insert of one instrument on one day, as a crash midway would.
type failingStore struct {
	Store
	failDay string
	failID  int64
}

func (f *failingStore) Bars(ctx context.Context, day string, id int64) ([]domain.Snapshot, error) {
	if day == f.failDay && id == f.failID {
		return nil, errors.New("simulated crash")
	}
	return f.Store.Bars(ctx, day, id)
}

func TestRecorder_ResumesAfterAPartialRun(t *testing.T) {
	db := openDB(t)
	seedDay(t, db, "2026-10-13")
	seedDay(t, db, "2026-10-14")
	repo := researchlabel.NewRepository(db)
	crash := &failingStore{Store: repo, failDay: "2026-10-14", failID: 2}
	rec := NewRecorder(crash, fixedCosts(Costs{Long: 19, Short: 27})).WithClock(fixedNow("2026-10-15 16:30"))

	res, err := rec.Run(context.Background())
	if err == nil {
		t.Fatal("the simulated crash was not returned")
	}
	if len(res.Days) != 1 || res.Days[0] != "2026-10-13" {
		t.Fatalf("completed days = %v, want only 10-13", res.Days)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_label_days`); n != 1 {
		t.Fatalf("marks after a crash = %d, want 1 (the unfinished day is not marked)", n)
	}
	partial := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`)
	if partial != rowsPerInstrumentDay*2+rowsPerInstrumentDay { // 10-13 both instruments + 10-14 instrument 1
		t.Fatalf("rows after the crash = %d", partial)
	}

	// The next run redoes 10-14 only and completes it without duplicates.
	res, err = newRecorder(db, "2026-10-15 16:30").Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Days) != 1 || res.Days[0] != "2026-10-14" || res.Rows != rowsPerInstrumentDay {
		t.Fatalf("resumed run = %+v, want 10-14 with %d new rows", res, rowsPerInstrumentDay)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`); n != 4*rowsPerInstrumentDay {
		t.Fatalf("rows = %d, want %d", n, 4*rowsPerInstrumentDay)
	}
}

func TestRecorder_BoundsTheBacklogPerRunOldestFirst(t *testing.T) {
	db := openDB(t)
	for _, d := range []string{"2026-10-12", "2026-10-13", "2026-10-14"} {
		seedDay(t, db, d)
	}
	rec := newRecorder(db, "2026-10-15 16:30").WithMaxDaysPerRun(2)
	first, err := rec.Run(context.Background())
	if err != nil || strings.Join(first.Days, ",") != "2026-10-12,2026-10-13" {
		t.Fatalf("first run = %+v, %v; want the two oldest days", first, err)
	}
	second, err := rec.Run(context.Background())
	if err != nil || strings.Join(second.Days, ",") != "2026-10-14" {
		t.Fatalf("second run = %+v, %v; want the remaining day", second, err)
	}
}

func TestRecorder_PurgedSnapshotsAreNotAnError(t *testing.T) {
	db := openDB(t)
	seedDay(t, db, "2026-10-13")
	seedDay(t, db, "2026-10-14")
	rec := newRecorder(db, "2026-10-15 16:30").WithMaxDaysPerRun(1)
	if _, err := rec.Run(context.Background()); err != nil { // records 10-13
		t.Fatal(err)
	}
	before := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`)

	// Retention purges market_snapshots; the research rows do not depend on them.
	if _, err := db.Exec(`DELETE FROM market_snapshots`); err != nil {
		t.Fatal(err)
	}
	res, err := rec.Run(context.Background())
	if err != nil || len(res.Days) != 0 {
		t.Fatalf("run over purged snapshots = %+v, %v; want nothing and no error", res, err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`); n != before {
		t.Fatalf("rows %d -> %d after the snapshots were purged", before, n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_label_days WHERE day = '2026-10-14'`); n != 0 {
		t.Error("a purged day was marked as recorded")
	}

	// A day purged in the middle of the list is skipped, later ones still recorded.
	db2 := openDB(t)
	for _, d := range []string{"2026-10-12", "2026-10-13", "2026-10-14"} {
		seedDay(t, db2, d)
	}
	from, to, _ := researchlabel.DayBounds("2026-10-13")
	if _, err := db2.Exec(`DELETE FROM market_snapshots WHERE timestamp >= ? AND timestamp < ?`, sqlutil.FormatTime(from), sqlutil.FormatTime(to)); err != nil {
		t.Fatal(err)
	}
	res, err = newRecorder(db2, "2026-10-15 16:30").Run(context.Background())
	if err != nil || strings.Join(res.Days, ",") != "2026-10-12,2026-10-14" {
		t.Fatalf("run with a purged middle day = %+v, %v", res, err)
	}
}

func TestRecorder_CostErrorIsReturnedAndNothingIsMarked(t *testing.T) {
	db := openDB(t)
	seedDay(t, db, "2026-10-13")
	rec := NewRecorder(researchlabel.NewRepository(db), func(context.Context) (Costs, error) { return Costs{}, errors.New("settings unreadable") }).
		WithClock(fixedNow("2026-10-15 16:30"))
	if _, err := rec.Run(context.Background()); err == nil {
		t.Fatal("a settings failure was swallowed")
	}
	if count(t, db, `SELECT COUNT(*) FROM research_label_days`) != 0 || count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`) != 0 {
		t.Error("something was written although the costs could not be read")
	}
}
