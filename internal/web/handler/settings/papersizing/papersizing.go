// Package papersizing builds the sizing lines of the Settings screen's Paper
// risk 計算結果 panel (issue #804): for the limits in force, the largest share
// price at which one 100-share lot passes each bound of sizing.Quantity, and
// the notices when it does not. It is a pure function of the limits, split out
// of internal/web/handler/settings to keep that directory within linterly's
// size limit.
package papersizing

import (
	"math"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/sizing"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// Items derives, from the limits in force, the largest share price
// at which one 100-share lot (sizing.TradingLotSize) still passes each bound
// of sizing.Quantity, with a warning when it does not fit: a lot above the
// capital, or candidates (up to screenerMaxPrice) the limits cannot afford.
func Items(l config.RiskLimits, screenerMaxPrice float64) []molecules.SettingDerivedItem {
	items := []molecules.SettingDerivedItem{{Key: "sizing", Heading: true, Label: "サイズ計算", Detail: "（1 単元 = " + strconv.Itoa(sizing.TradingLotSize) + " 株）"}}
	if l.InitialCapital <= 0 {
		return append(items, molecules.SettingDerivedItem{Key: "lot-no-capital", Label: "運用資金が未設定のため、新規エントリーはすべて見送られます", Warn: true})
	}
	lot := float64(sizing.TradingLotSize)
	symbolMax := affordablePrice(l.InitialCapital*l.MaxPositionPerSymbolPct/100, lot)
	totalMax := affordablePrice(l.InitialCapital*l.MaxTotalExposurePct/100, lot)
	lossMax := affordablePrice(l.InitialCapital*l.MaxTradeLossPct/100/(sizing.DefaultStopLossPct/100), lot)
	yen := func(v float64) string { return dailyguardsettings.FormatYen(v) + " 円" }
	items = append(items,
		molecules.SettingDerivedItem{Key: "lot-symbol", Label: "1 銘柄の上限で 1 単元を買える株価の上限", Value: yen(symbolMax),
			Detail: "（運用資金 " + yen(l.InitialCapital) + " × " + dailyguardsettings.FormatNumber(l.MaxPositionPerSymbolPct) + "%）"},
		molecules.SettingDerivedItem{Key: "lot-total", Label: "総エクスポージャの上限で 1 単元を買える株価の上限", Value: yen(totalMax),
			Detail: "（運用資金 × " + dailyguardsettings.FormatNumber(l.MaxTotalExposurePct) + "%、建玉なしの場合）"},
		molecules.SettingDerivedItem{Key: "lot-trade-loss", Label: "1 回の最大損失の上限で 1 単元を買える株価の上限", Value: yen(lossMax),
			Detail: "（1 回の最大損失 " + dailyguardsettings.FormatNumber(l.MaxTradeLossPct) + "% と初期ストップ " + dailyguardsettings.FormatNumber(sizing.DefaultStopLossPct) + "%）"})
	exposureMax := math.Min(symbolMax, totalMax)
	if lossMax < exposureMax {
		// The trade-loss bound is tighter than the two exposure limits: say so,
		// it is why a lot within the capital can still be skipped.
		items = append(items, molecules.SettingDerivedItem{Key: "lot-trade-loss-binding", Warn: true,
			Label: "株価が " + yen(lossMax) + " を超える銘柄は、1 単元が運用資金・上限に収まる株価（" + yen(exposureMax) + " 以下）でも、1 回の最大損失の上限（上の「1 回の取引の最大損失」）でサイズ計算に見送られます"})
	}
	if lotCost := screenerMaxPrice * lot; screenerMaxPrice > 0 && lotCost > l.InitialCapital {
		items = append(items, molecules.SettingDerivedItem{Key: "lot-exceeds-capital", Warn: true,
			Label: "候補の最高株価 " + yen(screenerMaxPrice) + " の銘柄は 1 単元（100 株）が " + yen(lotCost) + " で運用資金 " + yen(l.InitialCapital) +
				" を超えるため買えません（運用資金内で買えるのは株価 " + yen(affordablePrice(l.InitialCapital, lot)) + " 以下の銘柄です）"})
	}
	return items
}

// affordablePrice is the largest whole-yen share price at which lot shares
// cost at most budget.
func affordablePrice(budget, lot float64) float64 { return math.Floor(budget / lot) }
