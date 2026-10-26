package orderproposals_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/orderproposals"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

// dbSource is the Source over a test database, the way bootstrap/proposalview
// wires the real one.
type dbSource struct{ db *sql.DB }

func (s dbSource) List(ctx context.Context, f proposal.ListFilter) ([]proposal.Item, string, error) {
	return proposal.List(ctx, s.db, f)
}

func (s dbSource) Get(ctx context.Context, id int64) (proposal.State, error) {
	return proposal.Get(ctx, s.db, id)
}

type failingSource struct{ err error }

func (f failingSource) List(context.Context, proposal.ListFilter) ([]proposal.Item, string, error) {
	return nil, "", f.err
}
func (f failingSource) Get(context.Context, int64) (proposal.State, error) {
	return proposal.State{}, f.err
}

func get(t *testing.T, source orderproposals.Source, target string) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	h := orderproposals.NewHandler(source)
	h.Register(engine)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Code, rec.Body.String()
}

func outcomesSource(t *testing.T) dbSource {
	t.Helper()
	_, w := testkit.NewMigratedDB(t)
	testkit.SeedProposalOutcomes(t, w)
	return dbSource{w}
}

// The list shows one row per proposal, newest first, each with its status and
// reason code; the won and the lost proposal carry their realized pnl, the
// others none.
func TestList_ShowsEachProposalWithItsStatusAndPnl(t *testing.T) {
	code, body := get(t, outcomesSource(t), "/order-proposals?from=2026-10-01&to=2026-10-31")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	rowRe := regexp.MustCompile(`(?s)<tr class="[^"]*" data-testid="order-proposal-row" data-proposal-id="(\d+)" data-status="(\w+)">(.*?)</tr>`)
	var ids, statuses []string
	cells := map[string]string{}
	for _, m := range rowRe.FindAllStringSubmatch(body, -1) {
		ids, statuses, cells[m[1]] = append(ids, m[1]), append(statuses, m[2]), m[3]
	}
	if got := strings.Join(ids, ","); got != "6,5,4,3,2,1" {
		t.Fatalf("row ids = %s", got)
	}
	if got := strings.Join(statuses, ","); got != "closed,filled,filled,rejected,cancelled,expired" {
		t.Errorf("row statuses = %s", got)
	}
	for id, wants := range map[string][]string{
		"4": {"20,970 円", "text-emerald-700", "200 → 100", "20,000 円 / 15,000 円", "約定", `href="/order-proposals/4"`, `href="/symbols/7203"`},
		"5": {"-12,030 円", "text-red-700"},
		"3": {"拒否", "枠の予算を超える", "500", "—"},
		"2": {"債券ETF", "取消", "取消の依頼"},
		"1": {"期限切れ"},
		"6": {"決済", "決済済み"},
	} {
		for _, want := range wants {
			if !strings.Contains(cells[id], want) {
				t.Errorf("row %s lacks %q: %s", id, want, cells[id])
			}
		}
	}
	if strings.Contains(cells["3"], "円</td>") && strings.Contains(cells["3"], "text-emerald") {
		t.Errorf("a rejected proposal has no pnl: %s", cells["3"])
	}
	if !strings.Contains(body, `data-testid="order-proposals-filter"`) || strings.Contains(body, `data-testid="order-proposals-next"`) {
		t.Errorf("filter form missing or a next page without one: %s", body)
	}
}

func TestList_FiltersAndPages(t *testing.T) {
	src := outcomesSource(t)
	_, body := get(t, src, "/order-proposals?from=2026-10-01&to=2026-10-31&status=rejected")
	if n := strings.Count(body, `data-testid="order-proposal-row"`); n != 1 || !strings.Contains(body, `data-proposal-id="3"`) {
		t.Errorf("status filter: %d rows: %s", n, body)
	}
	_, body = get(t, src, "/order-proposals?from=2026-10-17&to=2026-10-17")
	if n := strings.Count(body, `data-testid="order-proposal-row"`); n != 3 {
		t.Errorf("one JST day: %d rows, want the 3 received on 2026-10-17", n)
	}
	_, body = get(t, src, "/order-proposals?from=2026-09-01&to=2026-09-02")
	if !strings.Contains(body, `data-testid="order-proposals-empty"`) {
		t.Errorf("an empty period must say so: %s", body)
	}
}

// More than a page: the next link keeps the filter and leads to the rest, with
// no row repeated or lost.
func TestList_NextPageKeepsTheFilter(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	for i := range 55 {
		at := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		testkit.Proposal(t, w, testkit.ProposalRow{ID: int64(i + 1), ReceivedAt: at, Status: "expired", Reason: "expired", Quantity: 100})
	}
	src := dbSource{w}
	_, first := get(t, src, "/order-proposals?from=2026-10-01&to=2026-10-31&status=expired")
	if n := strings.Count(first, `data-testid="order-proposal-row"`); n != 50 {
		t.Fatalf("first page = %d rows, want 50", n)
	}
	href := regexp.MustCompile(`href="([^"]+)" data-testid="order-proposals-next"`).FindStringSubmatch(first)
	if href == nil {
		t.Fatalf("no next link: %s", first)
	}
	next := strings.ReplaceAll(href[1], "&amp;", "&")
	for _, want := range []string{"from=2026-10-01", "to=2026-10-31", "status=expired", "cursor="} {
		if !strings.Contains(next, want) {
			t.Errorf("next link %q lacks %s", next, want)
		}
	}
	_, second := get(t, src, next)
	if n := strings.Count(second, `data-testid="order-proposal-row"`); n != 5 || strings.Contains(second, `data-testid="order-proposals-next"`) {
		t.Errorf("second page = %d rows (or has another next link)", n)
	}
}

func TestList_BadInputIs400WithTheReason(t *testing.T) {
	src := outcomesSource(t)
	for target, want := range map[string]string{
		"/order-proposals?from=yesterday":                "開始日は YYYY-MM-DD",
		"/order-proposals?to=2026-13-40":                 "終了日は YYYY-MM-DD",
		"/order-proposals?from=2026-10-10&to=2026-10-01": "終了日は開始日以降",
		"/order-proposals?status=approved":               "状態の指定が正しくありません",
		"/order-proposals?bucket=fx":                     "枠は株式か債券ETFを指定してください",
		"/order-proposals?cursor=not-a-cursor":           "ページの指定が正しくありません",
	} {
		code, body := get(t, src, target)
		if code != http.StatusBadRequest || !strings.Contains(body, `data-testid="order-proposals-error"`) || !strings.Contains(body, want) {
			t.Errorf("%s: status %d, want 400 with %q: %s", target, code, want, body)
		}
	}
}

func TestList_ReadFailureIs500WithoutLeakingIt(t *testing.T) {
	code, body := get(t, failingSource{errors.New("db exploded: SECRET-DSN")}, "/order-proposals")
	if code != http.StatusInternalServerError || !strings.Contains(body, "注文提案の取得に失敗しました。") || strings.Contains(body, "SECRET-DSN") {
		t.Errorf("status %d, body %s", code, body)
	}
}

// The detail follows the proposal to its signal, order and position: the plan,
// the loss the proposer expected, the application's estimate and the actual
// loss, the timeline and the realized pnl.
func TestDetail_TracesProposalToRealizedPnl(t *testing.T) {
	code, body := get(t, outcomesSource(t), "/order-proposals/5")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{
		"注文提案 5 番", `data-status="filled"`, "決算が好調で需給も良い", "引けで手じまう",
		"2,400 円", "2,700 円", "届いた", "届かなかった", // stop reached (exit 2380 <= 2400), take not reached
		"20,000 円", "15,000 円", "12,030 円", // proposed, estimated, actual loss
		"トレードシグナル 2 番", "mcp-proposal-v1", "通過",
		"注文 3 番", "買い / 成行", "約定", "2,500 円",
		"ポジション 2 番", "決済済み", "AIの注文提案による決済", "5 時間 0 分", "-12,030 円", "text-red-700",
		"2026-10-17 10:00:00 JST", // received 01:00Z
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
	if n := strings.Count(body, `data-testid="order-proposal-event"`); n != 2 {
		t.Errorf("events = %d, want 2", n)
	}
	if !strings.Contains(body, `href="/order-proposals"`) {
		t.Errorf("no link back to the list")
	}
}

func TestDetail_RejectedShowsTheRiskCodeAndNoOrder(t *testing.T) {
	_, body := get(t, outcomesSource(t), "/order-proposals/3")
	for _, want := range []string{"拒否", "枠の予算を超える", "トレードシグナル 3 番"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
	if strings.Contains(body, "注文 3 番") || strings.Contains(body, "ポジション 2 番") || strings.Contains(body, "used 1.2M") {
		t.Errorf("a rejected proposal has no order or position, and the signal's reason text is not shown: %s", body)
	}
	if n := strings.Count(body, `data-testid="order-proposal-event"`); n != 3 {
		t.Errorf("events = %d, want 3", n)
	}
}

func TestDetail_UnknownOrMalformedIDIs404(t *testing.T) {
	src := outcomesSource(t)
	for _, target := range []string{"/order-proposals/999", "/order-proposals/abc", "/order-proposals/0", "/order-proposals/-1"} {
		if code, _ := get(t, src, target); code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", target, code)
		}
	}
	if code, body := get(t, failingSource{errors.New("SECRET-DSN")}, "/order-proposals/1"); code != http.StatusInternalServerError || strings.Contains(body, "SECRET-DSN") {
		t.Errorf("read failure: status %d body %s", code, body)
	}
}

// What the client wrote is shown as text, never as HTML: the rationale, the
// exit plan and a source's title and URL are escaped; only an http(s) URL is a
// link and it carries rel="noopener noreferrer"; javascript: and the like are
// plain text.
func TestDetail_EscapesFreeTextAndOnlyLinksHTTPSources(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	testkit.Proposal(t, w, testkit.ProposalRow{
		ID: 1, ReceivedAt: "2026-10-17T00:00:00Z", Status: "received", Quantity: 100,
		Rationale: `<script>alert("rationale")</script><b>太字</b> & "引用"`,
		ExitPlan:  `<img src=x onerror=alert(1)>`,
		SourcesJSON: `[
			{"title":"<script>alert('title')</script>","url":"https://example.com/a?x=1&y=<2>","retrieved_at":"2026-10-17T00:00:00Z"},
			{"title":"古い記事","url":"http://example.com/old","retrieved_at":"2026-10-17T00:00:00Z"},
			{"title":"悪意","url":"javascript:alert(document.cookie)","retrieved_at":"2026-10-17T00:00:00Z"},
			{"title":"data","url":"data:text/html,<script>alert(1)</script>","retrieved_at":"2026-10-17T00:00:00Z"},
			{"title":"相対","url":"/settings","retrieved_at":"2026-10-17T00:00:00Z"}
		]`,
	})
	code, body := get(t, dbSource{w}, "/order-proposals/1")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, raw := range []string{`<script>alert`, `<b>太字</b>`, `<img src=x`, `onerror=alert(1)>`} {
		if strings.Contains(body, raw) {
			t.Errorf("unescaped HTML %q in the page", raw)
		}
	}
	for _, escaped := range []string{
		`&lt;script&gt;alert(&#34;rationale&#34;)&lt;/script&gt;&lt;b&gt;太字&lt;/b&gt; &amp; &#34;引用&#34;`,
		`&lt;img src=x onerror=alert(1)&gt;`,
		`&lt;script&gt;alert(&#39;title&#39;)&lt;/script&gt;`,
	} {
		if !strings.Contains(body, escaped) {
			t.Errorf("escaped text %q missing", escaped)
		}
	}
	anchors := regexp.MustCompile(`<a [^>]*href="(https?://[^"]*)"[^>]*>`).FindAllStringSubmatch(body, -1)
	if len(anchors) != 2 {
		t.Fatalf("external links = %d, want the https and the http source only: %v", len(anchors), anchors)
	}
	for _, a := range anchors {
		if !strings.Contains(a[0], `rel="noopener noreferrer"`) {
			t.Errorf("external link without rel=noopener noreferrer: %s", a[0])
		}
	}
	section := regexp.MustCompile(`(?s)data-testid="order-proposal-sources".*?</section>`).FindString(body)
	if n := strings.Count(section, "<a "); n != 2 {
		t.Errorf("links in the sources = %d, want 2 (javascript:, data: and a relative path are text)", n)
	}
	for _, dangerous := range []string{`href="javascript:`, `href="data:`, "about:invalid"} {
		if strings.Contains(body, dangerous) {
			t.Errorf("a non-http source became a link: %s", dangerous)
		}
	}
	for _, text := range []string{"javascript:alert(document.cookie)", "data:text/html,", "/settings"} {
		if !strings.Contains(body, text) {
			t.Errorf("the URL %q is not shown as text", text)
		}
	}
}

// The screens only read: no form posts, no approve / cancel button.
func TestScreensHaveNoActions(t *testing.T) {
	src := outcomesSource(t)
	for _, target := range []string{"/order-proposals?from=2026-10-01&to=2026-10-31", "/order-proposals/4"} {
		_, body := get(t, src, target)
		lower := strings.ToLower(body)
		for _, bad := range []string{`method="post"`, "hx-post", "hx-delete", "hx-put", "承認", "取り消す", "発注する"} {
			if strings.Contains(lower, strings.ToLower(bad)) {
				t.Errorf("%s has an action (%q)", target, bad)
			}
		}
	}
}
