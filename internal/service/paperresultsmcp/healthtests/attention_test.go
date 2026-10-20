// Package healthtests tests health_today's logic (issue #808) against the
// migrated test database, through query.HealthToday. The tool's HTTP side
// (arguments, response keys, leaks, read-only SQL) is tested in mcptests.
package healthtests

import (
	"database/sql"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

var at = testkit.At

func stored(t *testing.T, ts time.Time) string { return testkit.StoredAt(t, ts) }

func healthAt(t *testing.T, w *sql.DB, now time.Time) query.Health {
	t.Helper()
	h, err := query.HealthToday(t.Context(), w, now)
	if err != nil {
		t.Fatalf("HealthToday: %v", err)
	}
	if h.AttentionCodes == nil || h.MarketData.Alerts == nil || h.MissingSessionDates == nil ||
		h.InsufficientHistoryDates == nil || h.KillSwitch.Reasons == nil {
		t.Fatalf("a list is nil, want an empty one (JSON []): %+v", h)
	}
	if h.NeedsAttention != (len(h.AttentionCodes) > 0) {
		t.Fatalf("needs_attention = %v with codes %v", h.NeedsAttention, h.AttentionCodes)
	}
	return h
}

// Each attention code switches on from its own condition alone and off again
// when the condition goes; the healthy baseline raises none.
func TestEachAttentionCodeOnAndOff(t *testing.T) {
	afternoon := at(20, 13, 0) // Tuesday, afternoon session
	kabu, idle := domain.HealthAlertKabuNotLoggedIn, domain.HealthAlertMarketDataIdle
	del := func(table string) func(*testing.T, *sql.DB) {
		return func(t *testing.T, w *sql.DB) { testkit.MustExec(t, w, `DELETE FROM `+table) }
	}
	tests := []struct {
		name  string
		now   time.Time // zero: afternoon
		setup func(t *testing.T, w *sql.DB)
		want  []string
	}{
		{name: "healthy", setup: func(*testing.T, *sql.DB) {}},

		{name: "no_paper_trades_today on: nothing opened today", want: []string{query.AttentionNoPaperTradesToday}, setup: del("positions")},
		{name: "no_paper_trades_today on: only yesterday's position", want: []string{query.AttentionNoPaperTradesToday},
			setup: func(t *testing.T, w *sql.DB) {
				del("positions")(t, w)
				testkit.OpenPosition(t, w, at(19, 10, 0))
			}},
		{name: "no_paper_trades_today off: the morning is too early to say", now: at(20, 10, 0), setup: del("positions")},
		{name: "no_paper_trades_today off: opened and already closed today is a trade",
			setup: func(t *testing.T, w *sql.DB) {
				testkit.MustExec(t, w, `UPDATE positions SET closed_at = ?, realized_pnl = 10`, stored(t, at(20, 11, 0)))
			}},

		{name: "no_active_model on: none trained", want: []string{query.AttentionNoActiveModel}, setup: del("metalabel_models")},
		{name: "no_active_model on: only a rejected one", want: []string{query.AttentionNoActiveModel},
			setup: func(t *testing.T, w *sql.DB) {
				testkit.MustExec(t, w, `UPDATE metalabel_models SET status = 'rejected', reject_reason = 'auc'`)
			}},

		{name: "kabu_not_logged_in on", want: []string{kabu},
			setup: func(t *testing.T, w *sql.DB) { testkit.HealthAlert(t, w, kabu, at(20, 8, 45), afternoon) }},
		{name: "market_data_idle on", want: []string{idle},
			setup: func(t *testing.T, w *sql.DB) { testkit.HealthAlert(t, w, idle, at(20, 12, 40), afternoon) }},

		{name: "missing_session on: a trading day without a snapshot", want: []string{query.AttentionMissingSession},
			setup: func(t *testing.T, w *sql.DB) {
				testkit.MustExec(t, w, `DELETE FROM market_snapshots WHERE timestamp >= ? AND timestamp < ?`,
					stored(t, at(15, 0, 0)), stored(t, at(16, 0, 0)))
			}},
		{name: "missing_session off: today is not judged before its close", setup: func(*testing.T, *sql.DB) {}},
		{name: "missing_session on: today with no snapshot after the close", now: at(20, 15, 30),
			want: []string{query.AttentionMissingSession}, setup: func(*testing.T, *sql.DB) {}},

		{name: "daily_guard_halted on", want: []string{query.AttentionDailyGuardHalted}, setup: func(t *testing.T, w *sql.DB) {
			testkit.Setting(t, w, dailyguardsettings.KeyMaxTrades, "5")
			testkit.DailyGuardHalt(t, w, "2026-10-20", domain.DailyHaltReasonMaxTrades, "m")
		}},
		{name: "daily_guard_halted off: yesterday's halt is over", setup: func(t *testing.T, w *sql.DB) {
			testkit.Setting(t, w, dailyguardsettings.KeyMaxTrades, "5")
			testkit.DailyGuardHalt(t, w, "2026-10-19", domain.DailyHaltReasonMaxTrades, "m")
		}},
		{name: "daily_guard_halted off: the rule was switched off since", setup: func(t *testing.T, w *sql.DB) {
			testkit.DailyGuardHalt(t, w, "2026-10-20", domain.DailyHaltReasonMaxTrades, "m")
		}},

		{name: "kill_switch_active on: an unresolved event", want: []string{query.AttentionKillSwitchActive},
			setup: func(t *testing.T, w *sql.DB) { testkit.KillEvent(t, w, 1, "operator_manual", false) }},
		{name: "kill_switch_active off: a resolved event",
			setup: func(t *testing.T, w *sql.DB) { testkit.KillEvent(t, w, 1, "daily_loss_limit", true) }},
		{name: "kill_switch_active on: the system.killed flag alone", want: []string{query.AttentionKillSwitchActive},
			setup: func(t *testing.T, w *sql.DB) { testkit.Setting(t, w, "system.killed", "true") }},
		{name: "kill_switch_active off: a cleared flag",
			setup: func(t *testing.T, w *sql.DB) { testkit.Setting(t, w, "system.killed", "false") }},

		{name: "retrain_skipped on: skipped last night, no model since", want: []string{query.AttentionRetrainSkipped},
			setup: func(t *testing.T, w *sql.DB) { testkit.RetrainSkip(t, w, at(20, 3, 0), "too_few_rows_before_oos") }},
		{name: "retrain_skipped off: a model was trained after the skip",
			setup: func(t *testing.T, w *sql.DB) { testkit.RetrainSkip(t, w, at(19, 3, 0), "too_few_rows_before_oos") }},
		{name: "retrain_skipped off: a skip older than a day is no news", setup: func(t *testing.T, w *sql.DB) {
			testkit.MustExec(t, w, `UPDATE metalabel_models SET trained_at = ?`, stored(t, at(10, 9, 0)))
			testkit.RetrainSkip(t, w, at(17, 3, 0), "too_few_rows_before_oos")
		}},

		{name: "several at once, in the documented order", want: []string{
			query.AttentionNoPaperTradesToday, query.AttentionNoActiveModel, kabu, query.AttentionKillSwitchActive,
		}, setup: func(t *testing.T, w *sql.DB) {
			del("positions")(t, w)
			del("metalabel_models")(t, w)
			testkit.HealthAlert(t, w, kabu, at(20, 8, 45), afternoon)
			testkit.KillEvent(t, w, 1, "operator_manual", false)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := tt.now
			if now.IsZero() {
				now = afternoon
			}
			_, w := testkit.HealthyDay(t, now)
			tt.setup(t, w)
			got := healthAt(t, w, now).AttentionCodes
			if !slices.Equal(got, tt.want) && len(got)+len(tt.want) > 0 {
				t.Errorf("attention codes = %v, want %v", got, tt.want)
			}
		})
	}
}

// Every code the tool can raise is in the documented list, in this order.
func TestAttentionCodesAreTheDocumentedFixedSet(t *testing.T) {
	want := []string{"no_paper_trades_today", "no_active_model", "kabu_not_logged_in", "market_data_idle",
		"missing_session", "daily_guard_halted", "kill_switch_active", "retrain_skipped"}
	if got := query.AttentionCodes(); !slices.Equal(got, want) {
		t.Fatalf("AttentionCodes = %v, want %v", got, want)
	}
	// The two health alerts are the codes of the #802 in-app banner, not a copy.
	if query.AttentionKabuNotLoggedIn != domain.HealthAlertKabuNotLoggedIn || query.AttentionMarketDataIdle != domain.HealthAlertMarketDataIdle {
		t.Fatal("health alert codes drifted from the in-app banner (domain.HealthAlert*)")
	}
}
