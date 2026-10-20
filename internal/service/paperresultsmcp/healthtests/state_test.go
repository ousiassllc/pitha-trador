package healthtests

import (
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperbootstrapsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func alertCodes(alerts []query.Alert) []string {
	out := []string{}
	for _, a := range alerts {
		out = append(out, a.Code)
	}
	return out
}

// A weekend or a holiday is no outage: whatever the day's clock-bound state
// (no trades, a stale detector, an alert row left in the table), no code is
// raised and the statuses say "not watched". Real state (model, kill switch)
// still shows.
func TestNonTradingDayRaisesNoFalseAlerts(t *testing.T) {
	for name, now := range map[string]time.Time{
		"Saturday":     at(24, 10, 0),
		"Sunday night": at(25, 22, 0),
		"holiday":      at(12, 10, 0), // Sports Day
	} {
		t.Run(name, func(t *testing.T) {
			_, w := testkit.HealthyDay(t, now)
			// Alert rows the detector left behind, and a detector that has stopped.
			testkit.HealthAlert(t, w, domain.HealthAlertKabuNotLoggedIn, now.Add(-time.Hour), now)
			testkit.HealthAlert(t, w, domain.HealthAlertMarketDataIdle, now.Add(-time.Hour), now)
			testkit.DetectorChecked(t, w, now.Add(-time.Hour))
			h := healthAt(t, w, now)
			if h.TradingDay || h.NeedsAttention || len(h.AttentionCodes) != 0 {
				t.Fatalf("health = trading_day %v, codes %v; want a quiet non-trading day", h.TradingDay, h.AttentionCodes)
			}
			if h.MarketData.KabuLogin != query.StatusNotWatched || h.MarketData.MarketData != query.StatusNotWatched {
				t.Errorf("statuses = %s / %s, want not_watched", h.MarketData.KabuLogin, h.MarketData.MarketData)
			}
			if h.PaperTrades.OpenedCount != 0 {
				t.Errorf("paper trades = %+v, want none", h.PaperTrades)
			}

			// State that is not about the clock still shows on a non-trading day.
			testkit.MustExec(t, w, `DELETE FROM metalabel_models`)
			testkit.KillEvent(t, w, 1, "operator_manual", false)
			got := healthAt(t, w, now).AttentionCodes
			if want := []string{query.AttentionNoActiveModel, query.AttentionKillSwitchActive}; !slices.Equal(got, want) {
				t.Errorf("codes with real state = %v, want %v", got, want)
			}
		})
	}
}

// The kabu login and market-data statuses restate the #802 alerts for the
// hours the detector watches; an app that is not running cannot vouch for kabu
// ("unknown"), while the idle rule is derived from the jobs.
func TestMarketDataStatuses(t *testing.T) {
	kabu, idle := domain.HealthAlertKabuNotLoggedIn, domain.HealthAlertMarketDataIdle
	none := func(*testing.T, *sql.DB, time.Time) {}
	stopped := func(t *testing.T, w *sql.DB, now time.Time) { testkit.DetectorChecked(t, w, now.Add(-10*time.Minute)) }
	tests := []struct {
		name                 string
		now                  time.Time
		setup                func(t *testing.T, w *sql.DB, now time.Time)
		wantKabu, wantMarket string
		wantRunning          bool
		wantMarketDataCodes  []string
	}{
		{"before the 08:45 check", at(20, 8, 30), none, "not_watched", "not_watched", true, nil},
		{"in session, all well", at(20, 10, 0), none, "ok", "running", true, nil},
		{"in session, kabu logged out", at(20, 10, 0), func(t *testing.T, w *sql.DB, now time.Time) {
			testkit.HealthAlert(t, w, kabu, at(20, 8, 45), now)
		}, "not_logged_in", "running", true, []string{kabu}},
		{"in session, market data idle", at(20, 10, 0), func(t *testing.T, w *sql.DB, now time.Time) {
			testkit.HealthAlert(t, w, idle, at(20, 9, 40), now)
		}, "ok", "idle", true, []string{idle}},
		{"lunch break: kabu is watched, market data is not", at(20, 12, 0), none, "ok", "not_watched", true, nil},
		{"after the close", at(20, 15, 30), none, "not_watched", "not_watched", true, nil},
		{"app not running in session: kabu unknown, idle from the jobs", at(20, 10, 0), func(t *testing.T, w *sql.DB, now time.Time) {
			stopped(t, w, now)
			testkit.MarketDataJob(t, w, "succeeded", at(20, 9, 0))
		}, "unknown", "idle", false, []string{idle}},
		{"app not running, market data still flowing", at(20, 10, 0), func(t *testing.T, w *sql.DB, now time.Time) {
			stopped(t, w, now)
			testkit.MarketDataJob(t, w, "succeeded", at(20, 9, 58))
		}, "unknown", "running", false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, w := testkit.HealthyDay(t, tt.now)
			tt.setup(t, w, tt.now)
			h := healthAt(t, w, tt.now)
			m := h.MarketData
			if m.KabuLogin != tt.wantKabu || m.MarketData != tt.wantMarket || m.AppRunning != tt.wantRunning {
				t.Errorf("kabu/market/app_running = %s/%s/%v, want %s/%s/%v",
					m.KabuLogin, m.MarketData, m.AppRunning, tt.wantKabu, tt.wantMarket, tt.wantRunning)
			}
			var got []string
			for _, c := range h.AttentionCodes {
				if c == kabu || c == idle {
					got = append(got, c)
				}
			}
			if !slices.Equal(got, tt.wantMarketDataCodes) {
				t.Errorf("market-data codes = %v, want %v", got, tt.wantMarketDataCodes)
			}
			// Exactly the alerts collection_status shows.
			want, err := query.HealthAlerts(t.Context(), w, tt.now)
			if err != nil || !slices.Equal(alertCodes(m.Alerts), alertCodes(want)) {
				t.Errorf("alerts = %v, collection_status alerts = %v (%v)", alertCodes(m.Alerts), alertCodes(want), err)
			}
		})
	}
}

// The bootstrap state: the Settings switch, whether it is effective (on and no
// model adopted) and how many of today's signals it decided.
func TestBootstrap(t *testing.T) {
	now := at(20, 13, 0)
	tests := []struct {
		name                     string
		value                    string // "" = no row
		noModel                  bool
		wantEnabled, wantInForce bool
	}{
		{"default off, no row", "", true, false, false},
		{"on, no model: in force", "1", true, true, true},
		{"on, a model is adopted: switch on but not in force", "1", false, true, false},
		{"off", "0", true, false, false},
		{"an unparsable row counts as off", "yes", true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, w := testkit.HealthyDay(t, now)
			if tt.value != "" {
				testkit.Setting(t, w, paperbootstrapsettings.Key, tt.value)
			}
			if tt.noModel {
				testkit.MustExec(t, w, `DELETE FROM metalabel_models`)
			}
			for _, s := range []struct {
				at      time.Time
				version string
			}{
				{at(20, 9, 5), domain.PolicyVersionPaperBootstrap},
				{at(20, 9, 6), domain.PolicyVersionPaperBootstrap + "+sol-1"},
				{at(20, 9, 7), "policy-v2"},
				{at(19, 9, 5), domain.PolicyVersionPaperBootstrap}, // yesterday
			} {
				testkit.MustExec(t, w, `INSERT INTO trade_signals (instrument_id, symbol, timestamp, direction, policy_version, risk_passed)
					VALUES (1, '7203', ?, 'NONE', ?, 0)`, stored(t, s.at), s.version)
			}
			b := healthAt(t, w, now).Bootstrap
			if b.Enabled != tt.wantEnabled || b.Effective != tt.wantInForce || b.SignalsToday != 2 {
				t.Errorf("bootstrap = %+v, want enabled %v effective %v signals_today 2", b, tt.wantEnabled, tt.wantInForce)
			}
		})
	}
}

// The halt and kill switch details are fixed codes and counts, never the
// halt's message text; unknown retrain codes fold into "other".
func TestDetailsAreFixedCodes(t *testing.T) {
	now := at(20, 13, 0)
	const message = "SENTINEL-HALT-MESSAGE"
	_, w := testkit.HealthyDay(t, now)
	testkit.Setting(t, w, dailyguardsettings.KeyMaxTrades, "5")
	testkit.DailyGuardHalt(t, w, "2026-10-20", domain.DailyHaltReasonMaxTrades, message)
	testkit.KillEvent(t, w, 1, "operator_manual", false)
	testkit.KillEvent(t, w, 2, "daily_loss_limit", false)
	testkit.RetrainSkip(t, w, at(20, 3, 0), "unlisted_code_"+message)
	testkit.MustExec(t, w, `UPDATE metalabel_models SET trained_at = ?`, stored(t, at(10, 9, 0)))

	h := healthAt(t, w, now)
	if g := h.DailyGuard; !g.Halted || g.Reason == nil || *g.Reason != domain.DailyHaltReasonMaxTrades ||
		g.HaltedAt == nil || *g.HaltedAt != "2026-10-20T04:00:00Z" {
		t.Errorf("daily_guard = %+v", g)
	}
	if k := h.KillSwitch; !k.Active || k.UnresolvedCount != 2 || k.Since == nil || *k.Since != "2026-10-20T00:00:00Z" ||
		!slices.Equal(k.Reasons, []string{"daily_loss_limit", "operator_manual"}) {
		t.Errorf("kill_switch = %+v", k)
	}
	if r := h.Retrain; !r.Skipped || r.ReasonCode == nil || *r.ReasonCode != "other" {
		t.Errorf("retrain = %+v, want skipped with the unknown code folded into other", r)
	}
	if h.PaperTrades.OpenedCount != 1 || !h.MetaModel.Adopted || h.MetaModel.Version == nil || *h.MetaModel.Version != "v1" {
		t.Errorf("paper_trades/meta_model = %+v / %+v", h.PaperTrades, h.MetaModel)
	}
}

// Only the fixed runtime_settings keys of health_today are ever readable.
func TestReadsOnlyFixedSettingKeys(t *testing.T) {
	want := append(dailyguardsettings.Keys(), "system.killed", "system.daily_guard", paperbootstrapsettings.Key)
	got := query.ReadableSettingKeys()
	if !slices.Equal(got, want) {
		t.Fatalf("ReadableSettingKeys = %v, want %v", got, want)
	}
	for _, k := range got {
		for _, bad := range []string{"secret", "path", "token", "key_file", "llm"} {
			if strings.Contains(k, bad) {
				t.Errorf("readable key %q looks secret-bearing (%q)", k, bad)
			}
		}
	}
}
