package derivedpanel

import (
	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// BucketItems is the 資産ごとの予算枠 group's panel (issue #824): the
// operating capital, the budgets' total and what is left of the capital
// (capital - total), all in yen, plus a warning while the total is over the
// capital (a stored state the save checks normally prevent) and a notice while
// capital is left over (nothing uses it until a budget is saved with it: a
// raised capital never grows the stock budget by itself, FR-RISK-10).
func BucketItems(capital float64, b bucketsettings.Budgets) []molecules.SettingDerivedItem {
	yen := func(v float64) string { return dailyguardsettings.FormatYen(v) + " 円" }
	items := []molecules.SettingDerivedItem{
		{Key: "bucket-capital", Label: "運用資金", Value: yen(capital), Detail: "（「リスク・資金」ページの Paper の運用資金）"},
		{Key: "bucket-total", Label: "枠の合計", Value: yen(b.Total()), Detail: "（株 " + yen(b.StockJPY) + " + 債券ETF " + yen(b.BondETFJPY) + "）"},
		{Key: "bucket-remaining", Label: "残額（運用資金 − 枠の合計）", Value: yen(capital - b.Total())},
	}
	if b.Total() > capital {
		items = append(items, molecules.SettingDerivedItem{Key: "bucket-over", Warn: true, Label: "枠の合計が運用資金を超えています。どちらかの枠を減らしてください"})
	}
	if remaining := capital - b.Total(); remaining > 0 {
		items = append(items, molecules.SettingDerivedItem{Key: "bucket-unused", Warn: true,
			Label: "運用資金のうち " + yen(remaining) + " が、どの枠にも割り当てられておらず使われていません。使うには、株の枠（または債券ETFの枠）を増やして保存してください"})
	}
	return items
}
