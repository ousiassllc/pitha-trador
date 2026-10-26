package orderproposals_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The signal block of the detail screen (issue #860): a proposal the application
// refused after Risk passed shows "リスク判定は通過・後段で拒否"; one that passed Risk and
// ended expired / cancelled / waiting for the session shows that end instead;
// a Risk Engine rejection keeps "拒否" and "拒否の理由".
func TestDetail_SignalBlockTellsRiskPassedEndsApart(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	testkit.SeedProposalOutcomes(t, w)
	testkit.SeedAfterRiskRejection(t, w)
	src := dbSource{w}
	const later = "リスク判定は通過・後段で拒否"
	for _, c := range []struct {
		name, path string
		want, not  []string
	}{
		{"refused after Risk", "/order-proposals/101", []string{later, "数量が 0 株になる"}, []string{"拒否の理由", "size_zero", "リスク判定は通過・期限切れ", "リスク判定は通過・取消", "リスク判定は通過・セッション待ち"}},
		{"expired", "/order-proposals/103", []string{"リスク判定は通過・期限切れ", "期限切れ"}, []string{later, "拒否の理由"}},
		{"cancelled", "/order-proposals/104", []string{"リスク判定は通過・取消", "取消の依頼"}, []string{later, "拒否の理由", "cancel_requested"}},
		{"waiting for the session", "/order-proposals/105", []string{"リスク判定は通過・セッション待ち", "立会時間外"}, []string{later, "拒否の理由", "outside_trading_session"}},
		{"Risk Engine rejection", "/order-proposals/102", []string{"拒否", "拒否の理由", "Kill Switch が発動中"}, []string{later, "リスク判定は通過", "kill_switch_active"}},
	} {
		_, body := get(t, src, c.path)
		for _, w := range c.want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: detail lacks %q", c.name, w)
			}
		}
		for _, n := range c.not {
			if strings.Contains(body, n) {
				t.Errorf("%s: detail must not contain %q", c.name, n)
			}
		}
	}
}
