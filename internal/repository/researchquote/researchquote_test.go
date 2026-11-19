package researchquote_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/researchquote"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

func open(t *testing.T) (*researchquote.Repository, *sql.DB) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return researchquote.New(db), db
}

func sample(symbol string, at time.Time, price float64) researchquote.Sample {
	return researchquote.Sample{Timestamp: at, Source: researchquote.SourceTachibana, Symbol: symbol, Price: price, Via: researchquote.ViaEvent, SessionPhase: researchquote.PhaseMorning}
}

var minute = time.Date(2026, 10, 8, 0, 30, 0, 0, time.UTC)

func TestInsertBatchStoresEveryColumnAndIgnoresARepeatedMinute(t *testing.T) {
	r, db := open(t)
	bid, ask, high, low, vwap, vol, turn := 999.0, 1001.0, 1010.0, 990.0, 1000.5, 500.0, 500250.0
	observed := minute.Add(-3 * time.Second)
	full := sample("7203", minute, 1000)
	full.Bid, full.Ask, full.High, full.Low, full.VWAP, full.Volume, full.Turnover = &bid, &ask, &high, &low, &vwap, &vol, &turn
	full.SpecialQuote, full.ObservedAt, full.Via, full.SessionPhase = true, &observed, researchquote.ViaREST, researchquote.PhaseClose

	n, err := r.InsertBatch(context.Background(), []researchquote.Sample{full, sample("6758", minute, 2000)})
	if err != nil || n != 2 {
		t.Fatalf("InsertBatch = %d, %v, want 2 new rows", n, err)
	}
	// The same minute again, with another price, adds nothing and changes nothing.
	n, err = r.InsertBatch(context.Background(), []researchquote.Sample{sample("7203", minute, 1), sample("7203", minute.Add(time.Minute), 1002)})
	if err != nil || n != 1 {
		t.Fatalf("second InsertBatch = %d, %v, want only the new minute", n, err)
	}

	var ts, via, phase, obs string
	var price, b, a, h, l, v, vo, tu float64
	var special int
	if err := db.QueryRow(`SELECT timestamp, via, session_phase, observed_at, price, bid, ask, high, low, vwap, volume, turnover, special_quote
		FROM research_quote_samples WHERE symbol = '7203' ORDER BY timestamp LIMIT 1`).Scan(&ts, &via, &phase, &obs, &price, &b, &a, &h, &l, &v, &vo, &tu, &special); err != nil {
		t.Fatal(err)
	}
	if ts != sqlutil.FormatTime(minute) || obs != sqlutil.FormatTime(observed) || via != "rest" || phase != "close" || price != 1000 || special != 1 ||
		b != 999 || a != 1001 || h != 1010 || l != 990 || v != 1000.5 || vo != 500 || tu != 500250 {
		t.Errorf("stored row = %s %s %s %s %v bid %v ask %v high %v low %v vwap %v vol %v turn %v special %d", ts, obs, via, phase, price, b, a, h, l, v, vo, tu, special)
	}
	var nulls int
	if err := db.QueryRow(`SELECT COUNT(*) FROM research_quote_samples WHERE symbol = '6758' AND bid IS NULL AND vwap IS NULL AND observed_at IS NULL`).Scan(&nulls); err != nil || nulls != 1 {
		t.Errorf("a sample without optional values has NULL columns: %d (%v)", nulls, err)
	}
}

func TestInsertBatchIsAllOrNothing(t *testing.T) {
	r, db := open(t)
	bad := sample("6758", minute, 0) // a zero price violates the table's CHECK
	if _, err := r.InsertBatch(context.Background(), []researchquote.Sample{sample("7203", minute, 1000), bad}); err == nil {
		t.Fatal("a zero price was accepted")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM research_quote_samples`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d rows after a failed batch (%v), want none: the batch is one transaction", n, err)
	}
	if n, err := r.InsertBatch(context.Background(), nil); n != 0 || err != nil {
		t.Errorf("an empty batch = %d, %v", n, err)
	}
}

// Only rows before the date and up to the verified id are deleted, in bounded batches.
func TestDeleteBeforeKeepsNewerRowsAndRowsAfterTheVerifiedState(t *testing.T) {
	r, db := open(t)
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 30, 0, 0, time.UTC) }
	var batch []researchquote.Sample
	for d := 1; d <= 3; d++ {
		for i := range 4 {
			batch = append(batch, sample("7203", day(d).Add(time.Duration(i)*time.Minute), 1000))
		}
	}
	if _, err := r.InsertBatch(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	var maxID int64
	if err := db.QueryRow(`SELECT MAX(id) FROM research_quote_samples`).Scan(&maxID); err != nil {
		t.Fatal(err)
	}
	// A late writer adds a row to an old day after the archive was read.
	if _, err := r.InsertBatch(context.Background(), []researchquote.Sample{sample("7203", day(1).Add(time.Hour), 1000)}); err != nil {
		t.Fatal(err)
	}

	total := int64(0)
	for {
		n, err := r.DeleteBefore(context.Background(), "2026-10-03", maxID, 3)
		if err != nil {
			t.Fatal(err)
		}
		if n > 3 {
			t.Fatalf("a batch deleted %d rows, want at most 3", n)
		}
		total += n
		if n < 3 {
			break
		}
	}
	if total != 8 {
		t.Errorf("deleted %d rows, want the 8 of the two verified days", total)
	}
	var remaining, late int
	_ = db.QueryRow(`SELECT COUNT(*) FROM research_quote_samples`).Scan(&remaining)
	_ = db.QueryRow(`SELECT COUNT(*) FROM research_quote_samples WHERE id > ?`, maxID).Scan(&late)
	if remaining != 5 || late != 1 {
		t.Errorf("remaining %d (late writer's row kept: %d), want 5 and 1: day 3 and the row inserted after the archive stay", remaining, late)
	}
	if any, err := r.Any(context.Background()); err != nil || !any {
		t.Errorf("Any = %v, %v", any, err)
	}
}
