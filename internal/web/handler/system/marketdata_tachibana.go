package system

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

// BrokerSelection is the broker choice the banner follows: the effective
// runtime_settings value (settings.OperationalSettings satisfies it). It is
// read per request, like the Setup/Settings screens do.
type BrokerSelection interface {
	Broker(ctx context.Context) (config.BrokerSettings, error)
}

// tachibanaEnvironment is the 立花 environment to name on the banner for a
// failing session with the given issue, or "" when the banner stays the kabu
// one: no broker selection, kabu selected, or one of the kabu-only causes
// (not_logged_in, bad_password - the 立花 adapter never reports them, so the
// kabu adapter is the one running and its 4001007 etc. guidance stands).
func (h *MarketDataHandler) tachibanaEnvironment(ctx context.Context, issue broker.SessionIssue) string {
	if h.selection == nil || issue == broker.SessionIssueNotLoggedIn || issue == broker.SessionIssueBadPassword {
		return ""
	}
	b, err := h.selection.Broker(ctx)
	if err != nil {
		slog.Error("marketdata-status: read broker selection; assuming kabu", "error", err)
		return ""
	}
	if b.Provider != config.BrokerTachibana {
		return ""
	}
	if b.Tachibana.Production() {
		return config.TachibanaEnvProduction
	}
	return config.TachibanaEnvDemo
}

// tachibanaGuidance is the operator-facing cause and remedy for a failing
// 立花 e支店 session (issue #739). The text depends on the Issue and the
// environment only: SessionStatus.Guidance is never shown for 立花, so no
// credential or virtual URL an adapter might put there can reach the HTML.
// A cause outside the 立花 list gets the unknown-cause text.
func tachibanaGuidance(issue broker.SessionIssue, environment string) string {
	env := "デモ環境"
	if environment == config.TachibanaEnvProduction {
		env = "本番環境"
	}
	switch issue {
	case broker.SessionIssueBadAuthID:
		return "認証IDが正しくありません。設定画面の" + env + "の認証IDを、標準Webで取得した" + env + "の認証IDに合わせてください（本番とデモは認証ID・秘密鍵が別セットです）。"
	case broker.SessionIssueKeyMismatch:
		return "秘密鍵を使えません（登録済みの公開鍵と合っていないか、鍵ファイルを復号できません）。設定画面の" + env + "の秘密鍵ファイルが、標準Webで公開鍵を登録した秘密鍵を指しているか確認してください。"
	case broker.SessionIssueAPIDisabled:
		return "標準Webの" + env + "のAPI利用設定が「利用しない」（または無効）になっています。標準WebでAPI利用設定を「利用する」に変更してください。"
	case broker.SessionIssueDocumentsUnread:
		return "標準Webに未読の書面（金商法交付書面等）があるため、仮想URLが発行されずAPIを使えません。標準Webで書面を既読にしてください。"
	case broker.SessionIssueIPRejected:
		return "接続元が拒否されました（エラー10005）。IPv6のみの回線ではAPIを使えないため、IPv4で直結できる回線に切り替えてください。標準Webで固定IPを登録している場合は、現在のグローバルIPと一致しているか確認してください。"
	case broker.SessionIssueClockSkew:
		return "PCの時計がサーバ時刻と30秒以上ずれています（p_errno=8）。PCの時計をNTPで正確に合わせてください。"
	case broker.SessionIssueOutOfHours:
		return "サービス時間外です（03:30〜05:30はログインできません）。05:30以降に自動的にログインし直します。"
	case broker.SessionIssueSessionConflict:
		return "同じ認証IDで別のログインがあり、仮想URLが失効しました（多重ログイン）。本アプリの二重起動や、他のAPIツールで同じ認証IDを使っていないか確認してください。"
	case broker.SessionIssueUnreachable:
		return "立花証券のサーバに接続できません。インターネット接続（IPv4）と、設定画面の" + env + "の接続先URLを確認してください。"
	case broker.SessionIssueRejected:
		return "ログインはできましたが、以降の要求が拒否されています。標準Webで" + env + "のAPI利用設定と書面の既読状況を確認してください。"
	default:
		return "原因を特定できないエラーです。設定画面のエラーログをダウンロードして内容を確認してください。"
	}
}
