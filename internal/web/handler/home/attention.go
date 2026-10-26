package home

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/web/labels"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/homepanels"
)

// Item keys that are not health_today attention codes (the banners' names).
const (
	keyMarketData       = "marketdata_issue"
	keyMarketDataNotice = "marketdata_notice"
	keySecrets          = "secrets_unset"
	keyUpdate           = "update_available"
)

// Attention codes of health_today (query.Attention*; internal/bootstrap/homeview
// tests that this list and that package's stay the same).
const (
	codeNoPaperTrades   = "no_paper_trades_today"
	codeNoActiveModel   = "no_active_model"
	codeMissingSession  = "missing_session"
	codeDailyGuard      = "daily_guard_halted"
	codeKillSwitch      = "kill_switch_active"
	codeRetrainSkipped  = "retrain_skipped"
	codeProposalStalled = "order_proposal_stalled"
)

// AttentionCodes are the health_today attention codes the Home screen knows how
// to word, so a new code added there without wording here is caught by a test
// instead of showing up as an unexplained count.
func AttentionCodes() []string {
	return []string{
		codeNoPaperTrades, codeNoActiveModel,
		domain.HealthAlertKabuNotLoggedIn, domain.HealthAlertKabuUnreachable, domain.HealthAlertMarketDataIdle,
		codeMissingSession, codeDailyGuard, codeKillSwitch, codeRetrainSkipped, codeProposalStalled,
	}
}

// attention is the 要対応 list: health_today's attention codes and the former
// header banners (connection, daily halt, health alerts, unset secrets, update),
// one item per condition. A condition both a banner and a code report (the
// daily halt, the kabu alerts, the Kill Switch) is listed once, in the
// banner's fuller words. The header badge counts exactly these items.
func attention(in statusInputs) homepanels.AttentionView {
	var items []homepanels.AttentionItem
	seen := map[string]bool{}
	add := func(it homepanels.AttentionItem) {
		if !seen[it.Key] {
			seen[it.Key] = true
			items = append(items, it)
		}
	}
	if in.state == domain.SystemStateKilled || slices.Contains(in.health.AttentionCodes, codeKillSwitch) {
		add(killSwitchItem())
	}
	if in.halt != nil {
		add(dailyGuardItem(in.halt))
	}
	for _, it := range marketDataItems(in.market) {
		add(it)
	}
	for _, a := range in.alerts {
		add(alertItem(a))
	}
	for _, code := range in.health.AttentionCodes {
		if it, ok := codeItem(code, in.health); ok {
			add(it)
		}
	}
	if len(in.unsetKeys) > 0 {
		names := make([]string, len(in.unsetKeys))
		for i, key := range in.unsetKeys {
			names[i] = labels.SecretName(key)
		}
		add(homepanels.AttentionItem{
			Key: keySecrets, Level: homepanels.LevelInfo,
			Title:  fmt.Sprintf("任意の設定項目が %d 件未設定です", len(in.unsetKeys)),
			Detail: "必要に応じて設定画面から入力してください。",
			Help:   "未設定の項目: " + strings.Join(names, "、"),
			Link:   linkSettings,
		})
	}
	if in.update.Available {
		add(updateItem(in))
	}
	return homepanels.AttentionView{Items: items, Notice: attentionNotice(in)}
}

// attentionNotice says which sources could not be read: an empty list is then
// no proof that nothing is wrong.
func attentionNotice(in statusInputs) string {
	var unread []string
	if in.stateErr != nil {
		unread = append(unread, "システムの状態")
	}
	if in.haltErr != nil {
		unread = append(unread, readDailyHalt)
	}
	if in.alertsErr != nil {
		unread = append(unread, readAlerts)
	}
	if in.healthErr != nil {
		unread = append(unread, "ヘルスチェック")
	}
	if len(unread) == 0 {
		return ""
	}
	return "読み取れなかった項目があります（" + strings.Join(unread, "・") + "）。"
}

func killSwitchItem() homepanels.AttentionItem {
	return homepanels.AttentionItem{
		Key: codeKillSwitch, Level: homepanels.LevelAlert,
		Title:  "Kill Switch が発動しています",
		Detail: "解除するまで新規エントリーは行われません。",
		Link:   linkActivity,
	}
}

func dailyGuardItem(halt *domain.DailyEntryHalt) homepanels.AttentionItem {
	return homepanels.AttentionItem{
		Key: codeDailyGuard, Level: homepanels.LevelWarn,
		Title:  "本日の新規エントリーを停止しました（" + halt.Label() + "）",
		Detail: halt.Message,
		Help:   dailyGuardHelp,
		Link:   linkDailyGuard,
	}
}

// marketDataItems is the connection banner: the failing broker session, or
// the broker's non-failure notice while it is fine.
func marketDataItems(m organisms.MarketDataBannerProps) []homepanels.AttentionItem {
	switch {
	case m.Guidance != "":
		title := brokerName(m) + "の市況データを取得できません"
		if m.Persistent {
			title = brokerName(m) + "に未ログインの状態が続いています"
		}
		return []homepanels.AttentionItem{{
			Key: keyMarketData, Level: homepanels.LevelAlert,
			Title: title, Detail: m.Guidance, Help: "自動的に再試行します。", Link: linkBroker,
		}}
	case m.Notice != "":
		return []homepanels.AttentionItem{{
			Key: keyMarketDataNotice, Level: homepanels.LevelWarn, Title: m.Notice, Link: linkBroker,
		}}
	}
	return nil
}

// alertItem is one active health alert (the same wording as its banner).
func alertItem(a domain.HealthAlert) homepanels.AttentionItem {
	return homepanels.AttentionItem{
		Key: a.Code, Level: homepanels.LevelAlert,
		Title:  organisms.HealthAlertTitle(a.Code),
		Detail: a.Since.In(marketcalendar.JST).Format("15:04") + "から続いています。" + organisms.HealthAlertAction(a.Code),
		Help:   "原因が解消すると、この表示は自動的に消えます。",
		Link:   alertLink(a.Code),
	}
}

func alertLink(code string) homepanels.Link {
	if code == domain.HealthAlertMarketDataIdle {
		return linkActivity
	}
	return linkBroker
}

func updateItem(in statusInputs) homepanels.AttentionItem {
	detail, help := "自動更新の準備をしています。", ""
	switch {
	case in.update.Ready:
		detail = "ダウンロードが完了しました。まもなく自動的に再起動して更新します。"
	case in.update.Blocked:
		detail = "安全条件が揃い次第、自動的に更新・再起動されます。"
		help = "ポジション解消・Kill Switch解除・直近発注から5分経過の3つが揃うと更新します。"
	}
	return homepanels.AttentionItem{
		Key: keyUpdate, Level: homepanels.LevelInfo,
		Title: "新バージョン " + in.update.Version + " が利用可能です", Detail: detail, Help: help, Link: linkUpdate,
	}
}

// codeItem words a health_today attention code that no banner covers (or whose
// banner source could not be read). ok is false for a code Home does not know.
func codeItem(code string, h Health) (homepanels.AttentionItem, bool) {
	warn := func(title, detail, help string, link homepanels.Link) (homepanels.AttentionItem, bool) {
		return homepanels.AttentionItem{Key: code, Level: homepanels.LevelWarn, Title: title, Detail: detail, Help: help, Link: link}, true
	}
	switch code {
	case codeNoPaperTrades:
		return warn("今日はまだ Paper のポジションを開いていません", "午前の立会が終わりましたが、新規エントリーがありません。", "理由は下の「エントリーしなかった理由」で確認できます。", linkScanner)
	case codeNoActiveModel:
		return warn("売買判断モデルが採用されていません", "モデルが採用されるまで、通常のエントリーは行われません。", "", linkMetaModel)
	case domain.HealthAlertKabuNotLoggedIn, domain.HealthAlertKabuUnreachable, domain.HealthAlertMarketDataIdle:
		return homepanels.AttentionItem{
			Key: code, Level: homepanels.LevelAlert,
			Title: organisms.HealthAlertTitle(code), Detail: organisms.HealthAlertAction(code), Link: alertLink(code),
		}, true
	case codeMissingSession:
		return warn("市況データが欠けている立会日があります: "+strings.Join(h.MissingSessionDates, ", "), "稼働ログで、その日のデータ取得ジョブを確認してください。", "", linkActivity)
	case codeDailyGuard:
		return warn("本日の新規エントリーを停止しました（日次ガード）", "", dailyGuardHelp, linkDailyGuard)
	case codeKillSwitch:
		return killSwitchItem(), true
	case codeRetrainSkipped:
		return warn("夜間の再学習を見送りました", retrainDetail(h), "", linkMetaModel)
	case codeProposalStalled:
		return warn(fmt.Sprintf("処理が止まっている AI の注文提案が %d 件あります", h.StalledProposals), "一覧で状態を確認してください。", "受付後に処理が進んでいない提案です。", linkProposals)
	}
	return homepanels.AttentionItem{}, false
}

// retrainDetail words why the newest nightly retraining was skipped.
func retrainDetail(h Health) string {
	s := "データ不足などで再学習が行われず、その後モデルも作られていません。"
	if !h.RetrainSkippedAt.IsZero() {
		s = formatTime(h.RetrainSkippedAt) + "の" + s
	}
	return s
}

func formatTime(t time.Time) string { return t.In(marketcalendar.JST).Format("01/02 15:04") }
