package settings

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings/derivedpanel"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// stockBudgetNoAutoRiseNote is the notice shown on both the operating capital
// page and this one (issue #834, FR-RISK-10): the stock budget is seeded once
// at startup from the capital then in force, so a raised capital leaves it as
// it is and the surplus is refused as bucket_budget_exceeded until the stock
// budget is saved too.
const stockBudgetNoAutoRiseNote = "【運用資金を上げても株の枠は自動で増えません】株の枠は起動時に一度だけ、その時点の運用資金で決まり、その後に運用資金を上げても変わりません。" +
	"増えた分で新規エントリーするには、「資産ごとの予算枠」ページで株の枠も保存してください（枠を超える分は bucket_budget_exceeded で見送られます）。"

// bucketGroup holds the Paper per-asset-class budgets (予算枠, issue #824):
// how much of the operating capital the stock bucket and the bond ETF bucket
// may each hold. It sits on its own Settings page; the operating capital they
// split stays in paperRiskGroup. Paper only: Live keeps one whole-account
// bucket.
var bucketGroup = opsGroup{
	id:   "asset-buckets",
	name: "資産ごとの予算枠",
	description: "Paper の運用資金を、株と債券ETFの 2 つの枠に分けて使える額（円）を決めます（issue #824）。" +
		"2 つの枠の合計は運用資金以下にします。新規エントリーは銘柄の資産クラスの枠だけで判定され（issue #833）、債券ETFの取り込み・エントリーはまだありません（issue #823 の第 2 段階以降）。未保存の間は、株の枠が運用資金の全額、債券ETFの枠が 0 円で、枠を設ける前と挙動は変わりません。",
	note: "保存した値は再起動不要で、次のリスク判定から反映されます。Paper 専用です。Live の値と動作はこの設定の影響を受けません。" +
		"株の枠は、保有中の株の建玉（時価ベース。総エクスポージャ上限と同じ計算）に今回のエントリー額を足して枠を超えると見送ります。" +
		"枠を 0 円にするとその枠の新規エントリーを止めます（保有中の建玉は通常どおり決済されます）。" +
		"2 つの枠の合計が運用資金を超える保存は、運用資金を下げる保存・「既定に戻す」も含めて拒否されます。" +
		"枠の間で予算を移すときは、減らす方を先に保存してから増やす方を保存してください。" +
		stockBudgetNoAutoRiseNote,
	fields: []opsField{
		{key: bucketsettings.KeyStockBudgetJPY, label: "株の予算枠（円）", placeholder: "500000",
			hint: "株の新規エントリーに使える予算です。0 で株の新規エントリーを止めます。債券ETFの枠との合計が運用資金以下の、0 以上の整数です。"},
		{key: bucketsettings.KeyBondETFBudgetJPY, label: "債券ETFの予算枠（円）", placeholder: "0",
			hint: "債券ETFに使える予算です。0 でこの枠を使いません。株の枠との合計が運用資金以下の、0 以上の整数です。"},
	},
}

// bucketDerived is the group's 計算結果 panel (derivedpanel.BucketItems). A
// read failure is logged and shown as such instead of failing the screen.
func (h *SettingsHandler) bucketDerived(ctx context.Context) []molecules.SettingDerivedItem {
	var capital float64
	var b bucketsettings.Budgets
	for key, dst := range map[string]*float64{
		papersettings.KeyInitialCapitalJPY: &capital,
		bucketsettings.KeyStockBudgetJPY:   &b.StockJPY,
		bucketsettings.KeyBondETFBudgetJPY: &b.BondETFJPY,
	} {
		v, err := h.ops.Get(ctx, key)
		if err == nil {
			*dst, err = strconv.ParseFloat(v.Current, 64)
		}
		if err != nil {
			slog.Error("settings: read bucket budgets; showing the derived panel as unreadable", "key", key, "error", err)
			return []molecules.SettingDerivedItem{{Key: "error", Label: "計算結果を読み取れませんでした", Off: true}}
		}
	}
	return derivedpanel.BucketItems(capital, b)
}
