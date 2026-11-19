// Package edinettests holds the tests of the EDINET document-list client (issue
// #1096). Every request goes to an httptest server or a fake transport; the key
// is an obviously fake value.
package edinettests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosuresrc/edinet"
)

const testKey = "test-key-xxxx"

// transportFunc is a fake http.RoundTripper that counts its calls.
type transportFunc struct {
	calls atomic.Int32
	fn    func(*http.Request) (*http.Response, error)
}

func (f *transportFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	return f.fn(r)
}

func serverReturning(t *testing.T, status int, body string, seen *[]*http.Request) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = append(*seen, r.Clone(r.Context()))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWithoutAKeyNoRequestIsSent(t *testing.T) {
	tr := &transportFunc{fn: func(*http.Request) (*http.Response, error) { return nil, errors.New("must not be called") }}
	c := edinet.New(edinet.Config{APIKey: "", HTTPClient: &http.Client{Transport: tr}})
	if c.Configured() {
		t.Fatal("a client without a key reports Configured")
	}
	if _, err := c.ListDocuments(context.Background(), "2026-11-12"); !errors.Is(err, edinet.ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
	if n := tr.calls.Load(); n != 0 {
		t.Errorf("%d requests, want 0", n)
	}
}

func TestListsDocumentsWithTheKeyInTheQueryAndNormalizesCodes(t *testing.T) {
	var seen []*http.Request
	srv := serverReturning(t, 200, `{"metadata":{"status":"200"},"results":[
		{"docID":"S1","edinetCode":"E1","secCode":"72030","filerName":"トヨタ","docTypeCode":"120","submitDateTime":"2026-11-12 15:00","docDescription":"有価証券報告書"},
		{"docID":"S2","secCode":"130A0","submitDateTime":"2026-11-12 09:30","docDescription":"new-style code"},
		{"docID":"S3","secCode":null,"submitDateTime":"2026-11-12 10:00"},
		{"docID":"S4","secCode":"67580","submitDateTime":"garbage"},
		{"docID":"S5","secCode":"123","submitDateTime":"2026-11-12 10:00"}]}`, &seen)
	c := edinet.New(edinet.Config{BaseURL: srv.URL, APIKey: testKey})

	l, err := c.ListDocuments(context.Background(), "2026-11-12")
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].URL.Path != "/documents.json" {
		t.Fatalf("requests = %v, want one documents.json", seen)
	}
	q := seen[0].URL.Query()
	if q.Get("date") != "2026-11-12" || q.Get("type") != "2" || q.Get("Subscription-Key") != testKey {
		t.Errorf("query = %v", q)
	}
	if l.Total != 5 || l.Skipped != 3 || len(l.Docs) != 2 {
		t.Fatalf("total/skipped/docs = %d/%d/%d, want 5/3/2", l.Total, l.Skipped, len(l.Docs))
	}
	d := l.Docs[0]
	if d.DocID != "S1" || d.Symbol != "7203" || d.SecCode != "72030" || d.FilerName != "トヨタ" || d.DocTypeCode != "120" || d.Title != "有価証券報告書" {
		t.Errorf("doc = %+v", d)
	}
	if d.SubmittedAt.Format("2006-01-02T15:04:05Z07:00") != "2026-11-12T06:00:00Z" { // 15:00 JST
		t.Errorf("submitted = %v, want 06:00Z", d.SubmittedAt)
	}
	if l.Docs[1].Symbol != "130A" {
		t.Errorf("symbol = %q, want 130A", l.Docs[1].Symbol)
	}
}

func TestFailuresAreCodesWithoutTheKeyOrTheURL(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		code   string
	}{
		{"HTTP 401", 401, `{"statusCode":401,"message":"Access denied due to invalid subscription key test-key-xxxx"}`, "http_401"},
		{"HTTP 429", 429, ``, "http_429"},
		{"HTTP 404", 404, ``, "http_4xx"},
		{"HTTP 500", 500, `boom test-key-xxxx`, "http_5xx"},
		{"gateway error in a 200", 200, `{"statusCode":401,"message":"invalid test-key-xxxx"}`, "http_401"},
		{"metadata status in a 200", 200, `{"metadata":{"status":"500","message":"x"}}`, "http_5xx"},
		{"a body that is not JSON", 200, `<html>test-key-xxxx</html>`, "invalid_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := serverReturning(t, tc.status, tc.body, nil)
			_, err := edinet.New(edinet.Config{BaseURL: srv.URL, APIKey: testKey}).ListDocuments(context.Background(), "2026-11-12")
			var re *edinet.RequestError
			if !errors.As(err, &re) || re.Code != tc.code {
				t.Fatalf("err = %v, want a RequestError %q", err, tc.code)
			}
			if msg := err.Error(); strings.Contains(msg, testKey) || strings.Contains(msg, srv.URL) || strings.Contains(msg, "Subscription-Key") {
				t.Errorf("the error leaks the key or the URL: %q", msg)
			}
		})
	}
}

func TestTransportErrorsDoNotCarryTheURLWithTheKey(t *testing.T) {
	for _, tc := range []struct {
		name  string
		inner error
		code  string
	}{
		{"a timeout", context.DeadlineExceeded, disclosuresrc.FailTimeout},
		{"a refused dial", errors.New("connection refused"), disclosuresrc.FailTransport},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// What net/http returns: a *url.Error whose text holds the full URL, key included.
			tr := &transportFunc{fn: func(r *http.Request) (*http.Response, error) {
				return nil, &url.Error{Op: "Get", URL: r.URL.String(), Err: tc.inner}
			}}
			c := edinet.New(edinet.Config{APIKey: testKey, HTTPClient: &http.Client{Transport: tr}})
			_, err := c.ListDocuments(context.Background(), "2026-11-12")
			var re *edinet.RequestError
			if !errors.As(err, &re) || re.Code != tc.code {
				t.Fatalf("err = %v, want a RequestError %q", err, tc.code)
			}
			if msg := err.Error(); strings.Contains(msg, testKey) || strings.Contains(msg, "documents.json") || strings.Contains(msg, "api.edinet") {
				t.Errorf("the error leaks the key or the URL: %q", msg)
			}
		})
	}
}

func TestOnlyTheDocumentListIsEverRequested(t *testing.T) {
	var seen []*http.Request
	srv := serverReturning(t, 200, `{"metadata":{"status":"200"},"results":[{"docID":"S1","secCode":"72030","submitDateTime":"2026-11-12 15:00"}]}`, &seen)
	c := edinet.New(edinet.Config{BaseURL: srv.URL, APIKey: testKey})
	for _, day := range []string{"2026-11-12", "2026-11-11"} {
		if _, err := c.ListDocuments(context.Background(), day); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.ListDocuments(context.Background(), "not-a-date"); err == nil {
		t.Error("a malformed date was sent")
	}
	for _, r := range seen {
		if r.URL.Path != "/documents.json" {
			t.Errorf("requested %s: the client must never fetch a document body", r.URL.Path)
		}
	}
	if len(seen) != 2 {
		t.Errorf("%d requests, want 2 (the malformed date sends none)", len(seen))
	}
}
