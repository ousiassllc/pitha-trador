package pages_test

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

func renderSymbolDetail(t *testing.T, position *domain.Position) string {
	t.Helper()
	var buf bytes.Buffer
	props := pages.SymbolDetailProps{Symbol: "7203", Position: position}
	if err := pages.SymbolDetailPage(props).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// issue #358: the Position table must label its columns, and the header must
// have as many columns as PositionRow emits cells - for both an open row (9th
// cell "—", 10th cell Close button) and a closed row (closed-at, exit reason).
func TestSymbolDetailPage_PositionTableHeaderMatchesRowColumns(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	reason := domain.ExitReasonStopLoss
	pnl := -12.5
	open := &domain.Position{ID: 1, Symbol: "7203", Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 100, CurrentPrice: 101, OpenedAt: now}
	closed := &domain.Position{ID: 2, Symbol: "7203", Side: domain.PositionSideShort, Quantity: 100, EntryPrice: 100, CurrentPrice: 99, RealizedPnL: &pnl, OpenedAt: now, ClosedAt: &now, ExitReason: &reason}

	panelRe := regexp.MustCompile(`(?s)<section[^>]*id="position-panel".*?</section>`)
	for name, pos := range map[string]*domain.Position{"open": open, "closed": closed} {
		t.Run(name, func(t *testing.T) {
			panel := panelRe.FindString(renderSymbolDetail(t, pos))
			if panel == "" {
				t.Fatal("position-panel not rendered")
			}
			thead := regexp.MustCompile(`(?s)<thead>.*?</thead>`).FindString(panel)
			if thead == "" {
				t.Fatal("position table has no <thead>")
			}
			ths := strings.Count(thead, `<th scope="col"`)
			if got := strings.Count(thead, "<th "); got != ths {
				t.Errorf("%d of %d <th> lack scope=\"col\"", got-ths, got)
			}
			row := regexp.MustCompile(`(?s)<tr[^>]*data-testid="position-row".*?</tr>`).FindString(panel)
			if row == "" {
				t.Fatal("position-row not rendered")
			}
			if tds := strings.Count(row, "<td"); tds != ths {
				t.Errorf("<th> count = %d, PositionRow <td> count = %d", ths, tds)
			}
		})
	}
}
