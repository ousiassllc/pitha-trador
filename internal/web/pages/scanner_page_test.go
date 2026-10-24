package pages_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

func renderScannerPage(t *testing.T, candidates []domain.Candidate) string {
	t.Helper()
	var buf bytes.Buffer
	if err := pages.ScannerPage(candidates, time.Now(), organisms.ScanPanelView{}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// The page has no intro paragraph or color legend of its own: those moved
// into the help mark beside the candidate count (issue #902). The Lit
// script that hydrates the table is still loaded.
func TestScannerPage_HasNoIntroAndLoadsTableScript(t *testing.T) {
	body := renderScannerPage(t, []domain.Candidate{{Symbol: "7203", Price: 1}})

	for _, gone := range []string{`data-testid="scanner-description"`, `data-testid="scanner-legend"`, `data-testid="scanner-column-help"`} {
		if strings.Contains(body, gone) {
			t.Errorf("page still has %q", gone)
		}
	}
	for _, want := range []string{
		`id="help-scanner-count"`,
		"緑はプラス・LONG、赤はマイナス・SHORT",
		`src="/static/dist/js/scanner-table/pitha-scanner-table.js"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}
