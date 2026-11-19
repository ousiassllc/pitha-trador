// Package disclosuresrctests holds the tests of the disclosure source recorders
// (issue #1096): EDINET's document list and the 立花 news. They live apart from
// the code so each file stays small. Everything runs against httptest servers
// and fakes: no real service is ever called, and the keys are obviously fake.
package disclosuresrctests

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/disclosuresrcsettings"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosuresrc/edinet"
)

const testKey = "test-key-xxxx"

// now is the fixed clock: Friday 2026-11-13 19:00 JST.
var now = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// settings is an in-memory runtime_settings.
type settings map[string]string

func (s settings) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s[key]
	return v, ok, nil
}

func (s settings) load(ctx context.Context) (disclosuresrcsettings.Settings, error) {
	return disclosuresrcsettings.Load(ctx, s)
}

// countingTransport counts every request that reaches the network layer and
// records its path and query, then delegates to the real transport.
type countingTransport struct {
	mu    sync.Mutex
	paths []string
	urls  []string
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.paths = append(c.paths, r.URL.Path)
	c.urls = append(c.urls, r.URL.String())
	c.mu.Unlock()
	return http.DefaultTransport.RoundTrip(r)
}

func (c *countingTransport) requests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.paths)
}

// edinetServer serves documents.json by date: listFor(date) is the JSON body,
// status the HTTP status (200 by default).
type edinetServer struct {
	*httptest.Server
	transport *countingTransport
	status    int
	listFor   func(date string) string
}

func newEdinetServer(t *testing.T, listFor func(date string) string) *edinetServer {
	t.Helper()
	s := &edinetServer{transport: &countingTransport{}, status: http.StatusOK, listFor: listFor}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("Subscription-Key") != testKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if s.status != http.StatusOK {
			w.WriteHeader(s.status)
			return
		}
		_, _ = w.Write([]byte(s.listFor(r.URL.Query().Get("date"))))
	}))
	t.Cleanup(s.Close)
	return s
}

// client is an EDINET client of the server with key ("" = not set), going
// through the counting transport.
func (s *edinetServer) client(key string) *edinet.Client {
	return edinet.New(edinet.Config{BaseURL: s.URL + "/api/v2", APIKey: key, HTTPClient: &http.Client{Transport: s.transport}})
}

// doc is one result of the EDINET list.
func doc(id, secCode, submit, desc string) string {
	return fmt.Sprintf(`{"docID":%q,"edinetCode":"E00001","secCode":%s,"filerName":"テスト株式会社","docTypeCode":"120","submitDateTime":%q,"docDescription":%q}`,
		id, jsonNullable(secCode), submit, desc)
}

func jsonNullable(s string) string {
	if s == "" {
		return "null"
	}
	return fmt.Sprintf("%q", s)
}

func list(docs ...string) string {
	return `{"metadata":{"status":"200","message":"OK"},"results":[` + strings.Join(docs, ",") + `]}`
}

// fixtureList is the list of every day: a company's report (secCode 72030 ->
// 7203), a fund without a secCode, and a document with an unreadable time.
func fixtureList(date string) string {
	return list(
		doc("S-"+date+"-A", "72030", date+" 15:00", "有価証券報告書"),
		doc("S-"+date+"-B", "", date+" 15:10", "投資信託の報告書"),
		doc("S-"+date+"-C", "67580", "not a time", "壊れた時刻"),
	)
}

func newEdinetRecorder(db *sql.DB, client disclosuresrc.EdinetLister, s settings, pauses *int) *disclosuresrc.EdinetRecorder {
	return disclosuresrc.NewEdinetRecorder(repo.NewRepository(db), client, s.load).WithClock(func() time.Time { return now }).
		WithPause(func(context.Context, time.Duration) error {
			if pauses != nil {
				*pauses++
			}
			return nil
		})
}

func on(extra map[string]string) settings {
	s := settings{"disclosure_src.edinet.enabled": "1", "disclosure_src.tachibana_news.enabled": "1", "disclosure_src.edinet.lookback_days": "3"}
	for k, v := range extra {
		s[k] = v
	}
	return s
}
