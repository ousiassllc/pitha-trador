package organisms_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

func ptr[T any](v T) *T { return &v }

func renderScannerTableAt(t *testing.T, candidates []domain.Candidate, asOf time.Time) string {
	t.Helper()
	var sb strings.Builder
	if err := organisms.ScannerTableFallback(candidates, asOf).Render(context.Background(), &sb); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return sb.String()
}

func renderScannerTable(t *testing.T, candidates []domain.Candidate) string {
	t.Helper()
	return renderScannerTableAt(t, candidates, time.Now())
}

// Per-cell formatting, colors, badges, links and column definitions are
// pinned against the Lit component by scanner_table_contract_test.go.

func TestScannerTableFallback_ShowsCountAndRFC3339AsOfCaption(t *testing.T) {
	asOf := time.Date(2026, 9, 26, 10, 15, 0, 0, time.FixedZone("JST", 9*60*60))
	body := renderScannerTableAt(t, []domain.Candidate{{Symbol: "7203", Price: 1}, {Symbol: "9984", Price: 2}}, asOf)

	for _, want := range []string{
		`data-testid="scanner-count"`,
		">2</span>",
		"Scanner Dashboard — as of 2026-09-26T10:15:00+09:00</caption>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected table to contain %q, got %q", want, body)
		}
	}
	if strings.Contains(body, `data-testid="scanner-empty"`) {
		t.Error("empty-state notice must not render when candidates exist")
	}
}

func TestScannerTableFallback_EmptyStateWhenNoCandidates(t *testing.T) {
	body := renderScannerTable(t, nil)

	if !strings.Contains(body, `data-testid="scanner-empty"`) {
		t.Errorf("expected empty-state notice, got %q", body)
	}
	if !strings.Contains(body, ">0</span>") {
		t.Errorf("expected candidate count 0, got %q", body)
	}
}
