package organisms_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

func renderComponent(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return sb.String()
}

// Repositories return UTC times (sqlutil.ParseTime); every SSR time label
// must still read in JST (issue #542): the TSE open 09:30 JST is 00:30Z.
func TestSSRTimesRenderInJST(t *testing.T) {
	utc := time.Date(2026, 10, 5, 0, 30, 0, 0, time.UTC)
	nextDay := time.Date(2026, 10, 5, 15, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		html string
		want []string
	}{
		{
			name: "decision history",
			html: renderComponent(t, organisms.DecisionHistoryList([]domain.JevDecision{{ID: 1, Symbol: "7203", Timestamp: utc}})),
			want: []string{"2026-10-05 09:30:00 JST"},
		},
		{
			name: "activity feed rows and caption",
			html: renderComponent(t, organisms.ActivityFeedFallback([]domain.ActivityEvent{{Type: domain.ActivityTypeJob, Timestamp: nextDay}}, utc)),
			want: []string{"2026-10-06 00:30:00 JST", "as of 2026-10-05 09:30:00 JST"},
		},
		{
			name: "scanner caption",
			html: renderScannerTableAt(t, []domain.Candidate{{Symbol: "7203", Price: 1}}, utc),
			want: []string{"Scanner Dashboard — as of 2026-10-05 09:30:00 JST</caption>"},
		},
		{
			name: "position opened and closed",
			html: renderComponent(t, molecules.PositionRow(domain.Position{ID: 1, Symbol: "7203", Side: domain.PositionSideLong, OpenedAt: utc, ClosedAt: &nextDay})),
			want: []string{"2026-10-05 09:30:00 JST", "2026-10-06 00:30:00 JST"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, want := range tt.want {
				if !strings.Contains(tt.html, want) {
					t.Errorf("missing %q in %q", want, tt.html)
				}
			}
			if strings.Contains(tt.html, "00:30:00Z") || strings.Contains(tt.html, "15:30:00Z") {
				t.Errorf("UTC timestamp leaked into SSR output: %q", tt.html)
			}
		})
	}
}
