package researchindex_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	rindex "github.com/ousiassllc/pitha-trador/internal/domain/researchindex"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchindex"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

func newRepo(t *testing.T) (*researchindex.Repository, *sql.DB) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return researchindex.NewRepository(db), db
}

func minute(day, hm string, price float64) rindex.Minute {
	at, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hm, rindex.JST)
	if err != nil {
		panic(err)
	}
	return rindex.Minute{Symbol: "101", Time: at.UTC(), TradingDay: day, Price: price, Via: rindex.ViaEvent}
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRecordDay_WritesMinutesAndTheMarkerAtomicallyAndIdempotently(t *testing.T) {
	repo, db := newRepo(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC)
	minutes := []rindex.Minute{minute("2026-10-15", "09:00", 38000), minute("2026-10-15", "09:01", 38010)}

	d, err := repo.RecordDay(ctx, "101", "2026-10-15", minutes, at)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != rindex.StatusRecorded || d.MinuteCount != 2 {
		t.Fatalf("marker = %+v, want recorded with 2 minutes", d)
	}
	var openTime string
	if err := db.QueryRow(`SELECT minute_time FROM research_index_minutes ORDER BY minute_time LIMIT 1`).Scan(&openTime); err != nil || openTime != "2026-10-15T00:00:00.000000000Z" {
		t.Fatalf("first minute_time = %q, %v; want UTC with 9 fractional digits", openTime, err)
	}

	// A rerun (even with other numbers) adds no row and keeps the stored values.
	if d, err = repo.RecordDay(ctx, "101", "2026-10-15", []rindex.Minute{minute("2026-10-15", "09:00", 1)}, at.Add(time.Hour)); err != nil || d.MinuteCount != 2 {
		t.Fatalf("rerun = %+v, %v; want 2 minutes still", d, err)
	}
	var kept float64
	if err := db.QueryRow(`SELECT price FROM research_index_minutes ORDER BY minute_time LIMIT 1`).Scan(&kept); err != nil || kept != 38000 {
		t.Fatalf("the stored value was overwritten: %v, %v", kept, err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM research_index_days`); got != 1 {
		t.Fatalf("markers = %d, want 1 (replaced, not duplicated)", got)
	}

	// A day without a value is an 'unavailable' marker (not obtained), not an error.
	if d, err = repo.RecordDay(ctx, "101", "2026-10-16", nil, at); err != nil || d.Status != rindex.StatusUnavailable || d.MinuteCount != 0 {
		t.Fatalf("unavailable day = %+v, %v", d, err)
	}
	marked, err := repo.MarkedDays(ctx, "101", "2026-10-01", "2026-10-31")
	if err != nil || len(marked) != 2 || !marked["2026-10-15"] || !marked["2026-10-16"] {
		t.Fatalf("MarkedDays = %v, %v", marked, err)
	}
	if marked, _ = repo.MarkedDays(ctx, "151", "2026-10-01", "2026-10-31"); len(marked) != 0 {
		t.Fatalf("MarkedDays of another code = %v", marked)
	}
}

// A failure while writing rolls the whole day back: no value without its marker.
func TestRecordDay_ARejectedMinuteLeavesNothingBehind(t *testing.T) {
	repo, db := newRepo(t)
	bad := minute("2026-10-15", "09:01", 0) // a non-positive price violates the table's CHECK
	if _, err := repo.RecordDay(context.Background(), "101", "2026-10-15", []rindex.Minute{minute("2026-10-15", "09:00", 1), bad}, time.Now()); err == nil {
		t.Fatal("a non-positive price was accepted")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_index_minutes`) + count(t, db, `SELECT COUNT(*) FROM research_index_days`); n != 0 {
		t.Fatalf("%d rows left after the failed write", n)
	}
	if _, err := repo.RecordDay(context.Background(), "101", "2026-10-15", []rindex.Minute{minute("2026-10-14", "09:00", 1)}, time.Now()); err == nil {
		t.Fatal("a minute of another trading day was accepted")
	}
	if _, err := repo.RecordDay(context.Background(), "101", "15/10/2026", nil, time.Now()); err == nil {
		t.Fatal("a malformed trading day was accepted")
	}
}

// Samples reads only the 立花 rows of the code on the JST date, in order, and
// never writes the quote table.
func TestSamples_ReadsOnlyTheCodesTachibanaRowsOfTheJSTDay(t *testing.T) {
	repo, db := newRepo(t)
	put := func(source, symbol string, m rindex.Minute, via string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO research_quote_samples (timestamp, source, symbol, price, via, session_phase) VALUES (?, ?, ?, ?, ?, 'morning')`,
			sqlutil.FormatTime(m.Time), source, symbol, m.Price, via); err != nil {
			t.Fatal(err)
		}
	}
	put("tachibana", "101", minute("2026-10-15", "09:01", 2), "rest")
	put("tachibana", "101", minute("2026-10-15", "09:00", 1), "event")
	put("tachibana", "101", minute("2026-10-14", "15:29", 9), "event") // the day before
	put("tachibana", "101", minute("2026-10-16", "09:00", 9), "event") // the day after
	put("kabu", "101", minute("2026-10-15", "09:02", 9), "event")      // another source
	put("tachibana", "151", minute("2026-10-15", "09:00", 9), "event") // another code
	before := count(t, db, `SELECT COUNT(*) FROM research_quote_samples`)

	got, err := repo.Samples(context.Background(), "101", "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Price != 1 || got[0].Via != rindex.ViaEvent || got[1].Price != 2 || got[1].Via != rindex.ViaREST || got[0].TradingDay != "2026-10-15" {
		t.Fatalf("Samples = %+v, want the two 101 rows of 10-15 in order", got)
	}
	if !got[0].Time.Equal(minute("2026-10-15", "09:00", 0).Time) {
		t.Fatalf("first time = %v", got[0].Time)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_quote_samples`); n != before {
		t.Fatalf("the quote table changed: %d -> %d", before, n)
	}
	if _, err := repo.Samples(context.Background(), "101", "bad"); err == nil {
		t.Fatal("a malformed day was accepted")
	}
}
