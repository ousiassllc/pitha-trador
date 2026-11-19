package disclosuresrcwire

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

const testKey = "test-key-xxxx"

// 2026-11-13 19:00 JST (after RunFrom) and 12:00 JST (before it).
var (
	evening = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	noon    = time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC)
)

func setup(t *testing.T) (*sql.DB, *system.RuntimeSettingsRepository) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, system.NewRuntimeSettingsRepository(db)
}

func edinetServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"metadata":{"status":"200"},"results":[{"docID":"S1","secCode":"72030","submitDateTime":"2026-11-13 15:00","docDescription":"x"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEdinetJobDoesNothingByDefault(t *testing.T) {
	var hits atomic.Int32
	srv := edinetServer(t, &hits)
	db, settings := setup(t)
	job := NewEdinetJob(db, settings, testKey, srv.URL) // a key is set, but nothing is switched on
	job.now = func() time.Time { return evening }

	if err := job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Run = %v, want ErrSkipped", err)
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests with the switch off, want 0", hits.Load())
	}
	for _, table := range []string{"research_edinet_documents", "research_disclosure_source_status", "runtime_settings"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if table != "runtime_settings" && n != 0 {
			t.Errorf("%s has %d rows after a skipped run", table, n)
		}
	}
}

func TestEdinetJobWaitsForTheEveningAndNeedsAKey(t *testing.T) {
	var hits atomic.Int32
	srv := edinetServer(t, &hits)
	db, settings := setup(t)
	if err := settings.Set(context.Background(), "disclosure_src.edinet.enabled", "1", evening); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), "disclosure_src.edinet.lookback_days", "1", evening); err != nil {
		t.Fatal(err)
	}

	before := NewEdinetJob(db, settings, testKey, srv.URL)
	before.now = func() time.Time { return noon }
	if err := before.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Errorf("Run before 18:00 JST = %v, want ErrSkipped", err)
	}
	noKey := NewEdinetJob(db, settings, "", srv.URL)
	noKey.now = func() time.Time { return evening }
	if err := noKey.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Errorf("Run without a key = %v, want ErrSkipped", err)
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests before the evening or without a key, want 0", hits.Load())
	}

	job := NewEdinetJob(db, settings, testKey, srv.URL)
	job.now = func() time.Time { return evening }
	if err := job.Run(context.Background()); err != nil {
		t.Fatalf("Run = %v, want success", err)
	}
	if hits.Load() != 1 {
		t.Errorf("%d requests, want the one list of the day", hits.Load())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM research_edinet_documents WHERE symbol = '7203'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("%d documents recorded (%v), want 1", n, err)
	}
}

func TestOnlyTheEdinetTaskIsRegisteredAndTheTachibanaJobNeedsACredential(t *testing.T) {
	db, settings := setup(t)
	tasks := Tasks(db, settings, "")
	if len(tasks) != 1 || tasks[0].Name != EdinetTaskName {
		t.Fatalf("tasks = %+v, want only the EDINET task (the 立花 news waits for issue #725)", tasks)
	}

	// With the switch on but a credential that is not configured, the 立花 job still does nothing.
	if err := settings.Set(context.Background(), "disclosure_src.tachibana_news.enabled", "1", evening); err != nil {
		t.Fatal(err)
	}
	f := &countingFetcher{}
	job := NewTachibanaNewsJob(db, settings, f, unconfigured{})
	job.now = func() time.Time { return evening }
	if err := job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Errorf("Run = %v, want ErrSkipped", err)
	}
	if f.calls != 0 {
		t.Errorf("the fetcher was called %d times, want 0", f.calls)
	}
}

type unconfigured struct{}

func (unconfigured) Configured() bool { return false }

type countingFetcher struct{ calls int }

func (f *countingFetcher) FetchNews(context.Context, time.Time) ([]disclosuresrc.NewsItem, error) {
	f.calls++
	return nil, nil
}
