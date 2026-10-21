package derivedpanel

import (
	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// BucketItems is the 資産ごとの予算枠 group's panel (issue #824): the
// operating capital, the budgets' total and what is left of the capital
// (capital - total), all in yen, plus a warning while the total is over the
// capital (a stored state the save checks normally prevent).
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
	return items
}
