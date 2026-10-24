package hometests

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/home"
)

// keys are the data-key of every 要対応 item, in order.
func keys(body string) []string {
	var out []string
	for _, part := range strings.Split(body, `data-testid="attention-item" data-key="`)[1:] {
		out = append(out, part[:strings.Index(part, `"`)])
	}
	return out
}

func TestAttention_NothingToDoIsOneLine(t *testing.T) {
	f := newFixture()
	body := f.mustGet(t, "/home/attention")
	if got := inner(t, body, "attention-none"); got != "対応が必要なものはありません" {
		t.Errorf("line = %q", got)
	}
	if count(body, "attention-item") != 0 || count(body, "attention-notice") != 0 {
		t.Errorf("an empty list is only the one line: %s", body)
	}
	if badge := f.mustGet(t, "/home/attention-badge"); strings.TrimSpace(badge) != "" {
		t.Errorf("no items, yet the badge rendered %q", badge)
	}
}

// Every health_today attention code is worded, in health_today's order, each
// with a link to where to look.
func TestAttention_EveryHealthCodeIsWordedWithALink(t *testing.T) {
	f := newFixture()
	f.source.health.AttentionCodes = home.AttentionCodes()
	f.source.health.MissingSessionDates = []string{"2026-10-16"}
	f.source.health.StalledProposals = 2
	body := f.mustGet(t, "/home/attention")
	// The Kill Switch, the gravest, leads the list; the rest keep health_today's order.
	want := append([]string{"kill_switch_active"}, slices.DeleteFunc(home.AttentionCodes(), func(c string) bool { return c == "kill_switch_active" })...)
	if got := keys(body); !slices.Equal(got, want) {
		t.Fatalf("items = %v, want one per code %v", got, want)
	}
	if n := count(body, "home-link"); n != len(want) {
		t.Errorf("links = %d, want one per item (%d)", n, len(want))
	}
	for _, text := range []string{"2026-10-16", "2 件", "夜間の再学習を見送りました"} {
		if !strings.Contains(clean(body), text) {
			t.Errorf("items lack %q", text)
		}
	}
}

// The banners that used to stack under the header on every page are listed
// here: connection error, daily halt, health alert, unset keys, update; the
// Kill Switch is both the state and a code and is listed once.
func TestAttention_FormerBannersAreListedOnceEach(t *testing.T) {
	f := newFixture()
	f.state = domain.SystemStateKilled
	f.halt = lossLimitHalt()
	f.market.Issue, f.market.Guidance = "not_logged_in", "ログインしてください。"
	f.alerts = []domain.HealthAlert{{Code: domain.HealthAlertMarketDataIdle, Since: jst(20, 9, 30)}}
	f.unsetKeys = []string{"SLACK_WEBHOOK_URL", "LUNA_API_KEY"}
	f.update = updater.Status{Available: true, Version: "v9.9.9", Blocked: true}
	f.source.health.AttentionCodes = []string{"daily_guard_halted", "kill_switch_active", "market_data_idle"}
	body := f.mustGet(t, "/home/attention")
	want := []string{"kill_switch_active", "daily_guard_halted", "marketdata_issue", "market_data_idle", "secrets_unset", "update_available"}
	if got := keys(body); !slices.Equal(got, want) {
		t.Fatalf("items = %v, want %v (each condition once)", got, want)
	}
	text := clean(body)
	for _, s := range []string{
		"Kill Switch が発動しています",
		"本日の新規エントリーを停止しました（1日の損失上限）",
		"kabuステーションの市況データを取得できません", "ログインしてください。",
		"立会中ですが、市況データの取得が止まっています。", "09:30から続いています",
		"任意の設定項目が 2 件未設定です", "未設定の設定キー: SLACK_WEBHOOK_URL, LUNA_API_KEY",
		"新バージョン v9.9.9 が利用可能です", "安全条件が揃い次第",
	} {
		if !strings.Contains(text, s) {
			t.Errorf("items lack %q", s)
		}
	}
	for _, href := range []string{"/activity", "/settings/risk#modal-daily-guard", "/settings/broker", "/settings", "/settings/maintenance#update-panel"} {
		if !strings.Contains(body, `href="`+href+`"`) {
			t.Errorf("no link to %s", href)
		}
	}
	// The header badge counts exactly what Home lists.
	if got := inner(t, f.mustGet(t, "/home/attention-badge"), "attention-badge"); got != "要対応 6 件" {
		t.Errorf("badge = %q, want 要対応 6 件", got)
	}
}

func TestAttention_PersistentLoginStreakIsWordedAsOngoing(t *testing.T) {
	f := newFixture()
	f.market.Issue, f.market.Guidance, f.market.Persistent = "not_logged_in", "g", true
	if got := inner(t, f.mustGet(t, "/home/attention"), "attention-item"); !strings.Contains(got, "kabuステーションに未ログインの状態が続いています") {
		t.Errorf("item = %q", got)
	}
}

func TestAttention_BrokerNoticeIsAWarningNotAFailure(t *testing.T) {
	f := newFixture()
	f.market.Notice = "証券会社のAPIの更新が告知されています。"
	body := f.mustGet(t, "/home/attention")
	if !strings.Contains(body, `data-key="marketdata_notice" data-level="warn"`) {
		t.Errorf("notice item missing or not a warning: %s", body)
	}
}

// A source that cannot be read is said so: an empty list is then no proof.
func TestAttention_UnreadSourceIsNamed(t *testing.T) {
	f := newFixture()
	f.alertsErr = errBoom
	f.source.healthErr = errBoom
	body := f.mustGet(t, "/home/attention")
	if count(body, "attention-none") != 0 {
		t.Errorf("must not claim nothing needs attention: %s", body)
	}
	got := inner(t, body, "attention-notice")
	for _, want := range []string{"市況データの警告", "ヘルスチェック"} {
		if !strings.Contains(got, want) {
			t.Errorf("notice %q lacks %q", got, want)
		}
	}
}

// The Kill Switch code alone (state unreadable) is still listed.
func TestAttention_CodeStandsInWhenItsBannerSourceFailed(t *testing.T) {
	f := newFixture()
	f.haltErr = errBoom
	f.source.health.AttentionCodes = []string{"daily_guard_halted"}
	if got := keys(f.mustGet(t, "/home/attention")); !slices.Equal(got, []string{"daily_guard_halted"}) {
		t.Errorf("items = %v", got)
	}
}

// 要対応 replaces the header banners, which were polled live regions (issue
// #626): a new alert must be announced without a role of its own in the
// fragment, an unchanged answer must be skipped, the poll is a background
// request, and what Settings' Save/Delete or the manual update check change
// is shown at once, not after the next poll.
func TestAttention_SectionIsAPolledLiveRegionRefreshedByTheEventsThatChangeIt(t *testing.T) {
	f := newFixture()
	tag := regexp.MustCompile(`(?s)<div\b[^>]*\bid="home-attention"[^>]*>`).FindString(f.mustGet(t, "/"))
	if tag == "" {
		t.Fatal("no #home-attention container on Home")
	}
	for _, want := range []string{
		`role="status"`, `aria-live="polite"`, "data-live-banner",
		`hx-get="/home/attention"`, "X-Pitha-Background",
		"every 30s", "secretsStatusChanged from:body", "updateStatusChanged from:body",
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("#home-attention lacks %q: %s", want, tag)
		}
	}

	// The container is the live region, so neither the items nor the one-line
	// "nothing to do" state carry a role of their own.
	f.unsetKeys = []string{"SLACK_WEBHOOK_URL"}
	f.update = updater.Status{Available: true, Version: "v9.9.9"}
	for name, body := range map[string]string{"items": f.mustGet(t, "/home/attention"), "none": newFixture().mustGet(t, "/home/attention")} {
		for _, role := range []string{`role="status"`, `role="alert"`} {
			if strings.Contains(body, role) {
				t.Errorf("the %s fragment must not carry its own %s: %s", name, role, body)
			}
		}
	}
}
