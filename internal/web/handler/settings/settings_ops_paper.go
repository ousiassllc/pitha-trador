package settings

import (
	"context"
	"log/slog"
	"math"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/sizing"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// paperRiskGroup holds the Paper operating capital and its exposure limits
// (issue #804) next to the daily guard's operating capital, so the two
// capitals are set - and compared (整合チェック) - on one screen. Paper only:
// Live keeps using config/risk.yaml.
var paperRiskGroup = opsGroup{
	id:   "paper-risk",
	name: "Paper の運用資金・1銘柄/総エクスポージャ上限",
	description: "Paper Trading の運用資金（ポジションサイズ計算の分母）と、1 銘柄・全体の建玉上限（運用資金に対する %）です（issue #804）。" +
		"Paper の初期値は運用資金 500,000 円・1 銘柄 100%・総エクスポージャ 100% です（50 万円を同一銘柄に全部載せても可）。" +
		"日次ガード用の運用資金も同じ値にそろえられます（異なると警告が出ます）。" +
		"なお、メタラベリングの Jev 学習データの終わり（metalabel.llm_cutoff と確認記録のモデル名・確認日・出典）は、同じ Settings の「成績・検証」ページ >「メタラベリング」で設定します。",
	note: "保存した値は再起動不要で、次のリスク判定（候補ごとのサイズ計算と上限チェック）から反映されます。未保存の項目は config/risk.yaml の Paper の値を使い、「既定に戻す」でその値に戻ります。" +
		"Paper 専用です。Live の値と動作はこの設定の影響を受けず、config/risk.yaml だけで決まります。" +
		"運用資金は 0 円より大きく 1 兆円以下、% は 0 より大きく 100 以下です。" +
		"1 回の取引の最大損失（max_trade_loss_pct）・最大建玉数など他の上限は config/risk.yaml のままで、ここでは変えられません。" +
		"他の上限は別に判定されるため、サイズ計算を通っても他のガードで見送られることがあります。",
	fields: []opsField{
		{key: papersettings.KeyInitialCapitalJPY, label: "Paper の運用資金（円）", placeholder: "500000",
			hint: "ポジションサイズ計算と、1 銘柄・総エクスポージャ・日次損失などの % 上限の分母です（例: 500000）。0 円より大きく 1 兆円以下です。"},
		{key: papersettings.KeyMaxPositionPerSymbolPct, label: "1 銘柄の上限（運用資金に対する %）", placeholder: "100",
			hint: "1 銘柄に載せられる建玉の上限です（例: 100 なら運用資金の全額まで）。0 より大きく 100 以下です。"},
		{key: papersettings.KeyMaxTotalExposurePct, label: "総エクスポージャの上限（運用資金に対する %）", placeholder: "100",
			hint: "保有中の建玉の合計の上限です（例: 100 なら運用資金の全額まで）。0 より大きく 100 以下です。"},
		{key: dailyguardsettings.KeyOperatingCapitalJPY, label: "日次ガード用の運用資金（円）", placeholder: "0（未設定）",
			hint: "「1日の損失上限・利益確保」の発動額を計算する元になる運用資金です（% × 運用資金 = 円）。Paper の運用資金と同じ値にそろえてください（異なると警告が出ます）。0 で未設定です（% 指定のルールは無効になります）。"},
	},
}

// paperLimitsSource is the optional capability of an OperationalSettings that
// knows the Paper limits in force (*opsettings.Service.PaperLimits).
type paperLimitsSource interface {
	PaperLimits(ctx context.Context) (config.RiskLimits, error)
}

// paperRiskDerived is the group's 計算結果 panel: what the sizing does with the
// values in force (the largest share price one 100-share lot fits at, and
// the notices when it does not), then the daily guard 整合チェック. A read
// failure is logged and shown as such instead of failing the screen.
func (h *SettingsHandler) paperRiskDerived(ctx context.Context) []molecules.SettingDerivedItem {
	src, ok := h.ops.(paperLimitsSource)
	if !ok {
		return nil
	}
	limits, err := src.PaperLimits(ctx)
	if err != nil {
		slog.Error("settings: read Paper risk limits; showing the derived panel as unreadable", "error", err)
		return []molecules.SettingDerivedItem{{Key: "error", Label: "計算結果を読み取れませんでした", Off: true}}
	}
	items := paperSizingItems(limits, h.screenerMaxPrice(ctx))
	if ref, ok := h.riskReference(ctx); ok {
		if resolved, err := h.ops.DailyGuard(ctx); err != nil {
			slog.Error("settings: read daily guard configuration for the Paper consistency check", "error", err)
		} else {
			items = append(items, consistencyItems(dailyguardsettings.CheckConsistency(resolved, ref))...)
		}
	}
	return items
}

// screenerMaxPrice is the effective Fast Screener upper price (0 when it
// cannot be read): the highest price a candidate can have.
func (h *SettingsHandler) screenerMaxPrice(ctx context.Context) float64 {
	v, err := h.ops.Get(ctx, "screener.max_price")
	if err != nil {
		return 0
	}
	f, err := strconv.ParseFloat(v.Current, 64)
	if err != nil {
		return 0
	}
	return f
}

// paperSizingItems derives, from the limits in force, the largest share price
// at which one 100-share lot (sizing.TradingLotSize) still passes each bound
// of sizing.Quantity, with a warning when it does not fit: a lot above the
// capital, or candidates (up to screenerMaxPrice) the limits cannot afford.
func paperSizingItems(l config.RiskLimits, screenerMaxPrice float64) []molecules.SettingDerivedItem {
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
			Detail: "（risk.yaml の max_trade_loss_pct " + dailyguardsettings.FormatNumber(l.MaxTradeLossPct) + "% と初期ストップ " + dailyguardsettings.FormatNumber(sizing.DefaultStopLossPct) + "%。ここでは変えられません）"})
	exposureMax := math.Min(symbolMax, totalMax)
	if lossMax < exposureMax {
		// The trade-loss bound (not changeable here) is tighter than the two
		// limits set on this screen: say so, it is why a lot within the
		// capital can still be skipped.
		items = append(items, molecules.SettingDerivedItem{Key: "lot-trade-loss-binding", Warn: true,
			Label: "株価が " + yen(lossMax) + " を超える銘柄は、1 単元が運用資金・上限に収まる株価（" + yen(exposureMax) + " 以下）でも、1 回の最大損失の上限（risk.yaml の max_trade_loss_pct）でサイズ計算に見送られます"})
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
