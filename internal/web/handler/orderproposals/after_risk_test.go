package orderproposals_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// A proposal the application refused after Risk passed shows "Risk check: 通過"
// and the code as 後段で拒否; a Risk Engine rejection keeps "拒否" and 拒否コード
// (issue #860).
func TestDetail_RejectedAfterRiskShowsRiskPassedAndTheLaterRejection(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	testkit.SeedProposalOutcomes(t, w)
	testkit.SeedAfterRiskRejection(t, w)
	src := dbSource{w}

	_, body := get(t, src, "/order-proposals/101")
	for _, want := range []string{"Risk check", "通過", "後段で拒否", "size_zero"} {
		if !strings.Contains(body, want) {
			t.Errorf("after-risk detail lacks %q", want)
		}
	}
	if strings.Contains(body, "拒否コード") {
		t.Errorf("after-risk detail must not show the Risk rejection label: %s", body)
	}

	_, body = get(t, src, "/order-proposals/102")
	for _, want := range []string{"拒否", "拒否コード", "kill_switch_active"} {
		if !strings.Contains(body, want) {
			t.Errorf("Risk rejection detail lacks %q", want)
		}
	}
	if strings.Contains(body, "後段で拒否") {
		t.Errorf("a Risk Engine rejection is not a later rejection: %s", body)
	}
}
