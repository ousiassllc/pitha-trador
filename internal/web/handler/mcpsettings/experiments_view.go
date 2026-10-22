package mcpsettings

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/mcp"
)

// minTradesNote states the minimum number of trades and that the floor is a
// provisional value (issue #871).
func minTradesNote(min int, source string) string {
	note := fmt.Sprintf("判定には、前後それぞれに %d 件以上のクローズ済み取引が要ります（下限は %d 件で、これより下げることはできません）。", min, experiment.MinTradesFloor)
	if source == experiment.MinTradesSourceFloor {
		note += "この下限の件数（" + strconv.Itoa(experiment.MinTradesFloor) + " 件）は" + experiment.MinTradesProvisionalText + "です。あいりの承認も未です。"
	}
	return note
}

func experimentsProps(rows []experiment.Summary, notice string) mcp.ExperimentsProps {
	props := mcp.ExperimentsProps{
		Notice:        notice,
		MinTradesNote: minTradesNote(experiment.MinTradesFloor, experiment.MinTradesSourceFloor),
	}
	for _, s := range rows {
		e := s.Experiment
		props.Rows = append(props.Rows, mcp.ExperimentRow{
			ID: e.ID, Status: e.Status, StatusLabel: statusLabel(e.Status), Period: periodText(e.StartedAt, e.EndedAt),
			Variable: e.Variable.Name, Change: changeText(e.Variable), By: createdBy(e),
			BeforeTrades: s.BeforeTrades, AfterTrades: s.AfterTrades,
			Verdict: s.Verdict, VerdictLabel: verdictLabel(s.Verdict), Running: e.Status == experiment.StatusRunning,
		})
	}
	return props
}

func detailProps(c experiment.Comparison) mcp.ExperimentDetailProps {
	e := c.Experiment
	p := mcp.ExperimentDetailProps{
		ID: e.ID, Hypothesis: e.Hypothesis, Variable: e.Variable.Name, Change: changeText(e.Variable),
		BeforePeriod: periodText(c.Before.From, &c.Before.To), AfterPeriod: periodText(c.After.From, &c.After.To),
		Verdict: c.Verdict, VerdictLabel: verdictLabel(c.Verdict), MinTradesNote: minTradesNote(c.MinTrades, c.MinTradesSource),
		TestNote: testNote(c.PerTradePnl),
		Metrics:  metricRows(c), Rejects: rejectRows(c.Before.RiskRejections, c.After.RiskRejections),
	}
	for _, f := range c.Confounders {
		p.Confounders = append(p.Confounders, confounderLine(f))
	}
	return p
}

// confounderLine is confounderText, except for a recorded switch of the
// configured Jev model (issue #885), whose Values are [before, after].
func confounderLine(f experiment.Confounder) string {
	if f.Kind == experiment.KindModel && f.Name == experiment.ModelSwitchName && len(f.Values) == 2 {
		return fmt.Sprintf("交絡: Jev のモデル（JEV_MODEL）が切り替えられました（%s、%s → %s）。", jst(f.At), f.Values[0], f.Values[1])
	}
	return confounderText(f)
}

func metricRows(c experiment.Comparison) []mcp.MetricRow {
	b, a := c.Before.Performance, c.After.Performance
	return []mcp.MetricRow{
		{Label: "取引件数", Before: strconv.Itoa(b.ClosedTradeCount), After: strconv.Itoa(a.ClosedTradeCount), Diff: signedInt(a.ClosedTradeCount - b.ClosedTradeCount)},
		{Label: "純損益（コスト控除後）", Before: yen(b.TotalPnlJPY), After: yen(a.TotalPnlJPY), Diff: signedYen(c.Diff.TotalPnlJPY)},
		{Label: "1 取引あたりの純損益（判定の基準）", Before: optYen2(c.PerTradePnl.Before.MeanJPY), After: optYen2(c.PerTradePnl.After.MeanJPY), Diff: optSignedYen2(c.PerTradePnl.Diff)},
		{Label: "勝率（参考）", Before: percent(b.WinRate), After: percent(a.WinRate)},
		{Label: "平均の勝ち", Before: optYen(b.AvgWinJPY), After: optYen(a.AvgWinJPY)},
		{Label: "平均の負け", Before: optYen(b.AvgLossJPY), After: optYen(a.AvgLossJPY)},
		{Label: "最大ドローダウン", Before: yen(b.MaxDrawdownJPY), After: yen(a.MaxDrawdownJPY), Diff: signedYen(c.Diff.MaxDrawdownJPY)},
	}
}

// rejectRows joins the two periods' rejection reasons: the Risk Engine's by
// code, and the Policy Engine's own rejections by their fixed code.
func rejectRows(before, after *query.RiskRejections) []mcp.RejectRow {
	type key struct{ group, reason string }
	counts := map[key]*mcp.RejectRow{}
	add := func(group string, buckets []query.RejectBucket, isAfter bool) {
		for _, b := range buckets {
			row := counts[key{group, b.Reason}]
			if row == nil {
				row = &mcp.RejectRow{Group: group, Reason: b.Reason}
				counts[key{group, b.Reason}] = row
			}
			if isAfter {
				row.After = b.Count
			} else {
				row.Before = b.Count
			}
		}
	}
	for i, rr := range []*query.RiskRejections{before, after} {
		if rr != nil {
			add("Risk Engine", rr.Buckets, i == 1)
			add("Policy Engine など", rr.OtherRejectBreakdown, i == 1)
		}
	}
	out := make([]mcp.RejectRow, 0, len(counts))
	for _, r := range counts {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(x, y mcp.RejectRow) int {
		if x.Group != y.Group {
			return -cmp.Compare(x.Group, y.Group) // Risk Engine first
		}
		if d := (y.Before + y.After) - (x.Before + x.After); d != 0 {
			return d
		}
		return cmp.Compare(x.Reason, y.Reason)
	})
	return out
}

func confounderText(f experiment.Confounder) string {
	when := "実験中（後の期間）に"
	switch f.Period {
	case experiment.PeriodBefore:
		when = "実験の前の期間に"
	case experiment.PeriodBoundary:
		when = "実験の前の期間と後の期間を通して"
	}
	switch f.Kind {
	case experiment.KindSetting:
		return fmt.Sprintf("交絡: %s設定 %s が変わりました（%s、監査 ID %d）。", when, f.Name, jst(f.At), deref(f.AuditID))
	case experiment.KindModel:
		return fmt.Sprintf("交絡: %s Jev（%s）のモデルが複数使われました: %v。", when, f.Name, f.Values)
	default:
		return fmt.Sprintf("交絡: %s Jev（%s）のプロンプトの版が複数使われました: %v。", when, f.Name, f.Values)
	}
}

func statusLabel(s string) string {
	switch s {
	case experiment.StatusRunning:
		return "実行中"
	case experiment.StatusClosed:
		return "終了"
	case experiment.StatusAborted:
		return "中止"
	}
	return s
}

func verdictLabel(v string) string {
	switch v {
	case experiment.VerdictInsufficientTrades:
		return "判定なし（取引件数が足りない）"
	case experiment.VerdictConfounded:
		return "判定なし（交絡あり）"
	case experiment.VerdictAborted:
		return "判定なし（中止）"
	case experiment.VerdictNotComputable:
		return "判定なし（全取引の損益が同じで、信頼区間を計算できない）"
	case experiment.VerdictImproved:
		return "改善（1 取引あたりの純損益の差の 95% 信頼区間が 0 より上で、最大ドローダウンは増えていない）"
	case experiment.VerdictMixed:
		return "改善とは言えない（1 取引あたりの純損益は増えたが、最大ドローダウンが増えた）"
	case experiment.VerdictWorsened:
		return "悪化（1 取引あたりの純損益の差の 95% 信頼区間が 0 より下）"
	case experiment.VerdictInconclusive:
		return "どちらとも言えない（信頼区間が 0 をまたぐ。差があるとも無いとも言えない）"
	}
	return v
}

// testNote states the interval the verdict rests on and what it cannot show.
func testNote(t experiment.Test) string {
	if !t.Computable() {
		return "1 取引あたりの純損益の差（後 − 前）の信頼区間は計算できませんでした。"
	}
	return fmt.Sprintf("1 取引あたりの純損益の差（後 − 前）は %s、%d%% 信頼区間は %s 〜 %s（Welch の t 検定。t = %.2f、自由度 %.1f）。"+
		"取引は同じ日・同じ相場に偏りうるので、この区間は実際より狭い（楽観的な）可能性があります。前後は別の期間なので、変えたことが原因だという証明にはなりません。",
		optSignedYen2(t.Diff), int(t.CILevel*100), optYen2(t.CILow), optYen2(t.CIHigh), *t.T, *t.DF)
}

// optYen2 is a yen figure rounded to 0.01, "—" when there is none.
func optYen2(v *float64) string {
	if v == nil {
		return "—"
	}
	return yen(math.Round(*v*100) / 100)
}

func optSignedYen2(v *float64) string {
	if v == nil {
		return "—"
	}
	return signedYen(math.Round(*v*100) / 100)
}

func changeText(v experiment.Variable) string {
	return fmt.Sprintf("%s → %s（%s）", v.Before, v.After, kindLabel(v.Kind))
}

func kindLabel(k string) string {
	switch k {
	case experiment.KindSetting:
		return "設定"
	case experiment.KindModel:
		return "Jev のモデル"
	case experiment.KindPrompt:
		return "プロンプトの版"
	}
	return k
}

func createdBy(e experiment.Experiment) string {
	if e.Actor == experiment.ActorMCP {
		if e.Client != "" {
			return "MCP（" + e.Client + "）"
		}
		return "MCP"
	}
	return "Settings 画面"
}

// periodText is "start 〜 end" in JST, or "start 〜 実行中".
func periodText(start string, end *string) string {
	if end == nil {
		return jst(start) + " 〜 実行中"
	}
	return jst(start) + " 〜 " + jst(*end)
}

func jst(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return t.In(query.JST).Format("2006-01-02 15:04")
}

func yen(v float64) string { return dailyguardsettings.FormatYen(v) + " 円" }

func signedYen(v float64) string {
	if v > 0 {
		return "+" + yen(v)
	}
	return yen(v)
}

func signedInt(v int) string {
	if v > 0 {
		return "+" + strconv.Itoa(v)
	}
	return strconv.Itoa(v)
}

func optYen(v *float64) string {
	if v == nil {
		return "—"
	}
	return yen(*v)
}

func percent(v *float64) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v*100, 'f', 1, 64) + " %"
}

func deref(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
