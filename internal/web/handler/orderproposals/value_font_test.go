package orderproposals_test

import (
	"regexp"
	"strings"
	"testing"
)

// The detail's label/value lists set a number, a time or an amount in
// JetBrains Mono (brand-guide §2, T5; issues #958, #1139) and wording (買い / 成行,
// 通過, 届いた, a reason) in the text font: Japanese in the Mono fallback is off the guide.
func TestDetail_NumbersAreMonoAndWordingIsNot(t *testing.T) {
	_, body := get(t, outcomesSource(t), "/order-proposals/5")
	classOf := func(value string) string {
		m := regexp.MustCompile(`<dd(?: class="([^"]*)")?>` + regexp.QuoteMeta(value) + `</dd>`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no <dd> with value %q: %s", value, body)
		}
		return m[1]
	}
	for _, v := range []string{"2,400 円", "2026-10-17 10:00:00 JST", "5 時間 0 分", "mcp-proposal-v1"} {
		if c := classOf(v); !strings.Contains(c, "font-mono") || !strings.Contains(c, "tabular-nums") {
			t.Errorf("%q should be font-mono tabular-nums, class = %q", v, c)
		}
	}
	for _, v := range []string{"届いた", "届かなかった", "通過", "買い / 成行", "約定", "決済済み", "AIの注文提案による決済"} {
		if c := classOf(v); strings.Contains(c, "font-mono") {
			t.Errorf("wording %q should not be font-mono, class = %q", v, c)
		}
	}
}
