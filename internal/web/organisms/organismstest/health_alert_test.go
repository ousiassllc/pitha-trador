package organismstest

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// issue #863: not logged in and not running read differently - the first says
// to log in, the second to start kabuステーション (and check the API setting).
// The Home screen's 要対応 cards reuse these texts.
func TestHealthAlertWording_KabuNotLoggedInAndUnreachableTellWhatToDo(t *testing.T) {
	loginTitle, loginAction := organisms.HealthAlertTitle("kabu_not_logged_in"), organisms.HealthAlertAction("kabu_not_logged_in")
	if !strings.Contains(loginTitle, "未ログインです") || !strings.Contains(loginAction, "kabuステーションにログインしてください") {
		t.Errorf("not-logged-in wording = %q / %q", loginTitle, loginAction)
	}
	if strings.Contains(loginAction, "起動してください") {
		t.Errorf("not-logged-in action tells the operator to start kabuステーション: %q", loginAction)
	}
	downTitle, downAction := organisms.HealthAlertTitle("kabu_unreachable"), organisms.HealthAlertAction("kabu_unreachable")
	for _, want := range []string{"接続できません", "未起動"} {
		if !strings.Contains(downTitle, want) {
			t.Errorf("unreachable title %q lacks %q", downTitle, want)
		}
	}
	if !strings.Contains(downAction, "kabuステーションを起動してログインしてください") {
		t.Errorf("unreachable action = %q", downAction)
	}
}

func TestHealthAlertWording_MarketDataIdleAndUnknownCode(t *testing.T) {
	if got := organisms.HealthAlertTitle("market_data_idle"); !strings.Contains(got, "市況データの取得が止まっています") {
		t.Errorf("market_data_idle title = %q", got)
	}
	if got := organisms.HealthAlertAction("market_data_idle"); !strings.Contains(got, "アクティビティ画面") {
		t.Errorf("market_data_idle action = %q", got)
	}
	if got := organisms.HealthAlertAction("unknown"); got != "" {
		t.Errorf("unknown code action = %q, want empty", got)
	}
}
