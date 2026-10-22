package orderproposals_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The signal block of the detail screen (issue #860): a proposal the application
// refused after Risk passed shows "Risk通過・後段で拒否"; one that passed Risk and
// ended expired / cancelled / waiting for the session shows that end instead;
// a Risk Engine rejection keeps "拒否" and "拒否コード".
func TestDetail_SignalBlockTellsRiskPassedEndsApart(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	testkit.SeedProposalOutcomes(t, w)
	testkit.SeedAfterRiskRejection(t, w)
	src := dbSource{w}
	const later = "Risk通過・後段で拒否"
	for _, c := range []struct {
		name, path string
		want, not  []string
	}{
		{"refused after Risk", "/order-proposals/101", []string{later, "理由コード", "size_zero"}, []string{"拒否コード", "Risk通過・期限切れ", "Risk通過・取消", "Risk通過・セッション待ち"}},
		{"expired", "/order-proposals/103", []string{"Risk通過・期限切れ", "理由コード", "expired"}, []string{later, "拒否コード"}},
		{"cancelled", "/order-proposals/104", []string{"Risk通過・取消", "理由コード", "cancel_requested"}, []string{later, "拒否コード"}},
		{"waiting for the session", "/order-proposals/105", []string{"Risk通過・セッション待ち", "理由コード", "outside_trading_session"}, []string{later, "拒否コード"}},
		{"Risk Engine rejection", "/order-proposals/102", []string{"拒否", "拒否コード", "kill_switch_active"}, []string{later, "Risk通過"}},
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
