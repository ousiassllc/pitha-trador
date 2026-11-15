package disclosuresrctests

import (
	"bytes"
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/disclosuresrc"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// fixed "now" of the server is 2026-10-18 12:00 JST (2026-10-18T03:00Z).
var (
	submitted = time.Date(2026, 10, 14, 6, 0, 0, 0, time.UTC)
	fetchedAt = time.Date(2026, 10, 18, 2, 30, 0, 0, time.UTC)
	failedAt  = time.Date(2026, 10, 18, 2, 45, 0, 0, time.UTC)
)

func seed(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	r := repo.NewRepository(w)
	ctx := t.Context()
	docs := []disclosuresrc.EdinetDocument{
		{DocID: "S100AAAA", Symbol: "7203", SecCode: "72030", Title: "有価証券報告書", FilerName: "x", DocTypeCode: "120", SubmittedAt: submitted},
		{DocID: "S100BBBB", Symbol: "6758", SecCode: "67580", Title: "大量保有報告書", FilerName: "y", DocTypeCode: "350", SubmittedAt: submitted.Add(time.Hour)},
		{DocID: "S100OLD0", Symbol: "9984", SecCode: "99840", Title: "old", SubmittedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}, // before the period
	}
	if _, err := r.UpsertEdinetDocuments(ctx, docs, fetchedAt); err != nil {
		t.Fatal(err)
	}
	// Two requests today (both counted), one failed.
	for range 2 {
		if ok, err := r.BeginRequest(ctx, disclosuresrc.SourceEdinet, "2026-10-18", 30, fetchedAt); err != nil || !ok {
			t.Fatalf("BeginRequest = %v, %v", ok, err)
		}
	}
	if err := r.RecordSuccess(ctx, disclosuresrc.SourceEdinet, fetchedAt); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordFailure(ctx, disclosuresrc.SourceEdinet, "http_429", failedAt); err != nil {
		t.Fatal(err)
	}
	return path, w
}

func TestStatusReturnsCountsLastFetchAndFailures(t *testing.T) {
	path, w := seed(t)
	testkit.Setting(t, w, "disclosure_src.edinet.enabled", "1")
	testkit.Setting(t, w, "disclosure_src.edinet.daily_request_cap", "12")

	d := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, period).data(t)
	if len(d.Sources) != 2 || d.Sources[0].Source != "edinet" || d.Sources[1].Source != "tachibana_news" {
		t.Fatalf("sources = %+v, want edinet then tachibana_news", d.Sources)
	}
	e := d.Sources[0]
	if !e.Enabled || e.DailyRequestCap != 12 {
		t.Errorf("enabled/cap = %v/%d, want true/12", e.Enabled, e.DailyRequestCap)
	}
	if e.RecordedTotal != 3 || e.RecordedInPeriod != 2 {
		t.Errorf("recorded total/in period = %d/%d, want 3/2", e.RecordedTotal, e.RecordedInPeriod)
	}
	if e.RequestCount != 2 || e.RequestsToday != 2 || e.FailureCount != 1 || e.LastFailureCode != "http_429" {
		t.Errorf("requests/today/failures/code = %d/%d/%d/%q, want 2/2/1/http_429", e.RequestCount, e.RequestsToday, e.FailureCount, e.LastFailureCode)
	}
	if e.LastFetchedAt == nil || *e.LastFetchedAt != "2026-10-18T02:30:00Z" || e.LastSuccessAt == nil || *e.LastSuccessAt != "2026-10-18T02:30:00Z" ||
		e.LastFailureAt == nil || *e.LastFailureAt != "2026-10-18T02:45:00Z" {
		t.Errorf("last fetched/success/failure = %v/%v/%v", e.LastFetchedAt, e.LastSuccessAt, e.LastFailureAt)
	}
	if e.OldestAt == nil || *e.OldestAt != "2026-09-01T00:00:00Z" || e.NewestAt == nil || *e.NewestAt != "2026-10-14T07:00:00Z" {
		t.Errorf("oldest/newest = %v/%v", e.OldestAt, e.NewestAt)
	}

	n := d.Sources[1] // never fetched, switch not stored: off, empty
	if n.Enabled || n.RecordedTotal != 0 || n.RequestCount != 0 || n.LastFetchedAt != nil || n.LastFailureCode != "" {
		t.Errorf("tachibana_news = %+v, want off and empty", n)
	}
}

func TestStatusDefaultsAreOffAndYesterdaysRequestCountIsNotToday(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	r := repo.NewRepository(w)
	if ok, err := r.BeginRequest(t.Context(), disclosuresrc.SourceEdinet, "2026-10-17", 30, fetchedAt.Add(-24*time.Hour)); err != nil || !ok {
		t.Fatalf("BeginRequest = %v, %v", ok, err)
	}
	d := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, period).data(t)
	for _, s := range d.Sources {
		if s.Enabled {
			t.Errorf("%s is on with nothing stored", s.Source)
		}
	}
	if e := d.Sources[0]; e.RequestCount != 1 || e.RequestsToday != 0 || e.DailyRequestCap != 30 {
		t.Errorf("edinet = %+v, want request_count 1, requests_today 0 (an earlier day), cap 30", e)
	}
}

func TestStatusReturnsNoSecretsOrRecordedText(t *testing.T) {
	path, w := seed(t)
	testkit.Setting(t, w, "disclosure_src.edinet.enabled", "1")
	res := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, period)
	for _, forbidden := range []string{"有価証券報告書", "大量保有報告書", "S100AAAA", "Subscription-Key", "test-key-xxxx", "7203", "secrets"} {
		if bytes.Contains(res.body, []byte(forbidden)) {
			t.Errorf("the response contains %q", forbidden)
		}
	}
}

func TestStatusArgumentsAreStrictAndTheConnectionIsReadOnly(t *testing.T) {
	path, w := seed(t)
	s := newTestServer(t, paperresultsmcp.Options{DBPath: path})
	for name, a := range map[string]map[string]any{
		"an unknown argument":  {"from": "2026-10-10", "to": "2026-10-18", "enable": 1},
		"a switch as argument": {"from": "2026-10-10", "to": "2026-10-18", "disclosure_src.edinet.enabled": 1},
	} {
		if r := s.call(t, a); r.status != http.StatusUnprocessableEntity || !strings.Contains(string(r.body), "invalid_argument") {
			t.Errorf("%s: status %d body %s, want 422 invalid_argument", name, r.status, r.body)
		}
	}
	db, err := s.DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE research_disclosure_source_status SET request_count = 0`,
		`DELETE FROM research_edinet_documents`,
		`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('disclosure_src.edinet.enabled', '1', 't')`,
	} {
		if _, err := db.Exec(q); err == nil {
			t.Errorf("write succeeded on the MCP connection: %s", testkit.FirstLine(q))
		}
	}
	var on int
	if err := w.QueryRow(`SELECT COUNT(*) FROM runtime_settings WHERE key LIKE 'disclosure_src.%'`).Scan(&on); err != nil || on != 0 {
		t.Errorf("the tool left %d disclosure_src settings, err %v", on, err)
	}
}
