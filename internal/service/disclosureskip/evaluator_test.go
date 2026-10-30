package disclosureskip_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosureskip"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, name, market, is_active) VALUES (1, '7203', 'x', 'TSE Prime', 1)`); err != nil {
		t.Fatal(err)
	}
	return conn
}

func insertDecision(t *testing.T, conn *sql.DB, id int, kind, direction string, at time.Time) {
	t.Helper()
	if _, err := conn.Exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (?, 1, ?, ?, ?, 'h', '{}', 'trader-v3', '{}', ?, 0.7, 5, 'm')`,
		id, sym, sqlutil.FormatTime(at), kind, direction); err != nil {
		t.Fatal(err)
	}
}

type recordRow struct {
	verdict, reason string
	matches         int
}

func recordOf(t *testing.T, conn *sql.DB, decisionID int) (recordRow, bool) {
	t.Helper()
	var (
		r      recordRow
		reason sql.NullString
	)
	err := conn.QueryRow(`SELECT r.verdict, r.unknown_reason, (SELECT COUNT(*) FROM disclosure_skip_matches m WHERE m.record_id = r.id)
		FROM disclosure_skip_records r WHERE r.jev_decision_id = ?`, decisionID).Scan(&r.verdict, &reason, &r.matches)
	if err == sql.ErrNoRows {
		return r, false
	}
	if err != nil {
		t.Fatal(err)
	}
	r.reason = reason.String
	return r, true
}

// fetchInto stores one successful fetch through the store's writer, as the
// recorder's background writer does.
func fetchInto(t *testing.T, store *disclosureskip.Store, at time.Time, items ...assist.NewsItem) {
	t.Helper()
	sink := newCollector()
	feed := disclosureskip.NewRecordingFeed(&scriptedFeed{items: map[string][]assist.NewsItem{sym: items}}, 10, sink, disclosureskip.WithClock(func() time.Time { return at }))
	runWriter(t, feed)
	if _, err := feed.Fetch(context.Background(), sym); err != nil {
		t.Fatal(err)
	}
	waitFor(t, sink.got)
	if err := store.WriteFetch(context.Background(), sink.events[len(sink.events)-1]); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluator_RecordsOneVerdictPerEntryCandidateAfterTheFact(t *testing.T) {
	conn := openDB(t)
	store := disclosureskip.NewStore(conn)
	decided := jstAt(10, 14, 10, 0, 0)
	now := decided.Add(20 * time.Minute)
	ev := disclosureskip.NewEvaluator(store, weekdaysOnly, disclosureskip.WithEvaluatorClock(func() time.Time { return now }))

	insertDecision(t, conn, 1, "trader", "LONG", decided)
	insertDecision(t, conn, 2, "trader", "SHORT", decided.Add(time.Minute))
	insertDecision(t, conn, 3, "trader", "NONE", decided) // not an entry candidate
	insertDecision(t, conn, 4, "scout", "LONG", decided)  // not a trader decision
	before := countRows(t, conn, "jev_decisions")

	// Nothing fetched after the decisions yet: the verdict is still open.
	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := recordOf(t, conn, 1); ok {
		t.Fatal("a decision without a covering fetch was recorded before the give-up time")
	}

	// A successful fetch with the earnings disclosure of the day before arrives.
	fetchInto(t, store, decided.Add(10*time.Minute), tdnet("a", tanshin, jstAt(10, 13, 15, 30, 0)), tdnet("b", "配当予想の修正に関するお知らせ", jstAt(10, 14, 9, 0, 0)))
	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2} {
		r, ok := recordOf(t, conn, id)
		if !ok || r.verdict != "skip" || r.matches != 1 {
			t.Fatalf("decision %d: record = %+v, %v; want a skip with one match", id, r, ok)
		}
	}
	for _, id := range []int{3, 4} {
		if _, ok := recordOf(t, conn, id); ok {
			t.Fatalf("decision %d (not an entry candidate) was recorded", id)
		}
	}
	var title, kind string
	if err := conn.QueryRow(`SELECT title, kind FROM disclosure_skip_matches ORDER BY id LIMIT 1`).Scan(&title, &kind); err != nil || title != tanshin || kind != "earnings" {
		t.Fatalf("match = %q %q, %v", title, kind, err)
	}

	// A second cycle changes nothing, and jev_decisions was never written.
	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, conn, "disclosure_skip_records"); got != 2 {
		t.Fatalf("records = %d, want 2", got)
	}
	if got := countRows(t, conn, "jev_decisions"); got != before {
		t.Fatalf("jev_decisions rows changed: %d -> %d", before, got)
	}
}

func TestEvaluator_FeedGapBecomesUnknownOnlyAfterTheGiveUpTime(t *testing.T) {
	conn := openDB(t)
	store := disclosureskip.NewStore(conn)
	decided := jstAt(10, 14, 10, 0, 0)
	now := decided.Add(10 * time.Minute)
	ev := disclosureskip.NewEvaluator(store, weekdaysOnly, disclosureskip.WithEvaluatorClock(func() time.Time { return now }), disclosureskip.WithGiveUp(30*time.Minute))
	insertDecision(t, conn, 1, "trader", "LONG", decided)

	// A failed fetch is no coverage.
	if err := store.WriteFetch(context.Background(), disclosureskip.FetchEvent{Symbol: sym, At: decided.Add(6 * time.Minute), OK: false}); err != nil {
		t.Fatal(err)
	}
	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := recordOf(t, conn, 1); ok {
		t.Fatal("recorded as unknown before the give-up time")
	}

	now = decided.Add(31 * time.Minute)
	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r, ok := recordOf(t, conn, 1); !ok || r.verdict != "unknown" || r.reason != "no_fetch" || r.matches != 0 {
		t.Fatalf("record = %+v, %v; want unknown / no_fetch", r, ok)
	}
}

func TestEvaluator_TruncatedFetchThatDoesNotReachTheWindowIsUnknown(t *testing.T) {
	conn := openDB(t)
	store := disclosureskip.NewStore(conn)
	decided := jstAt(10, 14, 10, 0, 0)
	now := decided.Add(time.Hour)
	ev := disclosureskip.NewEvaluator(store, weekdaysOnly, disclosureskip.WithEvaluatorClock(func() time.Time { return now }))
	insertDecision(t, conn, 1, "trader", "LONG", decided)

	// Ten disclosures published this morning fill the response: the window
	// (from yesterday 15:00) is not reached.
	var items []assist.NewsItem
	for i := range 10 {
		items = append(items, tdnet(string(rune('a'+i)), "株主総会招集ご通知", jstAt(10, 14, 9, i, 0)))
	}
	fetchInto(t, store, decided.Add(6*time.Minute), items...)
	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r, ok := recordOf(t, conn, 1); !ok || r.verdict != "unknown" || r.reason != "incomplete_fetch" {
		t.Fatalf("record = %+v, %v; want unknown / incomplete_fetch", r, ok)
	}
}

func TestEvaluator_CoveredWithoutAMatchIsNoSkipAndOldFetchesArePruned(t *testing.T) {
	conn := openDB(t)
	store := disclosureskip.NewStore(conn)
	decided := jstAt(10, 14, 10, 0, 0)
	now := decided.Add(8 * 24 * time.Hour)
	ev := disclosureskip.NewEvaluator(store, weekdaysOnly, disclosureskip.WithEvaluatorClock(func() time.Time { return now }))
	insertDecision(t, conn, 1, "trader", "LONG", decided)
	fetchInto(t, store, decided.Add(6*time.Minute), tdnet("a", tanshin, jstAt(10, 13, 14, 59, 0)))

	// The decision is older than the look-back, so it is not evaluated at all
	// (the app was not running to record it), but the old fetch is pruned.
	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := recordOf(t, conn, 1); ok {
		t.Fatal("a decision older than the look-back was recorded")
	}
	if got := countRows(t, conn, "disclosure_fetches"); got != 0 {
		t.Fatalf("fetches = %d, want the 8-day-old fetch pruned", got)
	}
	if got := countRows(t, conn, "disclosure_items"); got != 1 {
		t.Fatalf("disclosure items = %d, want them kept", got)
	}

	now = decided.Add(10 * time.Minute) // the same decision, evaluated in time with a fresh fetch
	fetchInto(t, store, decided.Add(6*time.Minute), tdnet("a", tanshin, jstAt(10, 13, 14, 59, 0)))
	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r, ok := recordOf(t, conn, 1); !ok || r.verdict != "no_skip" {
		t.Fatalf("record = %+v, %v; want no_skip (14:59 is before the window)", r, ok)
	}
}

func TestStore_WriteFetchIsIdempotentPerDisclosure(t *testing.T) {
	conn := openDB(t)
	store := disclosureskip.NewStore(conn)
	at := jstAt(10, 14, 10, 0, 0)
	for range 3 {
		fetchInto(t, store, at, tdnet("a", tanshin, jstAt(10, 14, 9, 0, 0)))
	}
	if got := countRows(t, conn, "disclosure_items"); got != 1 {
		t.Fatalf("disclosure items = %d, want 1 (deduplicated by symbol and id)", got)
	}
	if got := countRows(t, conn, "disclosure_fetches"); got != 3 {
		t.Fatalf("fetches = %d, want 3", got)
	}
	var link string
	if err := conn.QueryRow(`SELECT document_url FROM disclosure_items`).Scan(&link); err != nil || link != "https://example.test/a.pdf" {
		t.Fatalf("document_url = %q, %v", link, err)
	}
}

func countRows(t *testing.T, conn *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
