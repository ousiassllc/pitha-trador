package mcpsettings

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/admin"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/mcp"
)

// panelProps maps the admin View to the organism's props. revealedKey is
// the freshly generated key on the key action's response, "" otherwise.
func panelProps(v admin.View, notice, revealedKey string) mcp.PanelProps {
	return mcp.PanelProps{
		Enabled:       v.Enabled,
		Listen:        v.Listen,
		State:         string(v.State),
		Note:          stateNote(v),
		KeyConfigured: v.KeyConfigured,
		URL:           v.URL,
		AuthHeader:    v.AuthHeader,
		ExePath:       v.ExePath,
		ExeExists:     v.ExeExists,
		SettingsPath:  v.SettingsPath,
		Warning:       loadWarning(v.LoadError),
		Notice:        notice,
		RevealedKey:   revealedKey,
		Managed:       v.Managed,
		External:      v.External,
		AutoStart:     v.AutoStart,
		AutoStartNote: autoStartNote(v),
		ConfigChanged: v.ConfigChanged,
	}
}

// stateNote words the state in operator-facing sentences. It uses fixed
// strings and the exit code only: nothing the process printed is shown.
func stateNote(v admin.View) string {
	switch v.State {
	case admin.StateStarting:
		return "アプリが MCP を起動しました。待受を確認しています。"
	case admin.StateError:
		return failureText(v.Failure, v.ExitCode)
	case admin.StateListening:
		if v.External {
			return "アプリの外で起動された MCP が待ち受けています（この画面からは停止できません）。"
		}
		return ""
	}
	return reasonText(v.Reason)
}

func failureText(k proc.FailureKind, code int) string {
	switch k {
	case proc.FailExeMissing:
		return "起動に失敗しました: paper-results-mcp の実行ファイルが見つかりません。"
	case proc.FailSpawn:
		return "起動に失敗しました: OS がプロセスを開始できませんでした。"
	case proc.FailPortInUse:
		return "起動に失敗しました: 待受アドレスのポートがすでに使われています。別のアドレスを保存するか、使っているものを止めてから再起動してください。"
	case proc.FailRefused:
		return "MCP は待受を拒否して終了しました（API キー・待受アドレス・有効/無効の設定を確認してください）。"
	case proc.FailSettings:
		return "MCP は設定ファイルを使えず終了しました。保存または API キーの生成で作り直してください。"
	case proc.FailNotListening:
		return "MCP は動いていますが、待受アドレスで待ち受けていません。再起動してください。"
	default:
		if code < 0 {
			return "MCP が強制終了されました。再起動してください。"
		}
		return fmt.Sprintf("MCP が異常終了しました（終了コード %d）。再起動してください。", code)
	}
}

func reasonText(r admin.Reason) string {
	switch r {
	case admin.ReasonDisabled:
		return "無効に設定されています（既定）。"
	case admin.ReasonSettingsUnreadable:
		return "設定ファイルを使えないため、MCP は待ち受けません。"
	case admin.ReasonListenNotAllowed:
		return "保存されている待受アドレスは許可範囲外のため、MCP は待ち受けません。許可されたアドレスで保存し直してください。"
	case admin.ReasonAccessIncomplete:
		return "Cloudflare Access のオリジン照合が有効なのに ID またはシークレットが空のため、MCP は待ち受けません。"
	case admin.ReasonStopping:
		return "停止しています。"
	case admin.ReasonNotRunning:
		return "有効ですが、そのアドレスで待ち受けている MCP が見つかりません。「起動」を押してください。"
	default:
		return ""
	}
}

// autoStartNote tells whether the MCP comes back with the application: the
// stored settings decide (enabled, key, permitted address), the same rule the
// application applies at start-up.
func autoStartNote(v admin.View) string {
	if v.AutoStart {
		return "次回起動時も自動で起動します。"
	}
	switch v.AutoStartBlocked {
	case admin.ReasonDisabled:
		return "自動では起動しません（無効です）。有効にして保存すると、次回からアプリと同時に起動します。"
	case admin.ReasonNoKey:
		return "自動では起動しません（API キーが未設定です）。"
	case admin.ReasonListenNotAllowed:
		return "自動では起動しません（待受アドレスが許可範囲外です）。"
	case admin.ReasonAccessIncomplete:
		return "自動では起動しません（Cloudflare Access の ID またはシークレットが空です）。"
	case admin.ReasonExeMissing:
		return "自動では起動しません（paper-results-mcp の実行ファイルが見つかりません。アプリ本体と同じフォルダに置いてください）。"
	default:
		return "自動では起動しません（設定ファイルを使えません）。"
	}
}

func loadWarning(k admin.LoadErrorKind) string {
	switch k {
	case admin.LoadInsecure:
		return "設定ファイルの権限が緩いため（所有者のみ 0600 が必要）MCP は読み込みません。保存または API キーの生成を行うと、既定値から安全な権限で作り直します（既存のキーは失われます）。"
	case admin.LoadUnusableFile:
		return "設定ファイルを読み込めません。保存または API キーの生成を行うと、既定値から作り直します（既存のキーは失われます）。"
	default:
		return ""
	}
}

// auditRows is how many audit rows the panel lists.
const auditRows = 10

// addWrite fills the settings-write section. A read failure hides nothing: the
// section shows the safe reading (off, no rows) and the failure is logged.
func (h *Handler) addWrite(ctx context.Context, props *mcp.PanelProps) {
	if h.gate == nil {
		return
	}
	props.WriteGate = true
	on, err := h.gate.Enabled(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "mcpsettings: read write switch", "error", err)
	}
	props.WriteEnabled = on && err == nil
	// Off unless the read succeeds and says on: the intake fails closed.
	proposals, err := h.gate.OrderProposalsEnabled(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "mcpsettings: read order proposals switch", "error", err)
	}
	props.OrderProposalsEnabled = proposals && err == nil
	entries, err := h.gate.Recent(ctx, auditRows)
	if err != nil {
		slog.ErrorContext(ctx, "mcpsettings: read audit log", "error", err)
		return
	}
	for _, e := range entries {
		props.Audit = append(props.Audit, auditRow(e))
	}
}

func auditRow(e mcpset.Entry) mcp.AuditRow {
	by := "Settings 画面"
	if e.Actor == mcpset.ActorMCP {
		by = "MCP"
		if e.Client != "" {
			by += "（" + e.Client + "）"
		}
	}
	return mcp.AuditRow{At: e.At, By: by, What: auditAction(e.Action), Key: e.Key, Before: auditValue(e.Before), After: auditValue(e.After)}
}

func auditAction(a string) string {
	switch a {
	case mcpset.ActionSet:
		return "変更"
	case mcpset.ActionReset:
		return "既定に戻す"
	case mcpset.ActionWriteEnable:
		return "書き込みを許可"
	case mcpset.ActionWriteDisable:
		return "書き込みを禁止"
	case mcpset.ActionOrderProposalsEnable:
		return "AIからの注文提案の受け付けをオン"
	case mcpset.ActionOrderProposalsDisable:
		return "AIからの注文提案の受け付けをオフ"
	}
	return a
}

// auditValue words a missing value: no stored row, the default applies.
func auditValue(v *string) string {
	if v == nil {
		return "（既定）"
	}
	return *v
}
