package mcpsettings

import (
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/admin"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/mcp"
)

// panelProps maps the admin View to the organism's props. revealedKey is
// the freshly generated key on the key action's response, "" otherwise.
func panelProps(v admin.View, notice, revealedKey string) mcp.PanelProps {
	return mcp.PanelProps{
		Enabled:       v.Enabled,
		Listen:        v.Listen,
		State:         string(v.State),
		Note:          reasonText(v.Reason),
		KeyConfigured: v.KeyConfigured,
		URL:           v.URL,
		AuthHeader:    v.AuthHeader,
		ExePath:       v.ExePath,
		ExeExists:     v.ExeExists,
		SettingsPath:  v.SettingsPath,
		Warning:       loadWarning(v.LoadError),
		Notice:        notice,
		RevealedKey:   revealedKey,
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
	case admin.ReasonNotRunning:
		return "有効ですが、そのアドレスで待ち受けている MCP が見つかりません。paper-results-mcp を起動してください（設定の変更後は再起動が必要です）。"
	default:
		return ""
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
