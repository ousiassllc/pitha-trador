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

// The description and legend explain the dashboard to first-time users
// (issue #239); the legend must spell out the color meaning in words, not
// by color alone, and the direction/quality vocabulary must be listed.
func TestScannerPage_ShowsDescriptionAndLegendAboveTable(t *testing.T) {
	body := renderScannerPage(t, []domain.Candidate{{Symbol: "7203", Price: 1}})

	desc := strings.Index(body, `data-testid="scanner-description"`)
	legend := strings.Index(body, `data-testid="scanner-legend"`)
	table := strings.Index(body, "<table")
	if desc < 0 || legend < 0 || table < 0 || desc > legend || legend > table {
		t.Fatalf("description at %d, legend at %d, table at %d; want description < legend < table", desc, legend, table)
	}
	for _, want := range []string{
		"= プラス / LONG",
		"= マイナス / SHORT",
		"poor &lt; fair &lt; good &lt; strong &lt; exceptional",
		`src="/static/dist/js/scanner-table/pitha-scanner-table.js"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}
