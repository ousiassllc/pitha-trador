package disclosuresrc_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/disclosuresrc"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

var at = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC) // 19:00 JST

func open(t *testing.T) (*repo.Repository, *sql.DB) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return repo.NewRepository(db), db
}

func TestBeginRequestCountsAgainstTheCapPerJSTDay(t *testing.T) {
	r, db := open(t)
	ctx := t.Context()
	for i := range 3 {
		ok, err := r.BeginRequest(ctx, disclosuresrc.SourceEdinet, "2026-11-13", 3, at)
		if err != nil || !ok {
			t.Fatalf("request %d: ok=%v err=%v, want allowed", i+1, ok, err)
		}
	}
	if ok, err := r.BeginRequest(ctx, disclosuresrc.SourceEdinet, "2026-11-13", 3, at); err != nil || ok {
		t.Errorf("4th request: ok=%v err=%v, want refused at the cap", ok, err)
	}
	st, err := r.Status(ctx, disclosuresrc.SourceEdinet)
	if err != nil {
		t.Fatal(err)
	}
	if st.RequestsToday != 3 || st.RequestCount != 3 || st.RequestsDay != "2026-11-13" {
		t.Errorf("status = %+v, want 3 requests on 2026-11-13 (a refused request is not counted)", st)
	}
	// A new JST day starts at one; the other source has its own count.
	if ok, err := r.BeginRequest(ctx, disclosuresrc.SourceEdinet, "2026-11-14", 3, at.Add(24*time.Hour)); err != nil || !ok {
		t.Fatalf("next day: ok=%v err=%v", ok, err)
	}
	if st, _ = r.Status(ctx, disclosuresrc.SourceEdinet); st.RequestsToday != 1 || st.RequestCount != 4 {
		t.Errorf("status after the next day = %+v, want 1 today and 4 in all", st)
	}
	if st, _ = r.Status(ctx, disclosuresrc.SourceTachibanaNews); st.RequestCount != 0 || st.LastFetchedAt != nil {
		t.Errorf("the other source = %+v, want untouched", st)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM research_disclosure_source_status`).Scan(&n); err != nil || n != 1 {
		t.Errorf("%d status rows (%v), want 1", n, err)
	}
}

func TestFailureAndSuccessAreRecordedWithTheirTimes(t *testing.T) {
	r, _ := open(t)
	ctx := t.Context()
	if _, err := r.BeginRequest(ctx, disclosuresrc.SourceEdinet, "2026-11-13", 10, at); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordFailure(ctx, disclosuresrc.SourceEdinet, "http_429", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordSuccess(ctx, disclosuresrc.SourceEdinet, at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	st, err := r.Status(ctx, disclosuresrc.SourceEdinet)
	if err != nil {
		t.Fatal(err)
	}
	if st.FailureCount != 1 || st.LastFailureCode != "http_429" || st.LastFailureAt == nil || !st.LastFailureAt.Equal(at.Add(time.Minute)) ||
		st.LastSuccessAt == nil || !st.LastSuccessAt.Equal(at.Add(2*time.Minute)) || st.LastFetchedAt == nil || !st.LastFetchedAt.Equal(at) {
		t.Errorf("status = %+v", st)
	}
}

func TestUpsertsAreIdempotentAndKeepFirstSeen(t *testing.T) {
	r, db := open(t)
	ctx := t.Context()
	d := disclosuresrc.EdinetDocument{DocID: "S1", Symbol: "7203", SecCode: "72030", Title: "v1", SubmittedAt: at}
	if _, err := r.UpsertEdinetDocuments(ctx, []disclosuresrc.EdinetDocument{d}, at); err != nil {
		t.Fatal(err)
	}
	d.Title = "v2"
	if _, err := r.UpsertEdinetDocuments(ctx, []disclosuresrc.EdinetDocument{d, d}, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var n int
	var title, first, updated string
	if err := db.QueryRow(`SELECT COUNT(*), MAX(title), MAX(first_seen_at), MAX(updated_at) FROM research_edinet_documents`).Scan(&n, &title, &first, &updated); err != nil {
		t.Fatal(err)
	}
	if n != 1 || title != "v2" || first != "2026-11-13T10:00:00.000000000Z" || updated != "2026-11-13T11:00:00.000000000Z" {
		t.Errorf("rows %d, title %q, first_seen %q, updated %q", n, title, first, updated)
	}

	news := []disclosuresrc.TachibanaNews{{NewsID: "n", Symbol: "7203", Headline: "h1", PublishedAt: at}, {NewsID: "n", Symbol: "", Headline: "h1", PublishedAt: at}}
	for range 2 {
		if _, err := r.UpsertTachibanaNews(ctx, news, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM research_tachibana_news`).Scan(&n); err != nil || n != 2 {
		t.Errorf("%d news rows (%v), want 2 (one per (news_id, symbol))", n, err)
	}
}

func TestEdinetDayFetchedAt(t *testing.T) {
	r, _ := open(t)
	ctx := t.Context()
	if _, ok, err := r.EdinetDayFetchedAt(ctx, "2026-11-13"); err != nil || ok {
		t.Fatalf("an unfetched day: ok=%v err=%v", ok, err)
	}
	if err := r.MarkEdinetDay(ctx, disclosuresrc.EdinetDay{Day: "2026-11-13", FetchedAt: at, Total: 3, Recorded: 1, Skipped: 2}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := r.EdinetDayFetchedAt(ctx, "2026-11-13")
	if err != nil || !ok || !got.Equal(at) {
		t.Errorf("fetched at = %v ok=%v err=%v, want %v", got, ok, err, at)
	}
	if repo.Day(at) != "2026-11-13" || repo.Day(time.Date(2026, 11, 13, 15, 30, 0, 0, time.UTC)) != "2026-11-14" {
		t.Error("Day is not the JST calendar day")
	}
}
