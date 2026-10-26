package orderproposals_test

import (
	"strings"
	"testing"
)

// The explanations moved into help marks stay in the DOM (a screen reader reads
// them through aria-describedby), while the status, the reason code and the
// loss stay on the screen itself.
func TestDetail_HelpMarksCarryTheExplanationsAndTheFactsStayVisible(t *testing.T) {
	_, body := get(t, outcomesSource(t), "/order-proposals/3")
	for _, want := range []string{
		`data-help-mark-text`, `aria-describedby="help-order-proposal-loss-1"`, `id="help-order-proposal-loss-1"`,
		"数量 ×（約定見込み価格 − 損切り価格）で見積もった最大損失です。",
		"拒否", "枠の予算を超える", "最大損失", "アプリの見積り",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
	if strings.Contains(body, "（数量 ×（約定見込み価格 − 損切り価格））") {
		t.Errorf("the loss formula is a help text, not part of the label")
	}
	_, list := get(t, outcomesSource(t), "/order-proposals?from=2026-10-01&to=2026-10-31")
	for _, want := range []string{`id="help-order-proposals-about"`, `id="help-order-proposals-max-loss"`, "Paper の注文提案"} {
		if !strings.Contains(list, want) {
			t.Errorf("list lacks %q", want)
		}
	}
}
