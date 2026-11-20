package jevtrial

import (
	"errors"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

// armsMessage words an invalid arm in Japanese for the screen: which part is
// wrong, never the setting's key or the Go error text. The field names are the
// ones the form labels carry.
func armsMessage(err error) string {
	var ae *jevtrialsettings.ArmsError
	if !errors.As(err, &ae) {
		return "アームの設定を読み取れません"
	}
	which := "アーム"
	if ae.ArmID != "" {
		which = "アーム " + strconv.Quote(ae.ArmID)
	}
	switch ae.Kind {
	case jevtrialsettings.ArmsErrTooMany:
		return "アームは最大 " + strconv.Itoa(jevtrialsettings.MaxArms) + " 件までです"
	case jevtrialsettings.ArmsErrDuplicateID:
		return which + " のアーム名が重複しています"
	case jevtrialsettings.ArmsErrID:
		return "アーム名は英小文字・数字・. _ - の 1〜48 文字で入力してください"
	case jevtrialsettings.ArmsErrProvider:
		return which + " の提供元は openrouter・custom・codex_chatgpt のどれかを選んでください（送り先・データの扱い・ZDR は OpenRouter 専用、codex_chatgpt では 1 日の上限と定価も使えません）"
	case jevtrialsettings.ArmsErrModel:
		return which + " のモデルを 1〜" + strconv.Itoa(jevtrialsettings.MaxModelLen) + " 文字で入力してください"
	case jevtrialsettings.ArmsErrPromptVersion:
		return which + " のプロンプトの版を選んでください"
	case jevtrialsettings.ArmsErrCap:
		return which + " の 1 日の上限は 0 より大きく " + strconv.FormatFloat(jevshadowsettings.MaxDailyCostCapUSD, 'g', -1, 64) + " USD 以下にしてください"
	case jevtrialsettings.ArmsErrListPrice:
		return which + " の定価は 0〜" + strconv.FormatFloat(jevtrialsettings.MaxListPriceUSDPerMtok, 'g', -1, 64) + " USD にしてください"
	case jevtrialsettings.ArmsErrProviderOnly:
		return which + " の送り先は最大 " + strconv.Itoa(jevtrialsettings.MaxProviderOnly) + " 件の提供元名（英数字と . _ - / :）で入力してください"
	case jevtrialsettings.ArmsErrDataCollection:
		return which + " のデータの扱いは「学習に使わない提供元だけ」か「制限しない」を選んでください"
	case jevtrialsettings.ArmsErrLLMCutoff:
		return which + " の llm_cutoff は空（未確認）か YYYY-MM-DD の日付にしてください"
	case jevtrialsettings.ArmsErrEndpoint:
		return which + " の API の送り先は「既定（/v1/systemone）」か「/alpha/decisions」を選んでください（codex_chatgpt では選べません）"
	}
	return "アームの設定が正しくありません"
}
