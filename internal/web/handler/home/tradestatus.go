package home

import (
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/homepanels"
)

const (
	sentenceAllowed = "新規エントリー: 可能"
	sentenceStopped = "新規エントリー: 停止中 — "
	sentenceUnknown = "新規エントリー: 状態を確認できません"
)

// Names of the reads a failed input leaves unknown, for the card's notice.
const (
	readDailyHalt = "日次ガード"
	readAlerts    = "市況データの警告"
	readHealth    = "モデルの状態"
)

// tradeStatus is the 取引の状態 card: whether new entries are allowed and, if
// not, the first reason in the priority order of issue #896 §5.1:
//
//  1. Kill Switch  2. 一時停止  3. 日次ガード  4. kabu 401・未ログイン・未起動
//  5. 立会時間外  6. 市況取得停止  7. メタモデル未採用かつブートストラップ無効
//
// Every reason found is a fact of its own source (the system state, the
// daily halt, the broker session, the clock, the health alerts, the model),
// so a lower one is reported only when no higher one applies. An input that
// could not be read is not guessed: with no reason found the card then says
// the state cannot be confirmed instead of 「可能」, and with one found it adds
// a notice that part of the state is unknown.
func tradeStatus(in statusInputs) homepanels.TradeStatusView {
	v := homepanels.TradeStatusView{State: in.state}
	if in.stateErr != nil {
		v.Err = "システムの状態を読み取れませんでした。"
		return v
	}
	r, unread := firstReason(in)
	if len(unread) > 0 {
		v.Notice = "読み取れなかった状態があります（" + strings.Join(unread, "・") + "）。"
	}
	switch {
	case r != nil:
		v.Sentence, v.Detail, v.Link = sentenceStopped+r.sentence, r.detail, r.link
	case len(unread) > 0:
		v.Sentence = sentenceUnknown
	default:
		v.Allowed, v.Sentence = true, sentenceAllowed
	}
	return v
}

// reason is one stop condition: the sentence after 「停止中 — 」, an optional
// explanation and where to look.
type reason struct {
	sentence string
	detail   string
	link     homepanels.Link
}

// firstReason walks the priority order and returns the first reason that
// applies, plus the names of the inputs that could not be read.
func firstReason(in statusInputs) (*reason, []string) {
	var unread []string
	if in.haltErr != nil {
		unread = append(unread, readDailyHalt)
	}
	if in.alertsErr != nil {
		unread = append(unread, readAlerts)
	}
	if in.healthErr != nil {
		unread = append(unread, readHealth)
	}
	for _, step := range []func(statusInputs) *reason{
		killSwitchReason, pausedReason, dailyGuardReason, brokerReason, offSessionReason, marketIdleReason, noModelReason,
	} {
		if r := step(in); r != nil {
			return r, unread
		}
	}
	return nil, unread
}

func killSwitchReason(in statusInputs) *reason {
	if in.state != domain.SystemStateKilled {
		return nil
	}
	return &reason{
		sentence: "Kill Switch が発動しています",
		detail:   "解除するまで新規エントリーは行われません。",
		link:     linkActivity,
	}
}

func pausedReason(in statusInputs) *reason {
	if in.state != domain.SystemStatePaused {
		return nil
	}
	return &reason{
		sentence: "一時停止中です",
		detail:   "下の「再開」で新規エントリーを再開できます。",
	}
}

func dailyGuardReason(in statusInputs) *reason {
	if in.halt == nil {
		return nil
	}
	return &reason{
		sentence: "日次ガード（" + in.halt.Label() + "）",
		detail:   in.halt.Message + "保有中のポジションの決済は通常どおり行われ、翌営業日に自動で解除されます。",
		link:     linkDailyGuard,
	}
}

// brokerName is the broker the failing session belongs to: the banner names
// the 立花 environment only for 立花.
func brokerName(m organisms.MarketDataBannerProps) string {
	if m.Environment != "" {
		return "立花証券 e支店"
	}
	return "kabuステーション"
}

// brokerReason covers a failing broker session: the last token issuance failed
// (未ログイン / 未起動 / 401 認証エラー / API 設定) or the health detector has
// been reporting a login or connection outage since the morning.
func brokerReason(in statusInputs) *reason {
	broker := brokerName(in.market)
	if in.market.Issue != "" {
		return &reason{sentence: brokerIssueSentence(broker, in.market.Issue), detail: in.market.Guidance, link: linkBroker}
	}
	for _, a := range in.alerts {
		switch a.Code {
		case domain.HealthAlertKabuNotLoggedIn:
			return &reason{sentence: broker + "に未ログインです", detail: "ログインしてください。ログイン後に自動で再発行します。", link: linkBroker}
		case domain.HealthAlertKabuUnreachable:
			return &reason{sentence: broker + "に接続できません（未起動の疑い）", detail: "起動してログインしてください。起動後に自動で再接続します。", link: linkBroker}
		}
	}
	return nil
}

// brokerIssueSentence words a marketdata.TokenIssue (the banner's data-issue).
func brokerIssueSentence(broker, issue string) string {
	switch issue {
	case "unreachable":
		return broker + "に接続できません（未起動の疑い）"
	case "not_logged_in":
		return broker + "に未ログインです"
	case "rejected":
		return broker + "が認証エラー（401）を返しています"
	case "api_disabled":
		return broker + "のAPI利用設定が完了していません"
	case "bad_password":
		return broker + "のAPIパスワードが正しくありません"
	}
	return broker + "に接続できません"
}

func offSessionReason(in statusInputs) *reason {
	if marketcalendar.TSE.IsOpen(in.now) {
		return nil
	}
	s := "立会時間外です"
	if next := marketcalendar.TSE.NextOpen(in.now); !next.IsZero() {
		s += "（次回の立会開始: " + organisms.FormatNextOpen(next) + "）"
	}
	return &reason{sentence: s, detail: "立会時間外は市況データの取得・スキャン・Jev Scoutが止まります。"}
}

func marketIdleReason(in statusInputs) *reason {
	for _, a := range in.alerts {
		if a.Code == domain.HealthAlertMarketDataIdle {
			return &reason{
				sentence: "市況データの取得が止まっています",
				detail:   "アプリが動いているか、ジョブが詰まっていないか（稼働ログ）を確認してください。",
				link:     linkActivity,
			}
		}
	}
	return nil
}

func noModelReason(in statusInputs) *reason {
	if in.healthErr != nil || in.health.MetaAdopted || in.health.BootstrapEnabled {
		return nil
	}
	return &reason{
		sentence: "売買判断モデルが未採用で、ブートストラップエントリーも無効です",
		detail:   "モデルが採用されるか、Paper のブートストラップエントリーを有効にするまで、新規エントリーは行われません。",
		link:     linkBootstrap,
	}
}
