package newsfeed_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// Real やのしん response shapes (observed on 2026-10-06 and from llms.txt).
const (
	yanoshinNested = `{"total_count":2,"condition_desc":"7203の適時開示情報一覧","items":[
	  {"Tdnet":{"id":"1283563","pubdate":"2026-10-05 15:30:00","company_code":"72030","company_name":"トヨタ自","title":"自己株式の取得状況に関するお知らせ","document_url":"https://webapi.yanoshin.jp/rd.php?https://www.release.tdnet.info/inbs/a.pdf","url_xbrl":null}},
	  {"TDnet":{"id":"1279204","pubdate":"2026-10-01 13:00:00","company_code":"72030","title":"業績予想の修正に関するお知らせ","document_url":"https://www.release.tdnet.info/inbs/b.pdf"}}
	],"actions":["7203"]}`
	yanoshinFlat = `{"total_count":1,"items":[{"id":"1283563","pubdate":"2026-10-05 15:30:00","company_code":"72030","title":"自己株式の取得状況に関するお知らせ","document_url":"https://www.release.tdnet.info/inbs/a.pdf"}],"actions":["7203"]}`
)

// fakeYanoshin serves body for every request and records the request paths
// (with query) and headers.
type fakeYanoshin struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
	auth  string
}

func newFakeYanoshin(t *testing.T, status int, body string) *fakeYanoshin {
	t.Helper()
	f := &fakeYanoshin{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.RequestURI())
		f.auth = r.Header.Get("Authorization")
		f.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeYanoshin) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

func TestYanoshinClient_Fetch_NormalizesNestedDisclosuresToTheInternalContract(t *testing.T) {
	fake := newFakeYanoshin(t, http.StatusOK, yanoshinNested)
	client := newsfeed.NewYanoshinClient(newsfeed.YanoshinConfig{BaseURL: fake.URL})

	items, err := client.Fetch(context.Background(), "7203")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := fake.requests(); len(got) != 1 || got[0] != "/webapi/tdnet/list/7203.json?limit=10" {
		t.Errorf("requests = %v, want GET /webapi/tdnet/list/7203.json (per symbol, limited)", got)
	}
	if fake.auth != "" {
		t.Errorf("Authorization = %q, want none: やのしん needs no API key", fake.auth)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v, want 2 (Tdnet and TDnet casings)", items)
	}
	first := items[0]
	if first.ID != "1283563" || first.Symbol != "7203" || first.Headline != "自己株式の取得状況に関するお知らせ" {
		t.Errorf("first = %+v, want id/headline mapped and the request symbol (not company_code 72030)", first)
	}
	if want := "TDnet 適時開示: https://webapi.yanoshin.jp/rd.php?https://www.release.tdnet.info/inbs/a.pdf"; first.Body != want {
		t.Errorf("Body = %q, want the document link only (no PDF body), %q", first.Body, want)
	}
	// 2026-10-05 15:30:00 JST == 06:30:00Z.
	if want := time.Date(2026, 10, 5, 6, 30, 0, 0, time.UTC); !first.PublishedAt.Equal(want) {
		t.Errorf("PublishedAt = %v, want %v (pubdate is JST)", first.PublishedAt, want)
	}
	if items[1].ID != "1279204" || items[1].Headline != "業績予想の修正に関するお知らせ" {
		t.Errorf("second = %+v, want the TDnet-cased item mapped too", items[1])
	}
}

func TestYanoshinClient_Fetch_AcceptsFlatJson2Shape(t *testing.T) {
	fake := newFakeYanoshin(t, http.StatusOK, yanoshinFlat)
	items, err := newsfeed.NewYanoshinClient(newsfeed.YanoshinConfig{BaseURL: fake.URL}).Fetch(context.Background(), "7203")
	if err != nil || len(items) != 1 || items[0].ID != "1283563" || items[0].Headline != "自己株式の取得状況に関するお知らせ" {
		t.Fatalf("Fetch = %+v, %v, want the flat item mapped", items, err)
	}
}

func TestYanoshinClient_Fetch_EmptyAndMalformedDisclosures(t *testing.T) {
	empty := newFakeYanoshin(t, http.StatusOK, `{"total_count":0,"items":[],"actions":["0001"]}`)
	if items, err := newsfeed.NewYanoshinClient(newsfeed.YanoshinConfig{BaseURL: empty.URL}).Fetch(context.Background(), "0001"); err != nil || len(items) != 0 {
		t.Errorf("Fetch(no disclosures) = %+v, %v, want empty and no error", items, err)
	}

	odd := newFakeYanoshin(t, http.StatusOK, `{"items":[{"Tdnet":{"id":"1","title":"","pubdate":"2026-10-05 15:30:00"}},{"Tdnet":{"id":"2","title":"見出し","pubdate":"not a date"}}]}`)
	items, err := newsfeed.NewYanoshinClient(newsfeed.YanoshinConfig{BaseURL: odd.URL}).Fetch(context.Background(), "7203")
	if err != nil || len(items) != 1 || items[0].ID != "2" {
		t.Fatalf("Fetch = %+v, %v, want the title-less item dropped and the item kept", items, err)
	}
	if !items[0].PublishedAt.IsZero() || items[0].Body != "TDnet 適時開示" {
		t.Errorf("item = %+v, want a zero PublishedAt (Service treats it as new) and a link-less body", items[0])
	}
}

func TestYanoshinClient_Fetch_FailuresAreErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"server error": {http.StatusInternalServerError, "boom"},
		"rate limited": {http.StatusTooManyRequests, ""},
		"html page":    {http.StatusOK, "<html>maintenance</html>"},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeYanoshin(t, tc.status, tc.body)
			if items, err := newsfeed.NewYanoshinClient(newsfeed.YanoshinConfig{BaseURL: fake.URL}).Fetch(context.Background(), "7203"); err == nil {
				t.Errorf("Fetch = %+v, nil; want an error", items)
			}
		})
	}
}

func TestYanoshinClient_Fetch_ShortTimeoutAbandonsASlowServer(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); server.Close() })
	client := newsfeed.NewYanoshinClient(newsfeed.YanoshinConfig{BaseURL: server.URL, HTTPClient: &http.Client{Timeout: 50 * time.Millisecond}})

	start := time.Now()
	if _, err := client.Fetch(context.Background(), "7203"); err == nil {
		t.Fatal("Fetch against a hanging server returned no error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Fetch took %v, want it bounded by the short timeout", elapsed)
	}
}
