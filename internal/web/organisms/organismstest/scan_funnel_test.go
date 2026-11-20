package organismstest

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// The scan funnel's four steps are named in plain Japanese, with the numbers
// and their test ids unchanged, and its help names no setting key (issue #904).
func TestScanFunnel_StepsAreNamedInPlainJapaneseAndKeepTheirNumbers(t *testing.T) {
	v := organisms.ScanPanelView{HasCycle: true}
	v.Cycle.Funnel = domain.ScanFunnel{Universe: 45, FeatureComputed: 44, FastScreenerPassed: 8, ScoutEvaluated: 6, ScoutPassed: 2}
	html := renderComponent(t, organisms.ScanFunnel(v))

	assertContains(t, html,
		"スキャン対象の銘柄数", "市況データを取得できた銘柄数", "絞り込みを通過した銘柄数", "Jev Scout を通過した銘柄数",
		`data-testid="scan-funnel-universe">45<`, `data-testid="scan-funnel-features">44<`,
		`data-testid="scan-funnel-fast">8<`, `data-testid="scan-funnel-scout">2<`,
		"6/8件判定済み")
	for _, gone := range []string{"ユニバース", "特徴量算出", "Fast Screener通過", "Scout通過", "scan.full_scan_enabled", "設定キー", "上位N件"} {
		if strings.Contains(html, gone) {
			t.Errorf("the funnel still shows %q", gone)
		}
	}
}

// issue #987: each funnel step nests in the scan panel card, so it is a surface-sel panel
// on 16px corners with the small-card padding (16) and no frame.
func TestScanFunnel_StepsAreNestedMediumRadiusPanels(t *testing.T) {
	v := organisms.ScanPanelView{HasCycle: true}
	html := renderComponent(t, organisms.ScanFunnel(v))
	if !strings.Contains(html, `<li class="rounded-card bg-surface-sel p-4">`) {
		t.Errorf("funnel steps must be rounded-card bg-surface-sel p-4 panels: %s", html)
	}
}

// A funnel step's label is a note on a surface-sel ground: text-note text-mute-on-soft
// (brand-guide app UI: captions are 13px).
func TestScanFunnel_StepLabelsAreTextNote(t *testing.T) {
	v := organisms.ScanPanelView{HasCycle: true}
	html := renderComponent(t, organisms.ScanFunnel(v))
	if !strings.Contains(html, `<div class="text-note text-mute-on-soft">`) || strings.Contains(html, `<div class="text-sm text-mute">`) {
		t.Errorf("funnel step labels must be text-note text-mute-on-soft: %s", html)
	}
}
