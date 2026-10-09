package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

// Operator guidance texts (safe to show: no credential, no URL).
const (
	guidanceClock      = "PCの時計がずれています（立花証券のサーバ時刻と30秒を超えて差があるとp_errno=8で拒否されます）。Windowsの時刻をNTPサーバと同期してください。"
	guidanceContention = "ログイン直後にセッションが切られています。同じ認証IDで別のプロセス・別のツール（二重起動のpitha-trador、サンプルプログラム、標準Webの自動ログイン等）がログインしていないか確認してください。"
	guidanceDocuments  = "標準Webで各種書面（金商法交付書面等）を確認してください。未読の間はログインが成功しても仮想URLが発行されず、APIを使えません。"
)

func guidanceClosed(reauth time.Duration) string {
	return fmt.Sprintf("立花証券は03:30〜05:30が閉局です。開局後（%s）に自動で再ログインします。", tachibana.FormatClock(reauth))
}

// failure is a classified login failure.
type failure struct {
	issue           broker.SessionIssue
	code            int
	guidance        string
	documentsUnread bool
}

// classify maps a login error onto the neutral session issue and an operator
// guidance. It looks only at error types and codes, never at secrets.
func classify(err error) failure {
	var apiErr *tachibana.APIError
	switch {
	case errors.Is(err, tachibana.ErrDocumentsUnread):
		return failure{issue: broker.SessionIssueDocumentsUnread, guidance: guidanceDocuments, documentsUnread: true}
	case errors.Is(err, tachibana.ErrKeyUnreadable):
		return failure{issue: broker.SessionIssueKeyMismatch,
			guidance: "秘密鍵ファイルを読めません。Settingsの秘密鍵パスと、RSAのPEM形式（PKCS#8）であることを確認してください。"}
	case errors.Is(err, tachibana.ErrDecryptURL):
		return failure{issue: broker.SessionIssueKeyMismatch,
			guidance: "仮想URLを秘密鍵で復号できません。利用設定画面に登録した公開鍵と対になる秘密鍵（本番とデモは別）か確認してください。"}
	case errors.As(err, &apiErr):
		return classifyAPIError(apiErr)
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
		return failure{issue: broker.SessionIssueUnreachable,
			guidance: "立花証券に接続できません。インターネット接続（IPv4）とファイアウォール、立花証券側のメンテナンスを確認してください。"}
	}
	var httpErr *tachibana.HTTPStatusError
	if errors.As(err, &httpErr) {
		return failure{issue: broker.SessionIssueUnreachable, code: httpErr.Status,
			guidance: "立花証券のサーバがエラーを返しています。しばらくしても続く場合は立花証券の障害情報を確認してください。"}
	}
	return failure{issue: broker.SessionIssueUnknown, guidance: "ログインに失敗しました。ログを確認してください。"}
}

// Login business errors (sResultCode, manual 結果コード表) with a dedicated
// guidance.
const (
	resultBadIP         = 10005
	resultPasskey       = 10009
	resultAuthIDInvalid = 10008
	resultLoginRefused  = 10031
	resultLoginLocked   = 10033
	resultPasskeyUnreg  = 10063
	resultPasskeyReg    = 10064
)

func classifyAPIError(e *tachibana.APIError) failure {
	f := failure{code: e.BrokerCode()}
	switch e.Kind() {
	case tachibana.KindOutOfHours:
		f.issue, f.guidance = broker.SessionIssueOutOfHours, "立花証券は閉局中です（03:30〜05:30）。開局後に自動で再ログインします。"
	case tachibana.KindClock:
		f.issue, f.guidance = broker.SessionIssueClockSkew, guidanceClock
	case tachibana.KindBusy:
		f.issue, f.guidance = broker.SessionIssueUnreachable, "立花証券が混雑しています。自動で再試行します。"
	case tachibana.KindHalted:
		f.issue, f.guidance = broker.SessionIssueUnreachable, "立花証券のサービスが停止中です。自動で再試行します。"
	case tachibana.KindBusiness:
		return classifyResultCode(e)
	default:
		f.issue, f.guidance = broker.SessionIssueUnknown, "ログイン要求が拒否されました（p_errno）。ログを確認してください。"
	}
	return f
}

func classifyResultCode(e *tachibana.APIError) failure {
	f := failure{code: e.ResultCode}
	switch e.ResultCode {
	case resultBadIP:
		f.issue, f.guidance = broker.SessionIssueIPRejected, "接続元のIPアドレスが受け付けられません。IPv6ではなくIPv4で接続できているか確認してください。"
	case resultPasskey, resultPasskeyUnreg, resultPasskeyReg:
		f.issue, f.guidance = broker.SessionIssueAPIDisabled, "標準Webでパスキー登録とe支店・API利用設定（「利用する」）を完了してください。"
	case resultLoginLocked:
		f.issue, f.guidance = broker.SessionIssueBadAuthID, "ログインが停止されています。解除は立花証券のコールセンターへ連絡してください。"
	case resultAuthIDInvalid, resultLoginRefused:
		f.issue, f.guidance = broker.SessionIssueBadAuthID, "認証IDが受け付けられません。利用設定画面で生成した認証ID（本番とデモは別）か、API利用設定が「利用する」か確認してください。"
	default:
		f.issue, f.guidance = broker.SessionIssueUnknown, "ログインが業務エラーで拒否されました。ログのコードを確認してください。"
	}
	return f
}
