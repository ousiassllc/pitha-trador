package scanner_test

import (
	"context"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/scanner"
)

func parseExport(t *testing.T, out *scanner.ScannerScanExportOutput) [][]string {
	t.Helper()
	body := string(out.Body)
	if !strings.HasPrefix(body, "\uFEFF") {
		t.Fatal("export lacks the UTF-8 BOM Excel needs for Japanese text")
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\uFEFF"))).ReadAll()
	if err != nil {
		t.Fatalf("export is not valid CSV: %v\n%s", err, body)
	}
	return rows
}

func TestScannerHandler_APIScannerScanExport_AllMatchesUnpagedWithReasonsAndScout(t *testing.T) {
	h := scanHandler(scanSource{cycle: scanFixture(), ok: true})
	out, err := h.APIScannerScanExport(context.Background(), &scanner.ScannerScanExportInput{})
	if err != nil {
		t.Fatal(err)
	}
	rows := parseExport(t, out)
	if len(rows) != 5 || out.RecordCount != 4 {
		t.Fatalf("rows = %d, record count = %d, want header + 4 symbols", len(rows), out.RecordCount)
	}
	if got := strings.Join(rows[0], ","); got != "symbol,name,market,status,reason_codes,reason_labels,scout" {
		t.Errorf("header = %s", got)
	}
	if got := rows[1]; got[0] != "1001" || got[3] != "passed" || got[4] != "" || got[6] != "passed" {
		t.Errorf("candidate row = %v, want passed with no reasons and scout=passed", got)
	}
	if got := rows[2]; got[3] != "excluded" || got[4] != "min_price;max_spread_bps" || got[6] != "" {
		t.Errorf("excluded row = %v, want both reason codes joined by ';' and no scout", got)
	}
	if got := rows[3][3]; got != "missing" {
		t.Errorf("1003 status = %q, want missing", got)
	}
	if !strings.Contains(out.ContentDisposition, `attachment; filename="pitha-scan-20261003-090001.csv"`) {
		t.Errorf("Content-Disposition = %q, want the cycle's finish time in the name", out.ContentDisposition)
	}
}

// A 4,000-symbol universe must export whole: the JSON list caps a page at
// 200, the export must not inherit that cap.
func TestScannerHandler_APIScannerScanExport_ExportsMoreThanOnePage(t *testing.T) {
	cycle := scanFixture()
	cycle.Symbols = nil
	for i := range 450 {
		cycle.Symbols = append(cycle.Symbols, domain.ScanSymbol{Symbol: "S" + strings.Repeat("0", 3) + string(rune('A'+i%26)) + string(rune('a'+i/26)), Name: "n", Market: "Prime"})
	}
	out, err := scanHandler(scanSource{cycle: cycle, ok: true}).APIScannerScanExport(context.Background(), &scanner.ScannerScanExportInput{})
	if err != nil {
		t.Fatal(err)
	}
	if rows := parseExport(t, out); len(rows) != 451 || out.RecordCount != 450 {
		t.Fatalf("rows = %d, record count = %d, want all 450 symbols", len(rows), out.RecordCount)
	}
}

func TestScannerHandler_APIScannerScanExport_AppliesFilters(t *testing.T) {
	h := scanHandler(scanSource{cycle: scanFixture(), ok: true})
	out, err := h.APIScannerScanExport(context.Background(), &scanner.ScannerScanExportInput{Status: "excluded", Reason: "min_price"})
	if err != nil {
		t.Fatal(err)
	}
	if rows := parseExport(t, out); len(rows) != 2 || rows[1][0] != "1002" {
		t.Fatalf("rows = %v, want only 1002", rows)
	}
	if _, err := h.APIScannerScanExport(context.Background(), &scanner.ScannerScanExportInput{Reason: "bogus"}); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v, want 400 naming the unknown reason", err)
	}
}

func TestScannerHandler_APIScannerScanExport_NoCycleIsHeaderOnly(t *testing.T) {
	out, err := scanHandler(scanSource{}).APIScannerScanExport(context.Background(), &scanner.ScannerScanExportInput{})
	if err != nil {
		t.Fatal(err)
	}
	if rows := parseExport(t, out); len(rows) != 1 || out.RecordCount != 0 {
		t.Fatalf("rows = %v, want header only before the first cycle", rows)
	}
}

// A name from the imported master that begins with a formula character
// must not run as a formula when the CSV is opened in Excel.
func TestScannerHandler_APIScannerScanExport_NeutralisesFormulaCells(t *testing.T) {
	cycle := scanFixture()
	cycle.Symbols = []domain.ScanSymbol{{Symbol: "1001", Name: `=HYPERLINK("http://x")`, Market: "Prime"}}
	out, _ := scanHandler(scanSource{cycle: cycle, ok: true}).APIScannerScanExport(context.Background(), &scanner.ScannerScanExportInput{})
	if got := parseExport(t, out)[1][1]; !strings.HasPrefix(got, "'=") {
		t.Errorf("name cell = %q, want a leading quote defusing the formula", got)
	}
}

func TestScannerHandler_ScanView_ExportLinkCarriesFiltersButNotPaging(t *testing.T) {
	_, body := getScan(t, scanSource{cycle: scanFixture(), ok: true}, "/scanner/scan?status=excluded&q=bet&page=2", true)
	if !strings.Contains(body, `href="/api/v1/scanner/scan/export?q=bet&amp;status=excluded"`) {
		t.Errorf("export link must carry q/status and drop page:\n%s", body)
	}
}
