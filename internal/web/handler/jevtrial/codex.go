package jevtrial

import (
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
	view "github.com/ousiassllc/pitha-trador/internal/web/organisms/jevtrial"
)

func codexProps(c jevtrial.CodexStatus) view.CodexProps {
	return view.CodexProps{
		Enabled: c.Enabled,
		CallCap: strconv.Itoa(c.CallCap), TokenCap: strconv.Itoa(c.TokenCap),
		CallCapIsDefault: c.CallCapIsDefault, TokenCapIsDefault: c.TokenCapIsDefault,
		CallsToday: c.CallsToday, TokensToday: c.TokensToday,
		LoginNote: codexLoginNote(c.Login),
		StopNote:  codexStopNote(c.Halt),
	}
}

// codexLoginNote says what the last login check found. It is the state only
// (never the CLI's output): the app writes it each time it asks the CLI before a call.
func codexLoginNote(l *jevtrialsettings.CodexLogin) string {
	if l == nil {
		return "まだ確認していません（アームを動かすと確認します。専用の CODEX_HOME は環境変数 PITHA_CODEX_HOME で指定します）"
	}
	at, _ := time.Parse(time.RFC3339, l.At)
	when := "（" + atoms.FormatJST(at) + " に確認）"
	switch l.State {
	case jevtrialsettings.CodexLoginChatGPT:
		return "ChatGPT でログイン済み" + when
	case jevtrialsettings.CodexLoginLoggedOut:
		return "ログインしていません。専用の CODEX_HOME に自分でログインしてください" + when
	case jevtrialsettings.CodexLoginOtherAuth:
		return "ChatGPT のログインとして確認できないため使いません（API キーのログインは従量課金になります）" + when
	}
	return "確認できません（Codex CLI か専用の CODEX_HOME（PITHA_CODEX_HOME）が見つかりません）" + when
}

// codexStopNote says why the provider is stopped now ("" for not stopped): the
// reason code in words and the time it resumes.
func codexStopNote(h *jevtrialsettings.CodexHalt) string {
	if h == nil {
		return ""
	}
	until, _ := time.Parse(time.RFC3339, h.Until)
	resume := atoms.FormatJST(until) + " まで"
	if !h.UntilKnown {
		resume += "（再開の時刻が分からないため、その日いっぱい）"
	}
	switch h.Reason {
	case jevtrialsettings.CodexHaltUsageLimit:
		return "ChatGPT の使用量の上限に達したため、" + resume + " 止まっています（再試行しません）。"
	case jevtrialsettings.CodexHaltCallCap:
		return "1 日の呼び出し回数の上限（" + strconv.Itoa(h.Calls) + " 回）に達したため、" + resume + " 止まっています。"
	case jevtrialsettings.CodexHaltTokenCap:
		return "1 日のトークン数の上限（" + strconv.Itoa(h.Tokens) + " トークン）に達したため、" + resume + " 止まっています。"
	}
	return "呼び出しが失敗したため（上限と確認できた文面ではありません）、" + resume + " 止まっています（再試行しません）。"
}

// codexArmNote is the status of a Codex arm that has no stop of its own: the
// provider's switch or stop, "" while it runs.
func codexArmNote(c jevtrial.CodexStatus) string {
	switch {
	case !c.Enabled:
		return "ChatGPT サブスクが無効のため動いていません"
	case c.Halt != nil:
		return "ChatGPT サブスク全体が止まっています（上の表示を参照）"
	}
	return ""
}
