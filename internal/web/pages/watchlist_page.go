package pages

import (
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/labels"
)

// watchSourceLabels are the operator-facing names of domain.WatchList.Source.
var watchSourceLabels = map[string]string{
	domain.WatchListDailyScreen:   "日足スクリーニング",
	domain.WatchListFixed:         "固定リスト",
	domain.WatchListCarriedOver:   "前営業日のリストを引き継ぎ（日足を使えなかったため）",
	domain.WatchListFixedFallback: "固定リストへ切り替え（日足を使えなかったため）",
}

// watchOriginLabels are the operator-facing names of WatchListEntry.Origin.
var watchOriginLabels = map[string]string{
	domain.WatchOriginHeld:   "保有・注文中",
	domain.WatchOriginManual: "手動指定",
	domain.WatchOriginScreen: "スクリーニング",
	domain.WatchOriginFixed:  "固定リスト",
}

// screenIndicatorLabels are the operator-facing names of the screening
// indicators (tachibanasource.TachibanaScreenIndicators).
var screenIndicatorLabels = map[string]string{
	tachibanasource.ScreenGainRate:      "値上がり率",
	tachibanasource.ScreenLossRate:      "値下がり率",
	tachibanasource.ScreenVolume:        "売買高",
	tachibanasource.ScreenTurnover:      "売買代金",
	tachibanasource.ScreenVolumeSurge:   "出来高急増",
	tachibanasource.ScreenTurnoverSurge: "売買代金急増",
	tachibanasource.ScreenRangeRate:     "値幅率",
}

// label is the name of key in names; a key it does not know is not printed.
func label(names map[string]string, key string) string {
	if l, ok := names[key]; ok {
		return l
	}
	return labels.Other
}

// indicatorsText joins the labels of the indicators that selected a symbol.
func indicatorsText(indicators []string) string {
	names := make([]string, len(indicators))
	for i, ind := range indicators {
		names[i] = label(screenIndicatorLabels, ind)
	}
	return strings.Join(names, "・")
}
