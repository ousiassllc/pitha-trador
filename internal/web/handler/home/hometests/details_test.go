package hometests

import (
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/home"
)

func TestRejectReasons_ListedByCountWithLabels(t *testing.T) {
	f := newFixture()
	f.source.rejects = []home.RejectCount{
		{Reason: "jev_none", Count: 12}, {Reason: "probability_below_threshold", Count: 7}, {Reason: "novel_reason", Count: 1},
	}
	body := f.mustGet(t, "/home/details")
	rows := strings.Split(body, `data-testid="reject-row"`)[1:]
	if len(rows) != 3 {
		t.Fatalf("reject rows = %d, want 3", len(rows))
	}
	for i, want := range []string{"Jev が方向なしと判断 jev_none 12", "確率がしきい値未満 probability_below_threshold 7", "novel_reason novel_reason 1"} {
		if got := clean(rows[i][:strings.Index(rows[i], "</li>")]); !strings.Contains(got, want) {
			t.Errorf("row %d = %q, want it to contain %q", i, got, want)
		}
	}
	if count(body, "entry-quality-note") != 0 {
		t.Errorf("entry quality is not the top reason, yet its note is shown: %s", body)
	}
	if got := inner(t, body, "home-reject-reasons"); !strings.Contains(got, "20 件") {
		t.Errorf("total missing from %q", got)
	}
}

// entry_quality_below_threshold on top: the minimum entry quality in use (Paper
// and Live) and a link to the settings page that changes it.
func TestRejectReasons_EntryQualityOnTopShowsTheMinimumAndSettingsLink(t *testing.T) {
	f := newFixture()
	f.source.rejects = []home.RejectCount{{Reason: "entry_quality_below_threshold", Count: 30}, {Reason: "jev_none", Count: 5}}
	body := f.mustGet(t, "/home/details")
	note := between(body, `data-testid="entry-quality-note"`, "</section>")
	text := clean(note)
	for _, want := range []string{
		"ロングの最低エントリー品質: Paper fair ／ Live good",
		"ショートの最低エントリー品質: Paper good ／ Live strong",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("note lacks %q: %s", want, text)
		}
	}
	if !strings.Contains(note, `href="/settings/operations#modal-policy-long"`) {
		t.Errorf("note lacks the settings link: %s", note)
	}
	if got := inner(t, note, "entry-quality-paper"); got != "Paper fair" {
		t.Errorf("paper = %q", got)
	}
}

func TestRejectReasons_EntryQualityNotTopHasNoNote(t *testing.T) {
	f := newFixture()
	f.source.rejects = []home.RejectCount{{Reason: "jev_none", Count: 30}, {Reason: "entry_quality_below_threshold", Count: 29}}
	if body := f.mustGet(t, "/home/details"); count(body, "entry-quality-note") != 0 {
		t.Errorf("note shown although entry quality is second: %s", body)
	}
}

// A tie for the most frequent reason counts as "the most frequent": the note is
// shown whichever of the tied reasons sorts first.
func TestRejectReasons_EntryQualityTiedForTopShowsTheNote(t *testing.T) {
	f := newFixture()
	f.source.rejects = []home.RejectCount{
		{Reason: "jev_none", Count: 30}, {Reason: "entry_quality_below_threshold", Count: 30}, {Reason: "spread_too_wide", Count: 2},
	}
	if body := f.mustGet(t, "/home/details"); count(body, "entry-quality-note") != 1 {
		t.Errorf("no note although entry quality ties for the top: %s", body)
	}
}

func TestRejectReasons_EntryQualityOnTopButSettingUnreadable(t *testing.T) {
	f := newFixture()
	f.source.rejects = []home.RejectCount{{Reason: "entry_quality_below_threshold", Count: 3}}
	f.settings.getErr = errBoom
	body := f.mustGet(t, "/home/details")
	if got := inner(t, body, "entry-quality-error"); got != "最低エントリー品質を読み取れませんでした。" {
		t.Errorf("error = %q", got)
	}
	if !strings.Contains(body, `href="/settings/operations#modal-policy-long"`) {
		t.Errorf("the settings link stays even when the value cannot be read: %s", body)
	}
}

func TestRejectReasons_NoSignalsYet(t *testing.T) {
	f := newFixture()
	if got := inner(t, f.mustGet(t, "/home/details"), "reject-empty"); got != "今日はまだ見送りの記録がありません。" {
		t.Errorf("empty = %q", got)
	}
}

func TestScan_FunnelAndEmptyState(t *testing.T) {
	f := newFixture()
	if body := f.mustGet(t, "/home/details"); count(body, "scan-none") != 1 {
		t.Errorf("no cycle yet must say so: %s", body)
	}
	f.cycle = &domain.ScanCycle{
		StartedAt: jst(20, 9, 59), FinishedAt: jst(20, 10, 0).Add(1500 * time.Millisecond),
		Funnel: domain.ScanFunnel{Universe: 45, FeatureComputed: 44, FastScreenerPassed: 8, ScoutEvaluated: 6, ScoutPassed: 2},
	}
	body := f.mustGet(t, "/home/details")
	for id, want := range map[string]string{
		"scan-funnel-universe": "45", "scan-funnel-features": "44", "scan-funnel-fast": "8", "scan-funnel-scout": "2",
	} {
		if got := inner(t, body, id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	if got := inner(t, body, "scan-cycle-meta"); !strings.Contains(got, "10-20 10:00") {
		t.Errorf("cycle meta = %q", got)
	}
}

func TestProposals_TodaysCountsAndNewest(t *testing.T) {
	f := newFixture()
	if got := inner(t, f.mustGet(t, "/home/details"), "proposals-none"); got != "今日受け付けた提案はありません。" {
		t.Errorf("empty = %q", got)
	}
	f.source.proposals = home.Proposals{
		Total:    4,
		ByStatus: []home.StatusCount{{Status: "received", Count: 1}, {Status: "filled", Count: 2}, {Status: "rejected", Count: 1}, {Status: "expired", Count: 0}},
		Latest:   []home.ProposalItem{{ID: 7, Symbol: "7203", Action: "open_long", Status: "filled", ReceivedAt: jst(20, 9, 5)}},
	}
	body := f.mustGet(t, "/home/details")
	if got := inner(t, body, "proposals-total"); got != "4" {
		t.Errorf("total = %q", got)
	}
	if n := count(body, "proposals-status"); n != 3 {
		t.Errorf("status entries = %d, want 3 (the zero one is left out)", n)
	}
	if row := inner(t, body, "proposal-row"); !strings.Contains(row, "7203") || !strings.Contains(row, "買い（新規）") || !strings.Contains(row, "10-20 09:05") {
		t.Errorf("latest row = %q", row)
	}
	if !strings.Contains(body, `href="/order-proposals/7"`) {
		t.Errorf("latest proposal does not link to its detail: %s", body)
	}
}

func TestTrades_NewestClosedWithPnLColour(t *testing.T) {
	f := newFixture()
	if got := inner(t, f.mustGet(t, "/home/details"), "trades-none"); got != "決済した取引はまだありません。" {
		t.Errorf("empty = %q", got)
	}
	f.source.trades = []home.Trade{
		{Symbol: "7203", Side: "LONG", Quantity: 100, ClosedAt: jst(20, 9, 50), PnLJPY: 1200, ExitReason: "take_profit"},
		{Symbol: "6758", Side: "LONG", Quantity: 200, ClosedAt: jst(20, 9, 40), PnLJPY: -800, ExitReason: "stop_loss"},
	}
	body := f.mustGet(t, "/home/details")
	if n := count(body, "trade-row"); n != 2 {
		t.Fatalf("trade rows = %d", n)
	}
	for _, want := range []string{"+1,200円", "-800円", "text-emerald-700", "text-red-700", `href="/symbols/6758"`} {
		if !strings.Contains(body, want) {
			t.Errorf("trades lack %q", want)
		}
	}
}

func TestModel_AdoptedNotAdoptedAndSkippedRetraining(t *testing.T) {
	f := newFixture()
	body := f.mustGet(t, "/home/details")
	if got := inner(t, body, "model-adopted"); got != "メタモデル: 採用中 v1" {
		t.Errorf("adopted = %q", got)
	}
	if got := inner(t, body, "model-trained-at"); got != "学習日時 10-19 09:00" {
		t.Errorf("trained at = %q", got)
	}
	f.source.health.MetaAdopted = false
	f.source.health.RetrainSkipped, f.source.health.RetrainSkipReason = true, "insufficient_data"
	body = f.mustGet(t, "/home/details")
	if got := inner(t, body, "model-not-adopted"); got != "メタモデル: 未採用" {
		t.Errorf("not adopted = %q", got)
	}
	if got := inner(t, body, "model-retrain"); got != "夜間の再学習を見送りました（insufficient_data）" {
		t.Errorf("retrain = %q", got)
	}
}

func TestKillEvents_LatestThree(t *testing.T) {
	f := newFixture()
	if got := inner(t, f.mustGet(t, "/home/details"), "kill-none"); got != "最近の Kill Switch はありません。" {
		t.Errorf("empty = %q", got)
	}
	for i := range 5 {
		f.events = append(f.events, domain.ActivityEvent{
			Type: domain.ActivityTypeKillSwitch, Timestamp: jst(20, 9, i),
			Detail: "reason=daily_loss_limit resolved_by=operator",
		})
	}
	f.events = append(f.events, domain.ActivityEvent{Type: domain.ActivityTypeJob, Timestamp: jst(20, 9, 59), Detail: "x"})
	body := f.mustGet(t, "/home/details")
	if n := count(body, "kill-row"); n != 3 {
		t.Fatalf("kill rows = %d, want 3", n)
	}
	if got := inner(t, body, "kill-row"); got != "10-20 09:00 理由: daily_loss_limit（解除: operator）" {
		t.Errorf("row = %q", got)
	}
}

func TestDetails_IsACollapsibleSectionOnThePage(t *testing.T) {
	f := newFixture()
	body := f.mustGet(t, "/")
	if !strings.Contains(body, `<details class="mt-6" open data-testid="home-details-section">`) || !strings.Contains(body, "<summary") {
		t.Errorf("今日の詳細 is not a collapsible <details>: %s", between(body, "home-details-section", "</summary>"))
	}
}
