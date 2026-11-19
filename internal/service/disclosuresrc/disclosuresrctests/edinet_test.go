package disclosuresrctests

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/disclosuresrc"
)

func TestEdinetRecordsTheListMetadataAndNothingElse(t *testing.T) {
	srv := newEdinetServer(t, fixtureList)
	db := openDB(t)
	rec := newEdinetRecorder(db, srv.client(testKey), on(nil), nil)

	res, err := rec.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Fri 13, Thu 12, Wed 11: one list request each.
	if res.Requests != 3 || res.Days != 3 || res.Rows != 3 || res.Failures != 0 || res.More {
		t.Fatalf("result = %+v, want 3 requests, 3 days, 3 rows", res)
	}
	// Metadata only: every request is the document LIST (documents.json, type=2),
	// never a document download.
	for i, p := range srv.transport.paths {
		if p != "/api/v2/documents.json" || !strings.Contains(srv.transport.urls[i], "type=2") {
			t.Errorf("request %d: %s, want the document list only", i, srv.transport.urls[i])
		}
	}
	// Only the document with a securities code and a readable time is a row; the
	// fund (no secCode) and the unreadable one count as skipped.
	if n := count(t, db, `SELECT COUNT(*) FROM research_edinet_documents`); n != 3 {
		t.Fatalf("%d documents, want 3 (one per day)", n)
	}
	var symbol, secCode, submittedAt, submitDay, title, docType string
	if err := db.QueryRow(`SELECT symbol, sec_code, submitted_at, submit_day, title, doc_type_code FROM research_edinet_documents WHERE doc_id = 'S-2026-11-12-A'`).
		Scan(&symbol, &secCode, &submittedAt, &submitDay, &title, &docType); err != nil {
		t.Fatal(err)
	}
	// 15:00 JST is 06:00Z; symbol is the 4-character code of disclosure_items.
	if symbol != "7203" || secCode != "72030" || submittedAt != "2026-11-12T06:00:00.000000000Z" || submitDay != "2026-11-12" || title != "有価証券報告書" || docType != "120" {
		t.Errorf("row = %q %q %q %q %q %q", symbol, secCode, submittedAt, submitDay, title, docType)
	}
	var total, recorded, skipped int
	if err := db.QueryRow(`SELECT total_count, recorded_count, skipped_count FROM research_edinet_fetch_days WHERE day = '2026-11-12'`).Scan(&total, &recorded, &skipped); err != nil {
		t.Fatal(err)
	}
	if total != 3 || recorded != 1 || skipped != 2 {
		t.Errorf("fetch day total/recorded/skipped = %d/%d/%d, want 3/1/2", total, recorded, skipped)
	}
	// The key is in no stored text.
	if n := count(t, db, `SELECT COUNT(*) FROM research_edinet_documents WHERE doc_id || symbol || sec_code || edinet_code || filer_name || title LIKE '%'||?||'%'`, testKey); n != 0 {
		t.Error("a document row holds the key")
	}

	// A second run the same evening: today's list is fetched once, and the past
	// days were fetched after their own day, so nothing is asked again.
	res, err = rec.Run(context.Background())
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if srv.transport.requests() != 3 || res.Requests != 0 {
		t.Errorf("second run sent %d more requests, want 0", srv.transport.requests()-3)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_edinet_documents`); n != 3 {
		t.Errorf("%d documents after the second run, want 3", n)
	}
}

func TestEdinetRefetchesADayFetchedBeforeItEndedAndUpsertsIdempotently(t *testing.T) {
	srv := newEdinetServer(t, fixtureList)
	db := openDB(t)
	s := on(map[string]string{"disclosure_src.edinet.lookback_days": "1"})
	rec := newEdinetRecorder(db, srv.client(testKey), s, nil)
	if _, err := rec.Run(context.Background()); err != nil { // Fri 13th evening
		t.Fatal(err)
	}
	// The next Monday: Friday's list was fetched while Friday was running, so it
	// is fetched once more (documents are submitted until the evening); the rows
	// stay one per document.
	monday := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	later := newEdinetRecorder(db, srv.client(testKey), on(map[string]string{"disclosure_src.edinet.lookback_days": "4"}), nil).WithClock(func() time.Time { return monday })
	res, err := later.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Mon 16 (new), Fri 13 (refetch); Sat/Sun are not asked for.
	if res.Requests != 2 {
		t.Errorf("requests = %d, want 2 (Monday and the Friday refetch)", res.Requests)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_edinet_documents WHERE submit_day = '2026-11-13'`); n != 1 {
		t.Errorf("%d documents of Friday after the refetch, want 1 (idempotent)", n)
	}
}

func TestEdinetStopsAtTheDailyRequestCap(t *testing.T) {
	srv := newEdinetServer(t, fixtureList)
	db := openDB(t)
	s := on(map[string]string{"disclosure_src.edinet.daily_request_cap": "2", "disclosure_src.edinet.lookback_days": "3"})
	rec := newEdinetRecorder(db, srv.client(testKey), s, nil)

	res, err := rec.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Requests != 2 || !res.CapReached {
		t.Fatalf("result = %+v, want 2 requests and CapReached", res)
	}
	res, err = rec.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Requests != 0 || !res.CapReached || srv.transport.requests() != 2 {
		t.Errorf("after the cap: result %+v, %d total requests, want none more", res, srv.transport.requests())
	}
	// The next JST day the count starts again (Saturday: Friday's list is fetched
	// once more, and two days never fetched are still behind).
	s2 := on(map[string]string{"disclosure_src.edinet.daily_request_cap": "2", "disclosure_src.edinet.lookback_days": "5"})
	tomorrow := newEdinetRecorder(db, srv.client(testKey), s2, nil).WithClock(func() time.Time { return now.Add(24 * time.Hour) })
	res, err = tomorrow.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Requests != 2 {
		t.Errorf("next day requests = %d, want 2 (the cap again)", res.Requests)
	}
}

func TestEdinetWorksInChunksWithAPauseBetweenRequests(t *testing.T) {
	srv := newEdinetServer(t, fixtureList)
	db := openDB(t)
	pauses := 0
	// 14 days back from Fri 13th: 10 weekdays.
	rec := newEdinetRecorder(db, srv.client(testKey), on(map[string]string{"disclosure_src.edinet.lookback_days": "14"}), &pauses)

	res, err := rec.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Requests != disclosuresrc.EdinetChunkRequests || !res.More || pauses != disclosuresrc.EdinetChunkRequests-1 || res.Purged != 0 {
		t.Fatalf("first chunk = %+v, pauses %d; want %d requests, More, %d pauses", res, pauses, disclosuresrc.EdinetChunkRequests, disclosuresrc.EdinetChunkRequests-1)
	}
	res, err = rec.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Requests != 5 || res.More {
		t.Errorf("second chunk = %+v, want the last 5 requests and no more", res)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_edinet_fetch_days`); n != 10 {
		t.Errorf("%d days recorded, want 10 weekdays", n)
	}
}

func TestEdinetRecordsAFailureByCodeOnlyAndStops(t *testing.T) {
	srv := newEdinetServer(t, fixtureList)
	srv.status = http.StatusTooManyRequests
	db := openDB(t)
	res, err := newEdinetRecorder(db, srv.client(testKey), on(nil), nil).Run(context.Background())
	if err != nil {
		t.Fatalf("a failed request is recorded, not returned: %v", err)
	}
	if res.Requests != 1 || res.Failures != 1 {
		t.Errorf("result = %+v, want one failed request and a stop", res)
	}
	var failures, requests int
	var code string
	if err := db.QueryRow(`SELECT failure_count, request_count, last_failure_code FROM research_disclosure_source_status WHERE source = 'edinet'`).Scan(&failures, &requests, &code); err != nil {
		t.Fatal(err)
	}
	if failures != 1 || requests != 1 || code != "http_429" {
		t.Errorf("status = %d failures / %d requests / %q, want 1/1/http_429", failures, requests, code)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM research_edinet_fetch_days`); n != 0 {
		t.Errorf("a failed day was marked fetched")
	}
}
