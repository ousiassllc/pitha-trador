package homeview_test

import (
	"database/sql"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/homeview"
	"github.com/ousiassllc/pitha-trador/internal/config/paperbootstrapsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/insight"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/home"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

// now is 13:00 JST on Tuesday 2026-10-20, a trading day.
var now = testkit.At(20, 13, 0)

func reader(t *testing.T, w *sql.DB) homeview.Reader { return homeview.Reader{DB: w} }

func iso(ts time.Time) string { return ts.UTC().Format(time.RFC3339) }

// The home screen's attention codes are the ones health_today can raise, so a
// code added there cannot reach the screen unworded.
func TestAttentionCodesMatchHealthToday(t *testing.T) {
	if got, want := home.AttentionCodes(), query.AttentionCodes(); !slices.Equal(got, want) {
		t.Fatalf("home.AttentionCodes() = %v, want health_today's %v", got, want)
	}
}

// Today's realized P&L is paper_daily_pnl's row of today and
// `GET /api/v1/performance`'s daily_pnl: positions closed since 00:00 JST
// count, one closed a second earlier does not, an open one and one with no
// realized P&L never do.
func TestRealizedToday_EqualsPaperDailyPnlOfToday(t *testing.T) {
	_, w := testkit.HealthyDay(t, now)
	closed := []struct {
		id  int64
		at  time.Time
		pnl float64
	}{
		{101, testkit.At(19, 23, 59).Add(59 * time.Second), 5000}, // 23:59:59 JST yesterday
		{102, testkit.At(20, 0, 0), -300},                         // exactly 00:00 JST
		{103, testkit.At(20, 10, 0), 1200},
	}
	var positions []domain.Position
	for _, c := range closed {
		testkit.ClosedPosition(t, w, c.id, 99, nil, 100, 1000, iso(testkit.At(19, 9, 0)), iso(c.at), c.pnl, "take_profit")
		at, pnl := c.at, c.pnl
		positions = append(positions, domain.Position{ClosedAt: &at, RealizedPnL: &pnl})
	}
	testkit.ClosedPosition(t, w, 104, 99, nil, 100, 1000, iso(testkit.At(20, 9, 0)), iso(testkit.At(20, 11, 0)), nil, "stop_loss") // no realized pnl

	got, err := reader(t, w).RealizedToday(t.Context(), now)
	if err != nil {
		t.Fatalf("RealizedToday: %v", err)
	}
	if got.PnLJPY != 900 || got.ClosedCount != 2 {
		t.Fatalf("RealizedToday = %+v, want 900 yen over 2 closes", got)
	}

	days, err := query.DailyPnL(t.Context(), w, query.Range{From: testkit.At(20, 0, 0), To: testkit.At(21, 0, 0)})
	if err != nil || len(days) != 1 || days[0].Date != "2026-10-20" {
		t.Fatalf("paper_daily_pnl rows = %+v, err %v", days, err)
	}
	if days[0].PnlJPY != got.PnLJPY || days[0].TradeCount != got.ClosedCount {
		t.Errorf("paper_daily_pnl today = %+v, home = %+v: must be equal", days[0], got)
	}
	if daily := insight.Aggregate(positions, 0, now).DailyPnL; daily != got.PnLJPY {
		t.Errorf("/api/v1/performance daily_pnl = %v, home = %v: must be equal", daily, got.PnLJPY)
	}
}

func TestRealizedToday_NoCloseIsZero(t *testing.T) {
	_, w := testkit.HealthyDay(t, now)
	got, err := reader(t, w).RealizedToday(t.Context(), now)
	if err != nil || got != (home.Realized{}) {
		t.Fatalf("RealizedToday = %+v, %v; want zero", got, err)
	}
}

func TestHealth_ReducesHealthToday(t *testing.T) {
	_, w := testkit.HealthyDay(t, now)
	h, err := reader(t, w).Health(t.Context(), now)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !h.MetaAdopted || h.MetaVersion != "v1" || h.MetaTrainedAt.IsZero() || h.OpenedToday != 1 || len(h.AttentionCodes) != 0 {
		t.Errorf("healthy day = %+v", h)
	}

	testkit.MustExec(t, w, `DELETE FROM metalabel_models`)
	testkit.Setting(t, w, paperbootstrapsettings.Key, "1")
	testkit.KillEvent(t, w, 1, "operator_manual", false)
	testkit.RetrainSkip(t, w, testkit.At(20, 3, 0), "insufficient_data")
	h, err = reader(t, w).Health(t.Context(), now)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.MetaAdopted || !h.BootstrapEnabled || !h.RetrainSkipped || h.RetrainSkipReason == "" {
		t.Errorf("no model, bootstrap on, retraining skipped = %+v", h)
	}
	for _, code := range []string{query.AttentionNoActiveModel, query.AttentionKillSwitchActive, query.AttentionRetrainSkipped} {
		if !slices.Contains(h.AttentionCodes, code) {
			t.Errorf("attention codes %v lack %s", h.AttentionCodes, code)
		}
	}
}

func TestRejectReasons_CountsTodaysNoneSignalsByHead(t *testing.T) {
	_, w := testkit.HealthyDay(t, now)
	signals := []struct {
		at     time.Time
		dir    string
		reason any
	}{
		{testkit.At(20, 9, 30), "NONE", "entry_quality_below_threshold: entry_quality=\"poor\" < min=\"good\""},
		{testkit.At(20, 9, 31), "NONE", "entry_quality_below_threshold: entry_quality=\"fair\" < min=\"good\""},
		{testkit.At(20, 9, 32), "NONE", "risk_engine_rejected: max_daily_trades"},
		{testkit.At(20, 9, 33), "LONG", nil},
		{testkit.At(19, 23, 59), "NONE", "jev_none"}, // yesterday JST
	}
	for _, s := range signals {
		testkit.MustExec(t, w, `INSERT INTO trade_signals (instrument_id, symbol, direction, timestamp, policy_version, risk_passed, reject_reason)
			VALUES (1, '7203', ?, ?, 'p1', ?, ?)`, s.dir, testkit.StoredAt(t, s.at), s.dir != "NONE", s.reason)
	}
	got, err := reader(t, w).RejectReasons(t.Context(), now)
	if err != nil {
		t.Fatalf("RejectReasons: %v", err)
	}
	want := []home.RejectCount{{Reason: "entry_quality_below_threshold", Count: 2}, {Reason: "risk_engine_rejected", Count: 1}}
	if !slices.Equal(got, want) {
		t.Errorf("RejectReasons = %+v, want %+v", got, want)
	}
}

func TestProposals_TodaysCountsAndNewest(t *testing.T) {
	_, w := testkit.HealthyDay(t, now)
	rows := []testkit.ProposalRow{
		{ID: 1, Status: "received", ReceivedAt: iso(testkit.At(20, 9, 0)), Symbol: "7203"},
		{ID: 2, Status: "rejected", Reason: "bucket_budget_exceeded", ReceivedAt: iso(testkit.At(20, 10, 0)), Symbol: "6758"},
		{ID: 3, Status: "filled", ReceivedAt: iso(testkit.At(20, 11, 0)), Symbol: "9984"},
		{ID: 4, Status: "expired", ReceivedAt: iso(testkit.At(20, 12, 0)), Symbol: "8306"},
		{ID: 5, Status: "filled", ReceivedAt: iso(testkit.At(19, 12, 0)), Symbol: "1111"}, // yesterday
	}
	for _, r := range rows {
		testkit.Proposal(t, w, r)
	}
	got, err := reader(t, w).Proposals(t.Context(), now)
	if err != nil {
		t.Fatalf("Proposals: %v", err)
	}
	if got.Total != 4 {
		t.Errorf("total = %d, want 4 (yesterday's is not counted)", got.Total)
	}
	counts := map[string]int{}
	for _, c := range got.ByStatus {
		counts[c.Status] = c.Count
	}
	if counts["received"] != 1 || counts["rejected"] != 1 || counts["filled"] != 1 || counts["expired"] != 1 {
		t.Errorf("by status = %v", counts)
	}
	if len(got.Latest) != 3 || got.Latest[0].ID != 4 || got.Latest[0].Symbol != "8306" || got.Latest[2].ID != 2 {
		t.Errorf("latest = %+v, want the newest three (4, 3, 2)", got.Latest)
	}
}

func TestRecentTrades_NewestClosedFirstLimited(t *testing.T) {
	_, w := testkit.HealthyDay(t, now)
	for i := range 4 {
		closed := testkit.At(20, 9+i, 0)
		testkit.ClosedPosition(t, w, int64(i+101), 99, nil, 100, 1000, iso(testkit.At(20, 8, 0)), iso(closed), float64(100*(i+1)), "take_profit")
	}
	got, err := reader(t, w).RecentTrades(t.Context(), now, 3)
	if err != nil {
		t.Fatalf("RecentTrades: %v", err)
	}
	if len(got) != 3 || got[0].PnLJPY != 400 || got[2].PnLJPY != 200 || got[0].Symbol != "7203" || got[0].ExitReason != "take_profit" {
		t.Errorf("RecentTrades = %+v, want the 3 newest closes (400, 300, 200)", got)
	}
}
