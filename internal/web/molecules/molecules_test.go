package molecules_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return sb.String()
}

// Symbol Detail shows the same rounded confidence as the Scanner SSR table
// (issue #677): 0.125 is exactly representable, so %.0f would give 12%.
func TestSignalBadgeGroup_ConfidenceRoundsHalfAwayFromZero(t *testing.T) {
	direction, confidence := "LONG", 0.125
	html := render(t, molecules.SignalBadgeGroup(&direction, &confidence, nil))
	if !strings.Contains(html, ">13%<") || strings.Contains(html, "12%") {
		t.Errorf("confidence 0.125 must render 13%%: %s", html)
	}
}

func TestSignalBadgeGroup_PendingAndMissingConfidence(t *testing.T) {
	if html := render(t, molecules.SignalBadgeGroup(nil, nil, nil)); !strings.Contains(html, "pending") {
		t.Errorf("nil direction must render pending: %s", html)
	}
	direction := "LONG"
	if html := render(t, molecules.SignalBadgeGroup(&direction, nil, nil)); !strings.Contains(html, ">—<") {
		t.Errorf("nil confidence must render a dash: %s", html)
	}
}

// htmx restores focus after a swap only to a same-id element (issue #676),
// so the row's buttons carry stable ids; the delete button, which vanishes
// once the value is gone, hands focus back to the input.
func TestSecretFieldRow_ButtonsHaveStableIDs(t *testing.T) {
	configured := render(t, molecules.SecretFieldRow(molecules.SecretFieldRowProps{Key: "jquants_api_key", Label: "J-Quants", Configured: true}))
	for _, want := range []string{`id="save-jquants_api_key"`, `id="delete-jquants_api_key"`, `data-focus-after-swap="secret-input-jquants_api_key"`, `id="secret-input-jquants_api_key"`} {
		if !strings.Contains(configured, want) {
			t.Errorf("configured row missing %q: %s", want, configured)
		}
	}
	unconfigured := render(t, molecules.SecretFieldRow(molecules.SecretFieldRowProps{Key: "jquants_api_key", Label: "J-Quants"}))
	if !strings.Contains(unconfigured, `id="save-jquants_api_key"`) || strings.Contains(unconfigured, "delete-jquants_api_key") {
		t.Errorf("unconfigured row must keep the save id and have no delete button: %s", unconfigured)
	}
}

// Closing a position swaps its row (outerHTML) for a closed row that has no
// Close button, so the focus would fall to <body> (issue #681): the button
// has a stable id and hands focus to the replaced row, whose id the
// response fragment also renders.
func TestPositionRow_CloseButtonHandsFocusToReplacedRow(t *testing.T) {
	now := time.Now()
	pnl, reason := 12.5, domain.ExitReasonManual
	open := domain.Position{ID: 7, Symbol: "7203", Side: domain.PositionSideLong, Quantity: 100, OpenedAt: now}
	closed := open
	closed.RealizedPnL, closed.ClosedAt, closed.ExitReason = &pnl, &now, &reason

	openHTML := render(t, molecules.PositionRow(open))
	for _, want := range []string{`id="close-position-7"`, `data-focus-after-swap="position-row-7"`, `hx-target="#position-row-7"`} {
		if !strings.Contains(openHTML, want) {
			t.Errorf("open row missing %q: %s", want, openHTML)
		}
	}
	closedHTML := render(t, molecules.PositionRow(closed))
	if !strings.Contains(closedHTML, `id="position-row-7"`) {
		t.Errorf("closed row must keep the focus target id: %s", closedHTML)
	}
	if strings.Contains(closedHTML, "close-position-7") {
		t.Errorf("closed row must not render the Close button: %s", closedHTML)
	}
}
