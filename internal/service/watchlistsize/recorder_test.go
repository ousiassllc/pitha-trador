package watchlistsize_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingwatch"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/watchlistruntime"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/watchlistsize"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// jst builds a time of Wednesday 2026-10-21 (a trading day) in JST.
func jst(h, m int) time.Time { return time.Date(2026, 10, 21, h, m, 0, 0, marketcalendar.JST) }

type failingStore struct{ err error }

func (f failingStore) RecordRuntimeSize(context.Context, string, string, int, time.Time) error {
	return f.err
}

// The recorder stores the size of the Watchlist (count and source) while the
// market is open, once per observation, and nothing outside the session; the
// watch list itself is only read.
func TestCheckRecordsTheSizeInSessionOnly(t *testing.T) {
	conn := newDB(t)
	list := &rankingwatch.Watchlist{}
	now := jst(8, 0)
	rec := watchlistsize.NewRecorder(watchlistsize.Config{
		Store: watchlistruntime.New(conn), Source: watchlistsize.SourceRanking, Size: list.Len,
		Now: func() time.Time { return now },
	})
	count := func() (n int) {
		t.Helper()
		if err := conn.QueryRow(`SELECT COUNT(*) FROM watch_list_runtime_days`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	list.Set([]string{"7203", "6758"})
	for _, at := range []time.Time{jst(8, 0), jst(11, 45), jst(15, 45)} { // before the open, the lunch break, after the close
		now = at
		if err := rec.Check(t.Context()); err != nil || count() != 0 {
			t.Fatalf("Check at %s: err %v, rows %d; want nothing recorded outside the session", at.Format("15:04"), err, count())
		}
	}

	now = jst(9, 1)
	if err := rec.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	list.Set([]string{"7203", "6758", "9984"})
	now = jst(10, 0)
	if err := rec.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	list.Set([]string{"7203"})
	now = jst(14, 0)
	if err := rec.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	var source string
	var entries, peak, samples int
	if err := conn.QueryRow(`SELECT source, entries, peak_entries, samples FROM watch_list_runtime_days WHERE session_date = '2026-10-21'`).
		Scan(&source, &entries, &peak, &samples); err != nil {
		t.Fatal(err)
	}
	if source != "ranking" || entries != 1 || peak != 3 || samples != 3 {
		t.Errorf("source/entries/peak/samples = %s/%d/%d/%d, want ranking/1/3/3 (the latest, the largest, the count)", source, entries, peak, samples)
	}
	if count() != 1 {
		t.Errorf("rows = %d, want the day updated in place", count())
	}
	if !list.Contains("7203") || list.Len() != 1 {
		t.Error("the observer changed the watch list")
	}
}

// A failed write (or a panicking size read) must not stop anything: Check
// returns the error for safego.Loop to log, the next pass works, and the watch
// list is left as it was.
func TestFailuresAreReturnedNotFatal(t *testing.T) {
	list := &rankingwatch.Watchlist{}
	list.Set([]string{"7203"})
	boom := errors.New("database is locked")
	rec := watchlistsize.NewRecorder(watchlistsize.Config{
		Store: failingStore{err: boom}, Source: watchlistsize.SourceRanking, Size: list.Len,
		Now: func() time.Time { return jst(10, 0) },
	})
	if err := rec.Check(t.Context()); !errors.Is(err, boom) {
		t.Fatalf("Check = %v, want the store's error", err)
	}
	if !list.Contains("7203") || list.Len() != 1 {
		t.Error("a failed record changed the watch list")
	}
}

// Run keeps observing through an error and through a panic in the size read:
// the loop is guarded, so a bad pass is logged and the next one still runs.
func TestRunSurvivesErrorsAndPanics(t *testing.T) {
	passes := make(chan int, 8)
	n := 0 // touched only by the Run goroutine: Size is its first step of every pass
	rec := watchlistsize.NewRecorder(watchlistsize.Config{
		Store: recordFunc(func() error {
			if n == 2 {
				return errors.New("database is locked")
			}
			return nil
		}),
		Source: watchlistsize.SourceRanking,
		Size: func() int {
			n++
			passes <- n
			if n == 1 {
				panic("size read failed")
			}
			return 1
		},
		Now:      func() time.Time { return jst(10, 0) },
		Interval: time.Millisecond,
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); rec.Run(ctx) }()
	for want := 1; want <= 4; want++ {
		select {
		case got := <-passes:
			if got != want {
				t.Fatalf("pass %d, want %d", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Run stopped after %d passes", want-1)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

type recordFunc func() error

func (f recordFunc) RecordRuntimeSize(context.Context, string, string, int, time.Time) error {
	return f()
}
