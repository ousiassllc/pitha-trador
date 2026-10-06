package scanner_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/scanner"
)

// scanSource is a CandidateSource that also serves a scan cycle.
type scanSource struct {
	scanner.StaticCandidateSource
	cycle domain.ScanCycle
	ok    bool
	err   error
}

func (s scanSource) Scan(context.Context) (domain.ScanCycle, bool, error) {
	return s.cycle, s.ok, s.err
}

func scanFixture() domain.ScanCycle {
	r := func(rs ...domain.ScreenReason) (out domain.ScreenReasons) {
		for _, x := range rs {
			out = out.Add(x)
		}
		return out
	}
	return domain.ScanCycle{
		StartedAt: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC), FinishedAt: time.Date(2026, 10, 3, 9, 0, 1, 500_000_000, time.UTC),
		Funnel: domain.ScanFunnel{Universe: 4, FeatureComputed: 3, FastScreenerPassed: 1, ScoutEvaluated: 1, ScoutPassed: 1},
		Symbols: []domain.ScanSymbol{
			{Symbol: "1001", Name: "Alpha", Market: "Prime"},
			{Symbol: "1002", Name: "Beta", Market: "Prime", Reasons: r(domain.ScreenReasonMinPrice, domain.ScreenReasonMaxSpread)},
			{Symbol: "1003", Name: "Gamma", Market: "Standard", Reasons: r(domain.ScreenReasonMissingSpread)},
			{Symbol: "1004", Name: "Delta", Market: "Growth", Reasons: r(domain.ScreenReasonNoSnapshot)},
		},
		Scout: map[string]domain.ScoutOutcome{"1001": domain.ScoutPassed},
	}
}

func scanHandler(src scanner.CandidateSource) *scanner.ScannerHandler {
	return scanner.NewScannerHandler(src, scanner.CandidateRefreshInterval{Min: time.Second, Max: 2 * time.Second})
}

func TestScannerHandler_APIScannerScan_FunnelFilterAndReasons(t *testing.T) {
	h := scanHandler(scanSource{cycle: scanFixture(), ok: true})
	out, err := h.APIScannerScan(context.Background(), &scanner.ScannerScanInput{Status: "excluded", Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	b := out.Body
	if !b.HasCycle || b.DurationMs != 1500 || b.Funnel.Universe != 4 || b.Funnel.FeatureComputed != 3 || b.Funnel.FastScreenerPassed != 1 || b.Funnel.ScoutPassed != 1 {
		t.Fatalf("body = %+v", b)
	}
	if b.Passed != 1 || b.Excluded != 1 || b.Missing != 2 {
		t.Errorf("status counts = %d/%d/%d, want 1/1/2", b.Passed, b.Excluded, b.Missing)
	}
	if b.Total != 1 || len(b.Items) != 1 || b.Items[0].Symbol != "1002" || b.Items[0].Status != "excluded" {
		t.Fatalf("items = %+v", b.Items)
	}
	if len(b.Items[0].Reasons) != 2 || b.Items[0].Reasons[0].Code != "min_price" || b.Items[0].Reasons[1].Code != "max_spread_bps" || b.Items[0].Reasons[0].Label == "" {
		t.Errorf("reasons = %+v", b.Items[0].Reasons)
	}
	counts := map[string]int{}
	for _, c := range b.ReasonCounts {
		counts[c.Code] = c.Count
	}
	if counts["min_price"] != 1 || counts["missing_spread"] != 1 || counts["no_snapshot"] != 1 || counts["max_price"] != 0 {
		t.Errorf("reason_counts = %v", counts)
	}
}

func TestScannerHandler_APIScannerScan_ScoutOutcomeOnCandidate(t *testing.T) {
	out, _ := scanHandler(scanSource{cycle: scanFixture(), ok: true}).APIScannerScan(context.Background(), &scanner.ScannerScanInput{Q: "alpha", PageSize: 50})
	if len(out.Body.Items) != 1 || out.Body.Items[0].Scout == nil || *out.Body.Items[0].Scout != "passed" || out.Body.Items[0].Status != "passed" {
		t.Fatalf("items = %+v", out.Body.Items)
	}
}

func TestScannerHandler_APIScannerScan_NoCycleOrNoScanSource(t *testing.T) {
	for name, src := range map[string]scanner.CandidateSource{
		"before first cycle":  scanSource{},
		"source without Scan": scanner.StaticCandidateSource{},
	} {
		out, err := scanHandler(src).APIScannerScan(context.Background(), &scanner.ScannerScanInput{})
		if err != nil || out.Body.HasCycle || out.Body.Items == nil || len(out.Body.Items) != 0 || out.Body.Pages != 1 {
			t.Errorf("%s: out=%+v err=%v, want empty has_cycle=false response", name, out.Body, err)
		}
	}
}

func TestScannerHandler_APIScannerScan_RejectsUnknownReason(t *testing.T) {
	_, err := scanHandler(scanSource{cycle: scanFixture(), ok: true}).APIScannerScan(context.Background(), &scanner.ScannerScanInput{Reason: "bogus"})
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v, want 400 naming the unknown reason", err)
	}
}

func TestScannerHandler_APIScannerScan_PagesLargeUniverse(t *testing.T) {
	cycle := scanFixture()
	cycle.Symbols = nil
	for i := range 4000 {
		cycle.Symbols = append(cycle.Symbols, domain.ScanSymbol{Symbol: fmt.Sprintf("%04d", i), Name: "n"})
	}
	out, _ := scanHandler(scanSource{cycle: cycle, ok: true}).APIScannerScan(context.Background(), &scanner.ScannerScanInput{Page: 3, PageSize: 100})
	b := out.Body
	if b.Total != 4000 || b.Pages != 40 || b.Page != 3 || len(b.Items) != 100 || b.Items[0].Symbol != "0200" {
		t.Fatalf("page = total %d pages %d page %d len %d first %q", b.Total, b.Pages, b.Page, len(b.Items), b.Items[0].Symbol)
	}
}

func getScan(t *testing.T, src scanner.CandidateSource, target string, hx bool) (int, string) {
	t.Helper()
	return getScanAt(t, src, time.Now, target, hx)
}

// getScanAt is getScan with the panel's clock pinned to now.
func getScanAt(t *testing.T, src scanner.CandidateSource, now func() time.Time, target string, hx bool) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	h := scanHandler(src)
	h.SetClock(now)
	engine.GET("/scanner", h.Page)
	engine.GET("/scanner/scan", h.ScanView)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestScannerHandler_Page_ShowsFunnelSummaryButNotSymbolList(t *testing.T) {
	_, body := getScan(t, scanSource{cycle: scanFixture(), ok: true}, "/scanner", false)
	for _, want := range []string{`data-testid="scan-panel"`, `data-testid="scan-funnel-universe">4<`, `data-testid="scan-funnel-features">3<`, `data-testid="scan-funnel-fast">1<`, `data-testid="scan-funnel-scout">1<`, "スキャン対象を見る", `data-testid="scan-refresh"`, "1.5s", "最終サイクル 2026-10-03 18:00:01 JST"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(body, `data-testid="scan-table"`) {
		t.Error("closed panel rendered the symbol list")
	}
}

func TestScannerHandler_Page_EmptyStateBeforeFirstCycle(t *testing.T) {
	for _, src := range []scanner.CandidateSource{scanSource{}, scanner.StaticCandidateSource{}} {
		code, body := getScan(t, src, "/scanner", false)
		if code != http.StatusOK || !strings.Contains(body, `data-testid="scan-empty"`) || strings.Contains(body, `data-testid="scan-funnel"`) {
			t.Errorf("code=%d, want empty-state panel without funnel", code)
		}
	}
}

func TestScannerHandler_Page_ScanFailureDoesNotBreakCandidates(t *testing.T) {
	code, body := getScan(t, scanSource{StaticCandidateSource: scanner.StaticCandidateSource{Items: fixtureCandidates(), AsOf: time.Now()}, err: errors.New("boom")}, "/scanner", false)
	if code != http.StatusOK || !strings.Contains(body, "7203") {
		t.Fatalf("code=%d, candidates must still render", code)
	}
}

func TestScannerHandler_ScanView_FragmentListsSymbolsWithStatusAndReasons(t *testing.T) {
	code, body := getScan(t, scanSource{cycle: scanFixture(), ok: true}, "/scanner/scan", true)
	if code != http.StatusOK || strings.Contains(body, "<html") {
		t.Fatalf("code=%d, want fragment", code)
	}
	for _, want := range []string{
		`data-symbol="1001" data-status="passed"`, `data-symbol="1002" data-status="excluded"`, `data-symbol="1003" data-status="missing"`, `data-symbol="1004" data-status="missing"`,
		`data-reason="min_price"`, `data-reason="max_spread_bps"`, `data-reason="missing_spread"`, `data-reason="no_snapshot"`,
		"Alpha", "通過", "除外", "データ欠損", `data-testid="scan-filter"`, "該当",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment missing %q", want)
		}
	}
}

func TestScannerHandler_ScanView_FiltersBySearchStatusReason(t *testing.T) {
	src := scanSource{cycle: scanFixture(), ok: true}
	tests := []struct {
		target  string
		present []string
		absent  []string
	}{
		{"/scanner/scan?status=missing", []string{`data-symbol="1003"`, `data-symbol="1004"`}, []string{`data-symbol="1001"`, `data-symbol="1002"`}},
		{"/scanner/scan?reason=min_price", []string{`data-symbol="1002"`}, []string{`data-symbol="1001"`, `data-symbol="1003"`}},
		{"/scanner/scan?q=gam", []string{`data-symbol="1003"`}, []string{`data-symbol="1002"`}},
		{"/scanner/scan?q=zzz", []string{`data-testid="scan-no-match"`}, []string{`data-symbol=`}},
		{"/scanner/scan?status=bogus&reason=bogus&page=x", []string{`data-symbol="1001"`, `data-symbol="1004"`}, nil}, // stale link: ignored, not an error
	}
	for _, tt := range tests {
		code, body := getScan(t, src, tt.target, true)
		if code != http.StatusOK {
			t.Fatalf("%s: code %d", tt.target, code)
		}
		for _, w := range tt.present {
			if !strings.Contains(body, w) {
				t.Errorf("%s: missing %q", tt.target, w)
			}
		}
		for _, w := range tt.absent {
			if strings.Contains(body, w) {
				t.Errorf("%s: unexpected %q", tt.target, w)
			}
		}
	}
}

func TestScannerHandler_ScanView_PagerAndRefreshKeepFilters(t *testing.T) {
	cycle := scanFixture()
	cycle.Symbols = nil
	for i := range 120 {
		cycle.Symbols = append(cycle.Symbols, domain.ScanSymbol{Symbol: fmt.Sprintf("%04d", i), Name: "corp"})
	}
	src := scanSource{cycle: cycle, ok: true}
	_, p1 := getScan(t, src, "/scanner/scan?q=corp", true)
	if !strings.Contains(p1, `hx-get="/scanner/scan?page=2&amp;q=corp"`) || strings.Contains(p1, "scan-prev") {
		t.Errorf("page 1 pager wrong")
	}
	_, p3 := getScan(t, src, "/scanner/scan?q=corp&page=3", true)
	if !strings.Contains(p3, `data-symbol="0119"`) || strings.Contains(p3, `data-symbol="0049"`) || strings.Contains(p3, "scan-next") || !strings.Contains(p3, "scan-prev") {
		t.Errorf("page 3 wrong")
	}
	if !strings.Contains(p3, `data-testid="scan-refresh"`) || !strings.Contains(p3, `hx-get="/scanner/scan?page=3&amp;q=corp"`) {
		t.Errorf("refresh must re-fetch the same filtered page")
	}
	_, closed := getScan(t, src, "/scanner/scan?open=0", true)
	if strings.Contains(closed, "scan-table") || !strings.Contains(closed, `hx-get="/scanner/scan?open=0"`) {
		t.Errorf("open=0 must render the funnel only and refresh to itself")
	}
}

func TestScannerHandler_ScanView_FullPageWithoutHXRequest(t *testing.T) {
	_, body := getScan(t, scanSource{cycle: scanFixture(), ok: true}, "/scanner/scan?status=excluded", false)
	if !strings.Contains(strings.ToLower(body), "<!doctype html>") || !strings.Contains(body, `data-symbol="1002"`) || !strings.Contains(body, `data-testid="scanner-table"`) {
		t.Error("non-HTMX /scanner/scan must be a full page with the open panel and the candidate table")
	}
}

func TestScannerHandler_ScanView_ScanErrorIs500(t *testing.T) {
	code, _ := getScan(t, scanSource{err: errors.New("boom")}, "/scanner/scan", true)
	if code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", code)
	}
}

// The production wiring (cmd/server) hands *screener.LiveSource to the
// router as the CandidateSource; it must satisfy ScanSource or the scan
// panel silently stays on its empty state (issue #303).
var _ scanner.ScanSource = (*screener.LiveSource)(nil)

func TestScannerHandler_ScanView_ServesLiveSourceCycle(t *testing.T) {
	live := screener.NewLiveSource()
	if _, body := getScan(t, live, "/scanner/scan", true); !strings.Contains(body, `data-testid="scan-empty"`) {
		t.Fatalf("before the first cycle: %s", body)
	}
	live.SetScan(domain.ScanCycle{Funnel: domain.ScanFunnel{Universe: 1}, Symbols: []domain.ScanSymbol{{Symbol: "7203", Name: "Toyota", Reasons: domain.ScreenReasons(0).Add(domain.ScreenReasonMaxSpread)}}})
	if _, body := getScan(t, live, "/scanner/scan", true); !strings.Contains(body, `data-symbol="7203" data-status="excluded"`) || !strings.Contains(body, `data-reason="max_spread_bps"`) {
		t.Fatalf("after a cycle: %s", body)
	}
}
