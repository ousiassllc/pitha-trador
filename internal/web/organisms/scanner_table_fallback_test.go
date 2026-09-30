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

func renderScannerTable(t *testing.T, candidates []domain.Candidate) string {
	t.Helper()
	var sb strings.Builder
	if err := organisms.ScannerTableFallback(candidates, time.Now()).Render(context.Background(), &sb); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return sb.String()
}

func TestScannerTableFallback_ShowsCountSignedReturnsAndBadges(t *testing.T) {
	body := renderScannerTable(t, []domain.Candidate{
		{
			Symbol: "7203", Price: 2831.5,
			Return1m: ptr(0.12), Return5m: ptr(-0.3),
			JevDirection: ptr("LONG"), JevConfidence: ptr(0.74), EntryQuality: ptr("strong"),
		},
		{Symbol: "9984", Price: 7000}, // no Jev evaluation yet
	})

	for _, want := range []string{
		`data-testid="scanner-count"`,
		">2</span>",      // candidate count
		"+0.12", "-0.30", // signed returns
		"text-green-700", "text-red-700", // sign colors
		"bg-green-100", // LONG badge / strong entry-quality badge
		"74%",
		"pending", // 9984 has no Jev evaluation
		`href="/symbols/7203"`,
		`title="`, // header tooltips
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
