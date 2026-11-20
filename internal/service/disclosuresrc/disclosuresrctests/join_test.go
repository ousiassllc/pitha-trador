package disclosuresrctests

import (
	"context"
	"testing"
	"time"

	dom "github.com/ousiassllc/pitha-trador/internal/domain/disclosuresrc"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosuresrc"
)

// The recorded rows join disclosure_items (issue #935, やのしん TDnet) on the same
// symbol and the same time: both store the 4-character code and a UTC RFC3339
// time of the same fixed width. This inserts a disclosure_items row the way
// disclosureskip.Store does and joins it with what the recorders stored from
// mocks, through the documented view and through the tables directly.
func TestRecordedRowsJoinDisclosureItemsBySymbolAndTime(t *testing.T) {
	srv := newEdinetServer(t, func(date string) string {
		return list(doc("S-"+date, "72030", date+" 15:00", "有価証券報告書"))
	})
	db := openDB(t)
	ctx := context.Background()
	if _, err := newEdinetRecorder(db, srv.client(testKey), on(map[string]string{"disclosure_src.edinet.lookback_days": "1"}), nil).Run(ctx); err != nil {
		t.Fatal(err)
	}
	published := time.Date(2026, 11, 13, 5, 30, 0, 0, time.UTC) // 14:30 JST, same day as the EDINET document
	news := &fakeNews{items: []disclosuresrc.NewsItem{{ID: "n1", Headline: "上方修正", PublishedAt: published, Symbols: []string{"7203"}}}}
	if _, err := disclosuresrc.NewTachibanaRecorder(repo.NewRepository(db), news, creds(true), on(nil).load).WithClock(func() time.Time { return now }).Run(ctx); err != nil {
		t.Fatal(err)
	}
	// A TDnet disclosure of the same company, as disclosureskip.Store writes it.
	if _, err := db.Exec(`INSERT INTO disclosure_items (symbol, source_id, published_at, title, document_url, first_seen_at) VALUES (?, 'tdnet-1', ?, '業績予想の修正', NULL, ?)`,
		"7203", sqlutil.FormatTime(published), sqlutil.FormatTime(now)); err != nil {
		t.Fatal(err)
	}

	// Through the view: the other sources' rows of the symbol within 2 hours after
	// each TDnet disclosure.
	rows, err := db.Query(`
SELECT e.source, e.ref_id FROM disclosure_items i
JOIN research_disclosure_source_events_v e ON e.symbol = i.symbol
	AND e.event_at >= i.published_at AND e.event_at < ?
ORDER BY e.source`, sqlutil.FormatTime(published.Add(2*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var source, ref string
		if err := rows.Scan(&source, &ref); err != nil {
			t.Fatal(err)
		}
		got = append(got, source+":"+ref)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// The news (14:30 JST, same minute) is within the window; the EDINET filing
	// (15:00 JST) is 30 minutes later, also within it.
	if len(got) != 2 || got[0] != "edinet:S-2026-11-13" || got[1] != "tachibana_news:n1" {
		t.Errorf("joined rows = %v, want the EDINET filing and the news of 7203", got)
	}

	// And through the tables, on the same symbol and an exact time.
	if n := count(t, db, `SELECT COUNT(*) FROM disclosure_items i JOIN research_tachibana_news n ON n.symbol = i.symbol AND n.published_at = i.published_at`); n != 1 {
		t.Errorf("exact-time join on the news = %d, want 1", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM disclosure_items i JOIN research_edinet_documents d ON d.symbol = i.symbol AND d.submit_day = '2026-11-13'`); n != 1 {
		t.Errorf("same-day join on the EDINET documents = %d, want 1", n)
	}
	// The recorders wrote nothing into disclosure_items.
	if n := count(t, db, `SELECT COUNT(*) FROM disclosure_items`); n != 1 {
		t.Errorf("disclosure_items has %d rows, want only the one the test inserted", n)
	}
}

func TestRetentionDeletesOldRecordedRowsOnly(t *testing.T) {
	db := openDB(t)
	r := repo.NewRepository(db)
	ctx := context.Background()
	old := now.AddDate(0, 0, -40)
	if _, err := r.UpsertTachibanaNews(ctx, []dom.TachibanaNews{
		{NewsID: "old", Symbol: "7203", Headline: "h", PublishedAt: old},
		{NewsID: "new", Symbol: "7203", Headline: "h", PublishedAt: now},
	}, now); err != nil {
		t.Fatal(err)
	}
	f := &fakeNews{}
	s := on(map[string]string{"disclosure_src.retention_days": "30"})
	if _, err := disclosuresrc.NewTachibanaRecorder(r, f, creds(true), s.load).WithClock(func() time.Time { return now }).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_tachibana_news WHERE news_id = 'old'`); n != 0 {
		t.Errorf("the 40-day-old news survived a 30-day retention")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_tachibana_news WHERE news_id = 'new'`); n != 1 {
		t.Errorf("the new news was deleted")
	}
}
