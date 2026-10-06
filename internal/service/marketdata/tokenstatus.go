package marketdata

import (
	"errors"
	"fmt"
	"net"
)

// kabuステーションAPIの /token が返すエラーコード
// (https://kabucom.github.io/kabusapi/ptal/error.html §2)。
const (
	codeNotLoggedIn   = 4001007 // ログイン認証エラー: kabuステーションにログインしているか確認
	codeAPIDisabled   = 4001008 // API利用不可: API利用設定が完了しているか確認
	codeBadPassword   = 4001013 // トークン取得失敗: ログイン状態でAPIパスワードが不正
	codeNotLoggedInV2 = 4001017 // ログイン認証エラー: kabuステーション未ログイン状態
)

// TokenIssue classifies why the most recent token issuance failed, so the
// UI can tell the operator what to fix (issue #295) instead of one generic
// error. TokenIssueNone means the last issuance succeeded (or none was
// attempted yet).
type TokenIssue string

const (
	TokenIssueNone        TokenIssue = ""
	TokenIssueUnreachable TokenIssue = "unreachable"   // 接続できない: kabuステーション未起動 / API未有効
	TokenIssueNotLoggedIn TokenIssue = "not_logged_in" // 4001007 / 4001017
	TokenIssueAPIDisabled TokenIssue = "api_disabled"  // 4001008
	TokenIssueBadPassword TokenIssue = "bad_password"  // 4001013
	TokenIssueUnknown     TokenIssue = "unknown"       // 上記以外
	TokenIssueRejected    TokenIssue = "rejected"      // /token は成功するが情報系APIが新トークンも拒否
)

// TokenStatus is a snapshot of the last token issuance outcome (Client.
// TokenStatus). Code is the kabuステーションAPI error code when the failure
// carried one (0 otherwise).
type TokenStatus struct {
	Issue TokenIssue
	Code  int
}

// Failed reports whether the last token issuance failed.
func (s TokenStatus) Failed() bool { return s.Issue != TokenIssueNone }

// Guidance words, for the operator, what to check for the failure. It
// returns "" when the last issuance did not fail.
func (s TokenStatus) Guidance() string {
	switch s.Issue {
	case TokenIssueNone:
		return ""
	case TokenIssueUnreachable:
		return "kabuステーションAPIに接続できません。kabuステーションが起動していること、「APIシステム設定」で「APIを利用する」が有効なこと（変更後はkabuステーションの再起動が必要）を確認してください。"
	case TokenIssueNotLoggedIn:
		return "kabuステーションにログインしていません（またはセッションが切れています）。kabuステーション右上のAPIアイコンが緑か確認し、一度ログアウトして再ログインしてください（再ログイン後に再発行します）。「APIを利用する」の設定不備は4001008、APIパスワード不正は4001013で別に通知されます。"
	case TokenIssueAPIDisabled:
		return "kabuステーションのAPI利用設定が完了していません。「APIシステム設定」で「APIを利用する」を有効にしてください。"
	case TokenIssueBadPassword:
		return "APIパスワードが正しくありません。設定画面の KABU_API_PASSWORD を、kabuステーション「APIシステム設定」のAPIパスワードと一致させてください（本番用と検証用の取り違えに注意）。"
	case TokenIssueRejected:
		return fmt.Sprintf("トークンは発行できましたが、板・銘柄情報APIが認証エラー（エラーコード %d）を返し続けています。取得を一時停止し30秒ごとに再試行しています。kabuステーションにログインした状態でも発生する場合は、別のプロセス（本アプリの二重起動や他のAPIツール）が同じkabuステーションの /token を呼んでトークンを取り合っていないか確認し、kabuステーションを一度ログアウトして再ログインしてください。", s.Code)
	case TokenIssueUnknown:
		fallthrough
	default:
		if s.Code != 0 {
			return fmt.Sprintf("kabuステーションAPIのトークンを取得できませんでした（エラーコード %d）。詳細はログを確認してください。", s.Code)
		}
		return "kabuステーションAPIのトークンを取得できませんでした。詳細はログを確認してください。"
	}
}

// classifyTokenError maps an IssueToken error to a TokenStatus: a kabu
// API error code selects the matching cause; a failure to reach the API at
// all (connection refused, timeout - on Windows the dial error carries
// WSAECONNREFUSED, which errors.Is(syscall.ECONNREFUSED) does not match, so
// any *net.OpError is treated alike) means it is not listening.
func classifyTokenError(err error) TokenStatus {
	if err == nil {
		return TokenStatus{}
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		status := TokenStatus{Issue: TokenIssueUnknown, Code: apiErr.Code}
		switch apiErr.Code {
		case codeNotLoggedIn, codeNotLoggedInV2:
			status.Issue = TokenIssueNotLoggedIn
		case codeAPIDisabled:
			status.Issue = TokenIssueAPIDisabled
		case codeBadPassword:
			status.Issue = TokenIssueBadPassword
		}
		return status
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return TokenStatus{Issue: TokenIssueUnreachable}
	}
	return TokenStatus{Issue: TokenIssueUnknown}
}
