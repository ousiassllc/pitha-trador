package disclosuresrctests

import (
	"context"
	"errors"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosuresrc"
)

func TestTachibanaNewsRecordsHeadlineTimeAndRelatedSymbols(t *testing.T) {
	db := openDB(t)
	published := time.Date(2026, 11, 13, 5, 30, 0, 0, time.UTC)
	f := &fakeNews{items: []disclosuresrc.NewsItem{
		{ID: "n1", Headline: "トヨタ、上方修正", PublishedAt: published, Symbols: []string{"7203", "6758", "7203"}},
		{ID: "n2", Headline: "市況ニュース", PublishedAt: published, Symbols: nil},
		{ID: "n3", Headline: "5桁コード", PublishedAt: published, Symbols: []string{"72030"}},
		{ID: "", Headline: "id なしは捨てる", PublishedAt: published},
	}}
	rec := disclosuresrc.NewTachibanaRecorder(repo.NewRepository(db), f, creds(true), on(nil).load).WithClock(func() time.Time { return now })

	res, err := rec.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Requests != 1 || res.Rows != 4 || res.Failures != 0 {
		t.Fatalf("result = %+v, want 1 request and 4 rows", res)
	}
	for _, c := range []struct {
		q    string
		want int
	}{
		{`SELECT COUNT(*) FROM research_tachibana_news WHERE news_id = 'n1' AND symbol IN ('7203', '6758')`, 2},
		{`SELECT COUNT(*) FROM research_tachibana_news WHERE news_id = 'n2' AND symbol = ''`, 1},
		{`SELECT COUNT(*) FROM research_tachibana_news WHERE news_id = 'n3' AND symbol = '7203'`, 1},
		{`SELECT COUNT(*) FROM research_tachibana_news`, 4},
	} {
		if got := count(t, db, c.q); got != c.want {
			t.Errorf("%s = %d, want %d", c.q, got, c.want)
		}
	}
	var at, headline string
	if err := db.QueryRow(`SELECT published_at, headline FROM research_tachibana_news WHERE news_id = 'n1' AND symbol = '6758'`).Scan(&at, &headline); err != nil {
		t.Fatal(err)
	}
	if at != "2026-11-13T05:30:00.000000000Z" || headline != "トヨタ、上方修正" {
		t.Errorf("row = %q %q", at, headline)
	}

	// Seeing the same news again changes no row count (idempotent).
	if _, err := rec.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_tachibana_news`); n != 4 {
		t.Errorf("%d rows after a second fetch, want 4", n)
	}
	var requests int
	if err := db.QueryRow(`SELECT request_count FROM research_disclosure_source_status WHERE source = 'tachibana_news'`).Scan(&requests); err != nil || requests != 2 {
		t.Errorf("request_count = %d (%v), want 2", requests, err)
	}
}

type codedError struct{}

func (codedError) Error() string {
	return "boom: secret-token-xyz at https://example.invalid/?key=secret-token-xyz"
}
func (codedError) FailureCode() string { return "http_403" }

func TestTachibanaNewsRecordsAFailureByCodeNeverByText(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code string
	}{
		"a coded error":   {codedError{}, "http_403"},
		"any other error": {errors.New("dial tcp: secret-token-xyz"), "transport"},
	} {
		t.Run(name, func(t *testing.T) {
			db := openDB(t)
			rec := disclosuresrc.NewTachibanaRecorder(repo.NewRepository(db), &fakeNews{err: tc.err}, creds(true), on(nil).load).WithClock(func() time.Time { return now })
			res, err := rec.Run(context.Background())
			if err != nil || res.Failures != 1 {
				t.Fatalf("result %+v, err %v; want a recorded failure", res, err)
			}
			var code string
			var failures int
			if err := db.QueryRow(`SELECT last_failure_code, failure_count FROM research_disclosure_source_status WHERE source = 'tachibana_news'`).Scan(&code, &failures); err != nil {
				t.Fatal(err)
			}
			if code != tc.code || failures != 1 {
				t.Errorf("status = %q / %d, want %q / 1", code, failures, tc.code)
			}
			if n := count(t, db, `SELECT COUNT(*) FROM research_disclosure_source_status WHERE last_failure_code LIKE '%secret%'`); n != 0 {
				t.Error("an error text reached the status row")
			}
		})
	}
}

func TestTachibanaNewsStopsAtTheDailyRequestCap(t *testing.T) {
	db := openDB(t)
	f := &fakeNews{}
	s := on(map[string]string{"disclosure_src.tachibana_news.daily_request_cap": "1"})
	rec := disclosuresrc.NewTachibanaRecorder(repo.NewRepository(db), f, creds(true), s.load).WithClock(func() time.Time { return now })
	for i := range 3 {
		if _, err := rec.Run(context.Background()); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if f.calls != 1 {
		t.Errorf("the fetcher was called %d times, want 1 (the cap)", f.calls)
	}
}
