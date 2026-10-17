package performance

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/actuals"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/evaluation"
)

const (
	undetermined = "判定不能"
	noValue      = "—"
	noKey        = "(なし)"
)

// evaluationPanel maps an Evaluation onto the plain props of
// evaluation.EvaluationPanel, formatting every figure.
func evaluationPanel(e actuals.Evaluation) evaluation.Panel {
	p := evaluation.Panel{
		Verdict:     string(e.Verdict.Status),
		VerdictText: statusText(e.Verdict.Status),
		Criteria:    criteriaText(e.Criteria),
		CapitalNote: "Paper では仮置きの値で、口座残高ではない（config/risk.yaml の initial_capital）",
		Overall:     statsRow("全期間", e.Overall),
	}
	if e.InitialCapital > 0 {
		p.Capital = "初期資金 " + yen(e.InitialCapital)
	} else {
		p.Capital = "初期資金 未設定（資金比の最大ドローダウンは判定不能）"
	}
	for _, c := range e.Verdict.Checks {
		p.Checks = append(p.Checks, checkView(c, e.Criteria))
	}
	o := e.Overall
	p.Costs = []evaluation.Pair{
		{Label: "取引件数 / 取引日数", Value: fmt.Sprintf("%d 件 / %d 日", o.N, o.Days)},
		{Label: "純損益（ネット）", Value: yen(o.NetPnLJPY)},
		{Label: "手数料", Value: yen(o.FeesJPY)},
		{Label: "グロス（手数料前）", Value: yen(o.GrossBeforeFeesJPY)},
		{Label: "スリッページ（約定価格に含まれる）", Value: fmt.Sprintf("%s（記録のある約定 %d）", yen(o.SlippageJPY), o.SlippageRecordedFill)},
		{Label: "グロス（コスト前）", Value: yen(o.GrossBeforeCostsJPY)},
	}
	for _, t := range []struct {
		id, title, header string
		groups            []actuals.Group
	}{
		{"day", "日別（JST）", "日", e.ByDay},
		{"week", "週別（月曜始まり）", "週", e.ByWeek},
		{"direction", "方向別", "方向", e.ByDirection},
		{"exit-reason", "exit_reason別", "exit_reason", e.ByExitReason},
		{"meta-model", "メタモデル版別（metalabel_models.version）", "版", e.ByMetaModel},
		{"ab-arm", "A/Bアーム別", "アーム", e.ByABArm},
	} {
		table := evaluation.Table{ID: t.id, Title: t.title, KeyHeader: t.header}
		for _, g := range t.groups {
			key := g.Key
			if key == "" {
				key = noKey
			}
			table.Rows = append(table.Rows, statsRow(key, g.Stats))
		}
		p.Tables = append(p.Tables, table)
	}
	for _, j := range e.Jev {
		p.Jev = append(p.Jev, evaluation.JevRow{
			Label: labelVersionText(j.LabelVersion), N: j.HitRate.N,
			HitRate: metricCell(j.HitRate, percent), Brier: metricCell(j.Brier, plain3),
		})
	}
	for _, m := range e.Meta {
		p.Meta = append(p.Meta, metaRow(m))
	}
	return p
}

func statsRow(key string, s actuals.Stats) evaluation.Row {
	return evaluation.Row{
		Key: key, N: s.N, NetPnL: yen(s.NetPnLJPY),
		WinRate: metricCell(s.WinRate, percent), EVJPY: metricCell(s.EVJPY, yenFloat),
		EVBps: metricCell(s.EVBps, plain2), ProfitFactor: metricCell(s.ProfitFactor, plain2),
		DDJPY: metricCell(s.MaxDrawdownJPY, yenFloat), DDPct: metricCell(s.MaxDrawdownPct, percentPoints),
	}
}

func metaRow(m actuals.MetaComparison) evaluation.MetaRow {
	oos := func(v *float64, f func(float64) string) evaluation.Cell {
		switch {
		case v == nil:
			return evaluation.Cell{Text: noValue, Title: "OOSでは定義できない（NULL）"}
		case !m.OOSReliable:
			return evaluation.Cell{Text: undetermined, Undetermined: true, Title: fmt.Sprintf("参考値 %s（OOS n=%d）", f(*v), m.OOSSamples)}
		}
		return evaluation.Cell{Text: f(*v), Title: fmt.Sprintf("OOS n=%d", m.OOSSamples)}
	}
	row := evaluation.MetaRow{
		Version: m.Version, Status: m.Status,
		OOSWindow:  m.OOSStart.In(jst).Format("2006-01-02") + "〜" + m.OOSEnd.In(jst).Format("2006-01-02"),
		OOSSamples: fmt.Sprintf("%d", m.OOSSamples),
		OOSAUC:     evaluation.Cell{Text: noValue}, OOSEV: evaluation.Cell{Text: noValue},
		LinkedN: m.Linked.N, WindowN: m.InOOSWindow.N,
		PredictedEV: metricCell(m.PredictedEVBps, plain2), RealizedEV: metricCell(m.Linked.EVBps, plain2),
		EVGap: metricCell(m.EVGapBps, plain2), WindowEV: metricCell(m.InOOSWindow.EVBps, plain2),
	}
	row.OOSAUC = oos(m.OOSAUC, plain3)
	row.OOSEV = oos(m.OOSExpectedBps, plain2)
	return row
}

func checkView(c actuals.Check, crit actuals.Criteria) evaluation.Check {
	v := evaluation.Check{ID: c.Key, Status: string(c.Status), StatusText: statusText(c.Status), Actual: noValue}
	switch c.Key {
	case actuals.CheckPeriodDays:
		v.Label, v.Required = "取引のあった日数", fmt.Sprintf("%d 日以上", crit.MinDays)
		v.Actual = fmt.Sprintf("%.0f 日", c.Actual)
	case actuals.CheckTrades:
		v.Label, v.Required = "クローズ済み取引件数", fmt.Sprintf("%d 件以上", crit.MinTrades)
		v.Actual = fmt.Sprintf("%.0f 件", c.Actual)
	case actuals.CheckEVLowerBound:
		v.Label, v.Required = "EV(bps/取引)の95%下側信頼限界", fmt.Sprintf("%s bps を上回る", plain2(c.Required))
		if c.HasActual {
			v.Actual = plain2(c.Actual) + " bps"
		}
	case actuals.CheckMaxDrawdown:
		v.Label, v.Required = "最大ドローダウン（初期資金比）", fmt.Sprintf("%s%% 以下", plain2(c.Required))
		if c.HasActual {
			v.Actual = plain2(c.Actual) + "%"
		}
	}
	return v
}

func criteriaText(c actuals.Criteria) string {
	return fmt.Sprintf("合格ライン: 取引のあった日が%d日以上・取引%d件以上・EV下側信頼限界が%s bps超・最大ドローダウンが初期資金の%s%%以下。"+
		"指標はn≥%d かつ %d日以上にまたがるときだけ判定する。",
		c.MinDays, c.MinTrades, plain2(c.MinEVLowerBps), plain2(c.MaxDrawdownPct), c.MinSample, c.MinClusters)
}

func statusText(s actuals.Status) string {
	switch s {
	case actuals.StatusPass:
		return "合格"
	case actuals.StatusFail:
		return "不合格"
	}
	return undetermined
}

func labelVersionText(v int) string {
	switch v {
	case 1:
		return "label_version 1（終値のみ）"
	case 2:
		return "label_version 2（トリプルバリア・コスト控除後）"
	}
	return fmt.Sprintf("label_version %d", v)
}

// metricCell renders m with f: the value with its 95% interval when it is
// reliable, otherwise 判定不能 with the reference value and n as tooltip.
func metricCell(m actuals.Metric, f func(float64) string) evaluation.Cell {
	if m.Reliable {
		return evaluation.Cell{Text: fmt.Sprintf("%s [%s–%s]", f(m.Value), f(m.Lo), f(m.Hi)), Title: fmt.Sprintf("n=%d", m.N)}
	}
	c := evaluation.Cell{Text: undetermined, Undetermined: true, Title: fmt.Sprintf("n=%d", m.N)}
	if m.Defined {
		c.Title = fmt.Sprintf("参考値 %s（n=%d", f(m.Value), m.N)
		if m.HasCI {
			c.Title += fmt.Sprintf("、区間 [%s–%s]", f(m.Lo), f(m.Hi))
		}
		c.Title += "）: サンプルまたは取引日数が基準未満のため判定不能"
	}
	return c
}

func percent(v float64) string       { return fmt.Sprintf("%.1f%%", v*100) }
func percentPoints(v float64) string { return fmt.Sprintf("%.2f%%", v) }
func plain2(v float64) string        { return fmt.Sprintf("%.2f", v) }
func plain3(v float64) string        { return fmt.Sprintf("%.3f", v) }
func yenFloat(v float64) string      { return yen(v) }

// yen renders a currency amount as whole yen with thousands separators,
// e.g. -¥1,234.
func yen(v float64) string {
	s := fmt.Sprintf("%.0f", v)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	if s == "0" {
		sign = "" // "-0" from a tiny negative amount
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return sign + "¥" + s
}
