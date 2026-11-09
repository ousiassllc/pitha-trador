package report

import (
	"fmt"
	"strings"
)

func replaySection(b *strings.Builder, s ReplaySection) {
	ok := layerHeader(b, "表 2: 再生層（(iii) 実際にエントリーされたもの・資金とリスクあり・約定と出口の円）", s.Available, s.Reason)
	w := func(format string, args ...any) { fmt.Fprintf(b, format+"\n", args...) }
	if len(s.UnavailablePeriods) > 0 {
		w("### 再生層が実行不可の期間（ラベル層は同じ期間も実行している）")
		w("")
		w("| 期間 | 理由 |")
		w("|---|---|")
		for _, p := range s.UnavailablePeriods {
			w("| %s 〜 %s | %s |", p.From, p.To, p.Reason)
		}
		w("")
	}
	if !ok {
		return
	}
	w("主評価は全セル同一の初期状態から時系列に連続して再生する（資金・連敗を日ごとに初期化しない。エンジンが実際に行う日次リセットのみ）。損益と DD は経路依存なので判断 ID の差では評価せず、日次損益の差（To − From）を日を単位とした再抽出で評価する。最大 DD は日次資産曲線上の値の記述で、その差の区間は参考（日数が少ないと信頼できない）。欠測で状態を引き継げない場合は、全セル共通の区間に分けて区間別に示し、区間どうしを一つの資産曲線に結合しない。")
	w("")
	for _, seg := range s.Segments {
		w("### 区間 %s（%s 〜 %s、%d 日、初期資金 %.0f 円・連敗 %d）", seg.Name, seg.From, seg.To, seg.Days, seg.InitialState.CapitalJPY, seg.InitialState.ConsecutiveLosses)
		w("")
		w("| セル | 適格候補 | entry | 損益(円) | 資金比 | 終了資産(円) | 最大DD(円) | 最大DD(資金比) | 日次平均(円) | 最良日 | 最悪日 | プラスの日 |")
		w("|---|---|---|---|---|---|---|---|---|---|---|---|")
		for _, c := range seg.Cells {
			w("| %s | %d | %d | %.0f | %.4f | %.0f | %.0f | %.4f | %.0f | %.0f | %.0f | %d/%d |", c.Name, c.EligibleCandidates, c.Entries, c.PnLJPY, c.ReturnOnCapital, c.EndEquityJPY, c.MaxDrawdownJPY, c.MaxDrawdownRatio, c.Daily.MeanJPY, c.Daily.BestDayJPY, c.Daily.WorstDayJPY, c.Daily.PositiveDays, c.Daily.Days)
		}
		w("")
		w("段階ごとの件数（最初に落ちた段）:")
		w("")
		for _, c := range seg.Cells {
			parts := make([]string, 0, len(c.Stages))
			for _, st := range c.Stages {
				parts = append(parts, fmt.Sprintf("%s %d", st.Stage, st.Count))
			}
			w("- %s: %s", c.Name, strings.Join(parts, " → "))
		}
		w("")
		b.WriteString(resultHeader)
		for _, r := range seg.Differences {
			resultRow(b, r, "")
		}
		w("")
	}
	if ref := s.DailyResetReference; ref != nil {
		w("### 参考: 日次独立リセットの実験（継続資産曲線・DD ではない）")
		w("")
		w("%s", ref.Description)
		w("")
		w("| セル | entry | 損益(円) | 資金比 |")
		w("|---|---|---|---|")
		for _, c := range ref.Cells {
			w("| %s | %d | %.0f | %.4f |", c.Name, c.Entries, c.PnLJPY, c.ReturnOnCapital)
		}
		w("")
	}
}
