package mcptests

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// health_today answers over HTTP in one call, from the server clock, with the
// fixed attention codes and no free text (the halt message stays in the DB).
func TestHealthToday_OneCallOverHTTP(t *testing.T) {
	const message = "SENTINEL-HALT-MESSAGE"
	now := testkit.At(20, 13, 0)
	path, w := testkit.HealthyDay(t, now)
	testkit.Setting(t, w, dailyguardsettings.KeyMaxTrades, "5")
	testkit.DailyGuardHalt(t, w, "2026-10-20", domain.DailyHaltReasonMaxTrades, message)
	testkit.KillEvent(t, w, 1, "operator_manual", false)
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, Now: func() time.Time { return now }})

	var d query.Health
	r := srv.call(t, healthToday, map[string]any{})
	r.data(t, healthToday, &d)
	if strings.Contains(string(r.body), message) {
		t.Fatalf("the response carries free text: %s", r.body)
	}
	if d.Date != "2026-10-20" || !d.TradingDay || !d.NeedsAttention {
		t.Errorf("date/trading_day/needs_attention = %s/%v/%v", d.Date, d.TradingDay, d.NeedsAttention)
	}
	if want := []string{query.AttentionDailyGuardHalted, query.AttentionKillSwitchActive}; !slices.Equal(d.AttentionCodes, want) {
		t.Errorf("attention_codes = %v, want %v", d.AttentionCodes, want)
	}
	if d.PaperTrades.OpenedCount != 1 || !d.MetaModel.Adopted || !d.DailyGuard.Halted || !d.KillSwitch.Active {
		t.Errorf("parts = %+v %+v %+v %+v", d.PaperTrades, d.MetaModel, d.DailyGuard, d.KillSwitch)
	}
}

// health_today takes no arguments: any argument is invalid_argument.
func TestHealthToday_RejectsArguments(t *testing.T) {
	srv := seededServer(t, nil)
	for _, a := range []map[string]any{{"from": "2026-10-01", "to": "2026-10-02"}, {"limit": 1}, {"x": 1}} {
		srv.call(t, healthToday, a).wantError(t, 422, paperresultsmcp.CodeInvalidArgument)
	}
}
