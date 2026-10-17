package dailyguardreplay

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
)

// Render writes the report as a plain-text table (the CLI's output): the
// thresholds, the limitations, one row per day and the totals. Column headers
// are ASCII so the tabwriter aligns them.
func Render(w io.Writer, r Report) error {
	var b strings.Builder
	b.WriteString("日次ガード再現（記録済みの確定損益のみ）\n")
	b.WriteString("閾値: " + describeLimits(r.Limits) + "\n")
	for _, l := range r.Limitations {
		b.WriteString("注意: " + l + "\n")
	}
	if r.MissingPnL > 0 {
		b.WriteString("注意: 確定損益が NULL の取引 " + strconv.Itoa(r.MissingPnL) + " 件は 0 円として扱いました。\n")
	}
	b.WriteString("列: trades=開始数(全/採用/見送り)  pnl=その日にクローズした確定損益(ガードなし/あり/見送り分)  maxdd=確定損益の日中最大ドローダウン(なし/あり)\n\n")

	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintln(tw, "day\ttrades(all/kept/skip)\tpnl_without\tpnl_with\tpnl_skipped\tmaxdd_without\tmaxdd_with\thalt\t")
	for _, d := range r.Days {
		writeRow(tw, d.Day, d, d.HaltReason)
	}
	writeRow(tw, "total", r.Total, strconv.Itoa(r.HaltedDays)+" halted days")
	if err := tw.Flush(); err != nil {
		return err
	}
	b.WriteString("\n結果の評価と Live の既定有効化の基準は決定待ちです（issue #782）。このツールは判断を行いません。\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func writeRow(w io.Writer, label string, d DayResult, halt string) {
	if halt == "" {
		halt = "-"
	}
	_, _ = fmt.Fprintf(w, "%s\t%d/%d/%d\t%s\t%s\t%s\t%s\t%s\t%s\t\n", label,
		d.TradesAll, d.TradesKept, d.TradesSkipped,
		signed(d.PnLWithoutJPY), signed(d.PnLWithJPY), signed(d.PnLSkippedJPY),
		dailyguardsettings.FormatYen(d.MaxDrawdownWithoutJPY), dailyguardsettings.FormatYen(d.MaxDrawdownWithJPY), halt)
}

func signed(v float64) string {
	if v > 0 {
		return "+" + dailyguardsettings.FormatYen(v)
	}
	return dailyguardsettings.FormatYen(v)
}

func describeLimits(l dailyguardsettings.Limits) string {
	var parts []string
	if l.LossLimitEnabled() {
		parts = append(parts, "損失上限 "+dailyguardsettings.FormatYen(l.LossLimitJPY)+" 円")
	} else {
		parts = append(parts, "損失上限 無効")
	}
	if l.ProfitLockEnabled() {
		parts = append(parts, "利益確保 発動 "+dailyguardsettings.FormatYen(l.ProfitLockTriggerJPY)+" 円 / 戻し "+dailyguardsettings.FormatNumber(l.ProfitLockGivebackPct)+"%")
	} else {
		parts = append(parts, "利益確保 無効")
	}
	if l.MaxTradesEnabled() {
		parts = append(parts, "最大取引回数 "+strconv.Itoa(l.MaxTrades)+" 回")
	} else {
		parts = append(parts, "最大取引回数 無効")
	}
	return strings.Join(parts, " / ")
}
