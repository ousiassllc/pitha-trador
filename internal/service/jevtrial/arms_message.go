package jevtrial

import (
	"errors"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

// armsMessage words an invalid arms setting in Japanese for the screen: which
// part is wrong, never the setting's key or the Go error text.
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
	case jevtrialsettings.ArmsErrJSON:
		return "アームの設定を JSON の配列として読み取れません（括弧・カンマ・項目名を確認してください）"
	case jevtrialsettings.ArmsErrTooMany:
		return "アームは最大 " + strconv.Itoa(jevtrialsettings.MaxArms) + " 件までです"
	case jevtrialsettings.ArmsErrDuplicateID:
		return which + " の id が重複しています"
	case jevtrialsettings.ArmsErrID:
		return "アームの id は英小文字・数字・. _ - の 1〜48 文字で指定してください"
	case jevtrialsettings.ArmsErrProvider:
		return which + " の provider は openrouter にしてください"
	case jevtrialsettings.ArmsErrModel:
		return which + " の model を 1〜" + strconv.Itoa(jevtrialsettings.MaxModelLen) + " 文字で指定してください"
	case jevtrialsettings.ArmsErrPromptVersion:
		return which + " の prompt_version を指定してください"
	case jevtrialsettings.ArmsErrCap:
		return which + " の daily_cost_cap_usd は 0 より大きく " + strconv.FormatFloat(jevshadowsettings.MaxDailyCostCapUSD, 'g', -1, 64) + " 以下にしてください"
	case jevtrialsettings.ArmsErrListPrice:
		return which + " の list_price_usd_per_mtok は 0〜" + strconv.FormatFloat(jevtrialsettings.MaxListPriceUSDPerMtok, 'g', -1, 64) + " にしてください"
	case jevtrialsettings.ArmsErrProviderOnly:
		return which + " の provider_only は最大 " + strconv.Itoa(jevtrialsettings.MaxProviderOnly) + " 件の提供元名（英数字と . _ - / :）で指定してください"
	case jevtrialsettings.ArmsErrDataCollection:
		return which + " の data_collection は deny か allow にしてください"
	}
	return "アームの設定が正しくありません"
}
