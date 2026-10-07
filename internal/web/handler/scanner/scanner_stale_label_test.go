package scanner_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/scanner"
)

// The stale_snapshot label is shown in the SSR panel, the JSON API and the CSV
// export. It must be the same threshold-agnostic text in all three, since the
// real threshold depends on scan.full_scan_enabled (issues #690/#691).
func TestScannerHandler_StaleSnapshotLabel_ThresholdAgnosticAcrossSSRAPIAndCSV(t *testing.T) {
	cycle := scanFixture()
	cycle.Symbols = []domain.ScanSymbol{{Symbol: "7203", Name: "Toyota", Market: "Prime", Reasons: domain.ScreenReasons(0).Add(domain.ScreenReasonStaleSnapshot)}}
	src := scanSource{cycle: cycle, ok: true}
	want := domain.ScreenReasonStaleSnapshot.Label()
	if strings.Contains(want, "3分") {
		t.Fatalf("label %q hard-codes the ranking-watch threshold", want)
	}

	code, body := getScan(t, src, "/scanner/scan", true)
	if code != http.StatusOK || !strings.Contains(body, `data-reason="stale_snapshot">`+want+"</li>") {
		t.Errorf("SSR row lacks the label %q (code=%d)", want, code)
	}
	if strings.Contains(body, "3分") {
		t.Error("SSR fragment still hard-codes 3分")
	}

	h := scanHandler(src)
	api, err := h.APIScannerScan(context.Background(), &scanner.ScannerScanInput{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(api.Body.Items) != 1 || len(api.Body.Items[0].Reasons) != 1 || api.Body.Items[0].Reasons[0].Label != want {
		t.Errorf("API reasons = %+v, want label %q", api.Body.Items, want)
	}

	out, err := h.APIScannerScanExport(context.Background(), &scanner.ScannerScanExportInput{})
	if err != nil {
		t.Fatal(err)
	}
	rows := parseExport(t, out)
	if len(rows) != 2 || rows[1][4] != "stale_snapshot" || rows[1][5] != want {
		t.Errorf("CSV rows = %v, want stale_snapshot with label %q", rows, want)
	}
}
