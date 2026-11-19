package disclosuresrctests

import (
	"context"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosuresrc"
)

// tables are every table the recorders write; a recorder that did nothing
// leaves all of them empty.
var tables = []string{"research_edinet_documents", "research_edinet_fetch_days", "research_tachibana_news", "research_disclosure_source_status"}

func TestEdinetSendsNoRequestWithoutAKeyOrWhileOff(t *testing.T) {
	for name, tc := range map[string]struct {
		key string
		s   settings
	}{
		"key unset, switch on":    {"", on(nil)},
		"key set, nothing stored": {testKey, settings{}},
		"key set, switch off":     {testKey, on(map[string]string{"disclosure_src.edinet.enabled": "0"})},
		"key set, damaged switch": {testKey, on(map[string]string{"disclosure_src.edinet.enabled": `"yes"`})},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newEdinetServer(t, fixtureList)
			db := openDB(t)
			res, err := newEdinetRecorder(db, srv.client(tc.key), tc.s, nil).Run(context.Background())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if n := srv.transport.requests(); n != 0 {
				t.Errorf("%d outbound requests, want 0", n)
			}
			if res.Requests != 0 || (!res.Disabled && !res.NotConfigured) {
				t.Errorf("result = %+v, want a no-op", res)
			}
			for _, table := range tables {
				if n := count(t, db, `SELECT COUNT(*) FROM `+table); n != 0 {
					t.Errorf("%s has %d rows after a no-op", table, n)
				}
			}
		})
	}
}

// fakeNews is a NewsFetcher that counts its calls.
type fakeNews struct {
	calls int
	items []disclosuresrc.NewsItem
	err   error
}

func (f *fakeNews) FetchNews(context.Context, time.Time) ([]disclosuresrc.NewsItem, error) {
	f.calls++
	return f.items, f.err
}

type creds bool

func (c creds) Configured() bool { return bool(c) }

func TestTachibanaNewsDoesNothingWithoutACredentialOrWhileOff(t *testing.T) {
	item := []disclosuresrc.NewsItem{{ID: "n1", Headline: "h", PublishedAt: now, Symbols: []string{"7203"}}}
	for name, tc := range map[string]struct {
		fetcher disclosuresrc.NewsFetcher
		creds   disclosuresrc.CredentialHolder
		s       settings
	}{
		"credential not configured": {&fakeNews{items: item}, creds(false), on(nil)},
		"no credential holder":      {&fakeNews{items: item}, nil, on(nil)},
		"no fetcher (production)":   {nil, creds(true), on(nil)},
		"switch off":                {&fakeNews{items: item}, creds(true), settings{}},
	} {
		t.Run(name, func(t *testing.T) {
			db := openDB(t)
			res, err := disclosuresrc.NewTachibanaRecorder(repo.NewRepository(db), tc.fetcher, tc.creds, tc.s.load).WithClock(func() time.Time { return now }).Run(context.Background())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if f, ok := tc.fetcher.(*fakeNews); ok && f.calls != 0 {
				t.Errorf("the fetcher was called %d times, want 0", f.calls)
			}
			if res.Requests != 0 || (!res.Disabled && !res.NotConfigured) {
				t.Errorf("result = %+v, want a no-op", res)
			}
			for _, table := range tables {
				if n := count(t, db, `SELECT COUNT(*) FROM `+table); n != 0 {
					t.Errorf("%s has %d rows after a no-op", table, n)
				}
			}
		})
	}
}
