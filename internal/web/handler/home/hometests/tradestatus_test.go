package hometests

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

const (
	sentenceAllowed = "新規エントリー: 可能"
	stopped         = "新規エントリー: 停止中 — "
)

func lossLimitHalt() *domain.DailyEntryHalt {
	return &domain.DailyEntryHalt{
		Day: "2026-10-20", Reason: domain.DailyHaltReasonLossLimit,
		Message: "当日損益 -50000円 が損失上限 -50000円 に達しました。", HaltedAt: jst(20, 9, 30),
	}
}

// A quiet trading morning reads 「新規エントリー: 可能」, with the existing Kill
// Switch panel under it.
func TestTradeStatus_AllowedWhenNothingStopsEntries(t *testing.T) {
	f := newFixture()
	body := f.mustGet(t, "/home/trade-status")
	if got := inner(t, body, "trade-status-sentence"); got != sentenceAllowed {
		t.Errorf("sentence = %q, want %q", got, sentenceAllowed)
	}
	if !strings.Contains(body, `data-allowed="true"`) {
		t.Errorf("allowed card lacks data-allowed=true: %s", body)
	}
	if count(body, "trade-status-detail") != 0 || count(body, "trade-status-notice") != 0 {
		t.Errorf("an allowed card shows no explanation: %s", body)
	}
}

// Each state of the card is reproduced alone, with its exact sentence.
func TestTradeStatus_EachStateShowsItsSentence(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*fixture)
		want  string
		link  string // href of the 「どこを見るか」 link, "" for none
	}{
		{"Kill Switch", func(f *fixture) { f.state = domain.SystemStateKilled },
			stopped + "Kill Switch が発動しています", "/activity"},
		{"一時停止", func(f *fixture) { f.state = domain.SystemStatePaused },
			stopped + "一時停止中です", ""},
		{"日次ガード 損失上限", func(f *fixture) { f.halt = lossLimitHalt() },
			stopped + "日次ガード（1日の損失上限）", "/settings/risk#modal-daily-guard"},
		{"日次ガード 利益確保", func(f *fixture) {
			f.halt = &domain.DailyEntryHalt{Reason: domain.DailyHaltReasonProfitLock, Message: "m"}
		}, stopped + "日次ガード（利益確保（profit lock））", "/settings/risk#modal-daily-guard"},
		{"日次ガード 最大取引回数", func(f *fixture) {
			f.halt = &domain.DailyEntryHalt{Reason: domain.DailyHaltReasonMaxTrades, Message: "m"}
		}, stopped + "日次ガード（1日の最大取引回数）", "/settings/risk#modal-daily-guard"},
		{"kabu 401", func(f *fixture) {
			f.market.Issue, f.market.Guidance = "rejected", "認証エラーが続いています。"
		},
			stopped + "kabuステーションが認証エラー（401）を返しています", "/settings/broker"},
		{"kabu 未ログイン（トークン発行失敗）", func(f *fixture) {
			f.market.Issue, f.market.Guidance = "not_logged_in", "ログインしてください。"
		},
			stopped + "kabuステーションに未ログインです", "/settings/broker"},
		{"kabu 未ログイン（朝の警告）", func(f *fixture) {
			f.alerts = []domain.HealthAlert{{Code: domain.HealthAlertKabuNotLoggedIn, Since: jst(20, 8, 45)}}
		}, stopped + "kabuステーションに未ログインです", "/settings/broker"},
		{"kabu 未起動（トークン発行失敗）", func(f *fixture) { f.market.Issue, f.market.Guidance = "unreachable", "起動してください。" },
			stopped + "kabuステーションに接続できません（未起動の疑い）", "/settings/broker"},
		{"kabu 未起動（朝の警告）", func(f *fixture) {
			f.alerts = []domain.HealthAlert{{Code: domain.HealthAlertKabuUnreachable, Since: jst(20, 8, 45)}}
		}, stopped + "kabuステーションに接続できません（未起動の疑い）", "/settings/broker"},
		{"立会時間外（次回開始時刻付き）", func(f *fixture) { f.now = jst(20, 18, 0) },
			stopped + "立会時間外です（次回の立会開始: 2026-10-21(水) 09:00 JST）", ""},
		{"立会時間外（金曜の夜は月曜の朝）", func(f *fixture) { f.now = jst(23, 18, 0) },
			stopped + "立会時間外です（次回の立会開始: 2026-10-26(月) 09:00 JST）", ""},
		{"市況取得停止", func(f *fixture) {
			f.alerts = []domain.HealthAlert{{Code: domain.HealthAlertMarketDataIdle, Since: jst(20, 9, 30)}}
		}, stopped + "市況データの取得が止まっています", "/activity"},
		{"メタモデル未採用かつブートストラップ無効", func(f *fixture) { f.source.health.MetaAdopted = false },
			stopped + "売買判断モデルが未採用で、ブートストラップエントリーも無効です", "/settings/operations#modal-paper-bootstrap"},
		{"メタモデル未採用でもブートストラップ有効なら可能", func(f *fixture) {
			f.source.health.MetaAdopted, f.source.health.BootstrapEnabled = false, true
		}, sentenceAllowed, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			tt.setup(f)
			body := f.mustGet(t, "/home/trade-status")
			if got := inner(t, body, "trade-status-sentence"); got != tt.want {
				t.Errorf("sentence = %q, want %q", got, tt.want)
			}
			hasLink := strings.Contains(body, `data-testid="home-link"`)
			if tt.link == "" && hasLink {
				t.Errorf("unexpected link: %s", body)
			}
			if tt.link != "" && !strings.Contains(body, `href="`+tt.link+`"`) {
				t.Errorf("link %s missing: %s", tt.link, body)
			}
		})
	}
}

// With every reason present at once the highest one wins; removing the winner
// one at a time walks down the priority order of issue #896 §5.1:
// Kill Switch → 一時停止 → 日次ガード → kabu 401・未ログイン・未起動 → 立会時間外 →
// 市況取得停止 → メタモデル未採用かつブートストラップ無効.
func TestTradeStatus_PriorityOrder(t *testing.T) {
	steps := []struct {
		name  string
		apply func(*fixture)
		want  string
	}{
		{"Kill Switch", func(f *fixture) { f.state = domain.SystemStateKilled }, stopped + "Kill Switch が発動しています"},
		{"一時停止", func(f *fixture) { f.state = domain.SystemStatePaused }, stopped + "一時停止中です"},
		{"日次ガード", func(f *fixture) { f.halt = lossLimitHalt() }, stopped + "日次ガード（1日の損失上限）"},
		{"kabu 401", func(f *fixture) { f.market.Issue, f.market.Guidance = "rejected", "g" }, stopped + "kabuステーションが認証エラー（401）を返しています"},
		{"立会時間外", func(f *fixture) { f.now = jst(20, 18, 0) }, stopped + "立会時間外です（次回の立会開始: 2026-10-21(水) 09:00 JST）"},
		{"市況取得停止", func(f *fixture) {
			f.alerts = []domain.HealthAlert{{Code: domain.HealthAlertMarketDataIdle, Since: jst(20, 9, 30)}}
		}, stopped + "市況データの取得が止まっています"},
		{"メタモデル未採用", func(f *fixture) { f.source.health.MetaAdopted = false }, stopped + "売買判断モデルが未採用で、ブートストラップエントリーも無効です"},
	}
	for i, step := range steps {
		t.Run("winner is "+step.name, func(t *testing.T) {
			f := newFixture()
			// The reasons are set lowest first, so the one under test is the
			// last (and, for the system state, the final) write.
			for j := len(steps) - 1; j >= i; j-- {
				steps[j].apply(f)
			}
			if got := f.sentence(t); got != step.want {
				t.Errorf("sentence = %q, want %q", got, step.want)
			}
		})
	}
}

// A kabu failure and a morning alert name the same fact; the token failure
// (the banner's source) is worded first, and an alert alone still stops.
func TestTradeStatus_BrokerFailureBeforeAlertWording(t *testing.T) {
	f := newFixture()
	f.market.Issue, f.market.Guidance = "rejected", "g"
	f.alerts = []domain.HealthAlert{{Code: domain.HealthAlertKabuNotLoggedIn, Since: jst(20, 8, 45)}}
	if got, want := f.sentence(t), stopped+"kabuステーションが認証エラー（401）を返しています"; got != want {
		t.Errorf("sentence = %q, want %q", got, want)
	}
}

// 立花 selected: the sentence names the broker the failing session belongs to.
func TestTradeStatus_NamesTachibanaWhenItIsTheBroker(t *testing.T) {
	f := newFixture()
	f.market.Issue, f.market.Guidance, f.market.Environment = "not_logged_in", "g", "demo"
	if got, want := f.sentence(t), stopped+"立花証券 e支店に未ログインです"; got != want {
		t.Errorf("sentence = %q, want %q", got, want)
	}
}

// The daily guard card explains itself: the figures that reached the rule.
func TestTradeStatus_DailyGuardShowsItsFigures(t *testing.T) {
	f := newFixture()
	f.halt = lossLimitHalt()
	body := f.mustGet(t, "/home/trade-status")
	if detail := inner(t, body, "trade-status-detail"); !strings.Contains(detail, "当日損益 -50000円 が損失上限 -50000円 に達しました。") {
		t.Errorf("detail %q lacks the figures that reached the rule", detail)
	}
	// What the guard does next is the card's help text: in the DOM, tied to its 「?」.
	if got := clean(body); !strings.Contains(got, "決済は通常どおり") || !strings.Contains(got, "翌営業日に自動で解除") {
		t.Errorf("the card lacks the daily guard's help text: %s", got)
	}
	if !strings.Contains(body, `aria-describedby="help-home-trade-status"`) {
		t.Errorf("the help mark is not tied to its text: %s", body)
	}
}

// An input that cannot be read is never guessed: with no reason found the card
// does not say 「可能」, and with one found it says part of the state is unknown.
func TestTradeStatus_UnreadInputsAreNotGuessed(t *testing.T) {
	t.Run("nothing stops but the daily guard is unreadable", func(t *testing.T) {
		f := newFixture()
		f.haltErr = errBoom
		body := f.mustGet(t, "/home/trade-status")
		if got := inner(t, body, "trade-status-sentence"); got != "新規エントリー: 状態を確認できません" {
			t.Errorf("sentence = %q, want the unknown one", got)
		}
		if got := inner(t, body, "trade-status-notice"); !strings.Contains(got, "日次ガード") {
			t.Errorf("notice = %q, want it to name 日次ガード", got)
		}
	})
	t.Run("a reason is found while the health check is unreadable", func(t *testing.T) {
		f := newFixture()
		f.state = domain.SystemStatePaused
		f.source.healthErr = errBoom
		body := f.mustGet(t, "/home/trade-status")
		if got := inner(t, body, "trade-status-sentence"); got != stopped+"一時停止中です" {
			t.Errorf("sentence = %q", got)
		}
		if got := inner(t, body, "trade-status-notice"); !strings.Contains(got, "モデルの状態") {
			t.Errorf("notice = %q, want it to name モデルの状態", got)
		}
	})
	t.Run("the system state itself is unreadable", func(t *testing.T) {
		f := newFixture()
		f.stateErr = errBoom
		body := f.mustGet(t, "/home/trade-status")
		if got := inner(t, body, "home-card-error"); got != "システムの状態を読み取れませんでした。" {
			t.Errorf("error = %q", got)
		}
		if count(body, "trade-status-sentence") != 0 {
			t.Errorf("no verdict may be shown without the state: %s", body)
		}
	})
}

// The clock decides 立会時間外: the lunch break is outside the session too.
func TestTradeStatus_LunchBreakIsOffSession(t *testing.T) {
	f := newFixture()
	f.now = jst(20, 12, 0)
	got := f.sentence(t)
	if !strings.HasPrefix(got, stopped+"立会時間外です（次回の立会開始: ") || !strings.Contains(got, "12:30 JST") {
		t.Errorf("sentence = %q, want 立会時間外 with the afternoon open", got)
	}
}
