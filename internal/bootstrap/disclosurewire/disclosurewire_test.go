package disclosurewire

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

type lunaStub struct{}

func (lunaStub) Classify(context.Context, assist.NewsItem) (assist.Classification, error) {
	return assist.Classification{Sentiment: domain.NewsSentimentNeutral, EventType: domain.NewsEventEarnings, Summary: "s"}, nil
}

type oneSymbol struct{}

func (oneSymbol) NewsSymbols(context.Context) ([]string, error) { return []string{"7203"}, nil }

// waitUntil polls cond until it holds, failing after a bound: the recorder and
// the evaluator run on their own goroutines, so the test waits for their effect.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func count(t *testing.T, conn *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// End to end against a fake やのしん server and a temporary database: one News
// Ingest poll sends exactly one request per symbol, the recording feed stores
// the response, and the evaluator stores a verdict for a Jev decision made
// before it. The real やのしん is never dialled.
func TestWire_RecordsTheNewsIngestFetchAndEvaluatesTheDecision(t *testing.T) {
	var requests atomic.Int32
	published := time.Now().Add(-3 * time.Hour).In(time.FixedZone("JST", 9*3600)).Format(time.DateTime)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = fmt.Fprintf(w, `{"items":[{"Tdnet":{"id":"1","title":"2026年3月期 第2四半期決算短信〔日本基準〕（連結）","document_url":"https://example.test/1.pdf","pubdate":%q}}]}`, published)
	}))
	t.Cleanup(srv.Close)

	path := filepath.Join(t.TempDir(), "pitha.db")
	app, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	if _, err := app.Exec(`INSERT INTO instruments (id, symbol, name, market, is_active) VALUES (1, '7203', 'x', 'TSE Prime', 1)`); err != nil {
		t.Fatal(err)
	}
	decidedAt := time.Now().Add(-10 * time.Minute)
	if _, err := app.Exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (1, 1, '7203', ?, 'trader', 'h', '{}', 'trader-v3', '{}', 'LONG', 0.7, 5, 'm')`, sqlutil.FormatTime(decidedAt)); err != nil {
		t.Fatal(err)
	}

	feed, wire := Wrap(newsfeed.NewYanoshinClient(newsfeed.YanoshinConfig{BaseURL: srv.URL}), false, path)
	wire.every = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wire.Start(ctx, &wg)
	news := newsfeed.NewService(feed, lunaStub{}, oneSymbol{})
	if err := news.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("the fake やのしん saw %d requests for one poll of one symbol, want 1", got)
	}

	waitUntil(t, "the evaluator's record", func() bool {
		return count(t, app, `SELECT COUNT(*) FROM disclosure_skip_records WHERE jev_decision_id = 1`) == 1
	})
	var verdict, title string
	if err := app.QueryRow(`SELECT r.verdict, m.title FROM disclosure_skip_records r JOIN disclosure_skip_matches m ON m.record_id = r.id`).Scan(&verdict, &title); err != nil {
		t.Fatal(err)
	}
	if verdict != "skip" || title == "" {
		t.Fatalf("verdict = %q, title = %q; want a skip on the earnings report", verdict, title)
	}
	if got := count(t, app, `SELECT COUNT(*) FROM disclosure_fetches WHERE outcome = 'ok'`); got != 1 {
		t.Fatalf("recorded fetches = %d, want 1", got)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("the recording added requests: %d", got)
	}

	cancel()
	wg.Wait() // Start's goroutines end with ctx and close their connection
}

// A custom feed (NEWS_FEED_URL) or a missing database path leaves the feed as it
// is and records nothing; a nil Wire does nothing at Start.
func TestWrap_LeavesCustomFeedsAndMissingPathsAlone(t *testing.T) {
	inner := newsfeed.NewYanoshinClient(newsfeed.YanoshinConfig{BaseURL: "http://127.0.0.1:1"})
	if feed, wire := Wrap(inner, true, "/tmp/x.db"); feed != newsfeed.Feed(inner) || wire != nil {
		t.Fatalf("custom feed: got %v, %v", feed, wire)
	}
	if feed, wire := Wrap(inner, false, ""); feed != newsfeed.Feed(inner) || wire != nil {
		t.Fatalf("no path: got %v, %v", feed, wire)
	}
	var wg sync.WaitGroup
	(*Wire)(nil).Start(context.Background(), &wg)
	wg.Wait()
}

// A database that cannot be opened only switches the record off: Start
// returns, the wrapped feed still returns the inner result.
func TestWire_StartWithoutADatabaseSwitchesTheRecordOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"items":[]}`)) }))
	t.Cleanup(srv.Close)
	feed, wire := Wrap(newsfeed.NewYanoshinClient(newsfeed.YanoshinConfig{BaseURL: srv.URL}), false, filepath.Join(t.TempDir(), "missing.db"))
	var wg sync.WaitGroup
	wire.Start(context.Background(), &wg)
	wg.Wait()
	items, err := feed.Fetch(context.Background(), "7203")
	if err != nil || len(items) != 0 {
		t.Fatalf("Fetch = %v, %v", items, err)
	}
}
