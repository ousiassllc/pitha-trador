package report

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
)

// Markdown renders the document for a reader.
func Markdown(d Document) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	w("# %s", d.Title)
	w("")
	w("研究用の集計。取引・発注・モデルの昇格・設定変更は行わない。%s", d.Approval)
	w("")
	w("## 読み方")
	w("")
	w("### 母集団は 3 段あり、混ぜない")
	w("")
	w("| | 母集団 | 損益 | 資金・リスク |")
	w("|---|---|---|---|")
	for _, p := range d.Populations {
		w("| %s | %s | %s | %s |", p.Name, p.Population, p.Outcome, p.CapitalRisk)
	}
	w("")
	w("- %s", d.ProxyCaveat)
	w("- **主比較**は一つの軸だけが違う 4 組（To − From）:")
	for _, p := range d.PrimaryPairs {
		w("  - %s", p)
	}
	w("- %s", d.DiagonalNote)
	w("- **%s**", d.VerdictMeaning)
	for _, s := range d.Statements {
		w("- %s", s)
	}
	w("")
	manifestSection(&b, d)
	labelSection(&b, d.Label)
	replaySection(&b, d.Replay)
	return b.String()
}

func manifestSection(b *strings.Builder, d Document) {
	m := d.Manifest
	w := func(format string, args ...any) { fmt.Fprintf(b, format+"\n", args...) }
	w("## 実行情報（manifest）")
	w("")
	w("- **実行回数**: %d（この実行を含む。評価窓の結果を見て閾値などを変えるときは別の実行にし、先の実行も残す）", d.RunCount)
	for _, p := range m.Run.Previous {
		w("  - 先の実行 %d: manifest %s（固定 %s）理由: %s", p.Count, p.SHA256, p.FrozenAt.Format("2006-01-02 15:04 MST"), p.ChangeReason)
	}
	if m.Frozen != nil {
		w("- manifest の固定: %s（SHA-256 %s）", m.Frozen.At.Format("2006-01-02 15:04 MST"), m.Frozen.SHA256)
	}
	w("- 対象モデル: %s（パラメータ SHA-256 %s。本体は出力しない）", m.Model.Version, m.Model.ParamsSHA256)
	w("- 評価対象のコホート / Jev の版（manifest で固定。別の行は除外）: %s", m.Cohort)
	w("- DB の写し: SHA-256 %s / as-of %s / コードの版 %s", m.DBCopy.SHA256, m.DBCopy.AsOf.Format("2006-01-02 15:04 MST"), m.CodeVersion)
	win := m.Windows
	w("- 評価窓 [%s, %s)、embargo %v、LabelEnd（モデルが使った最後）%s", win.EvalFrom.Format("2006-01-02 15:04"), win.EvalTo.Format("2006-01-02 15:04"), win.Embargo(), win.LabelEnd.Format("2006-01-02 15:04"))
	w("- 事前登録の規則: Q on = entry_quality >= %s（固定）、最低 EV %.2f bps、continuation/toxic_flow/liquidity_stressed の値は manifest のとおり（全セル共通）", m.Rules.QualityCut, m.Rules.MinEVBps)
	r := m.Rules.Replay
	w("- 再生層の資金・リスク・コスト（全セル共通）: 初期資金 %.0f 円、1 銘柄上限 %.0f 円、総エクスポージャ上限 %.0f 円、fill %s（spread %.1f / slippage %.1f / 手数料 %.1f bps）", r.InitialCapitalJPY, r.MaxPositionPerSymbolJPY, r.MaxTotalExposureJPY, r.FillModel, r.SpreadBps, r.SlippageBps, r.FeeBps)
	st := m.Statistics
	w("- 検定: %s、seed %d、再抽出 %d 回、有意水準 %.3f、補正 %s", st.Method, st.Seed, st.Resamples, st.Alpha, st.Correction)
	for _, lm := range st.PrimaryMetrics {
		w("  - 主要指標（%s 層）: %s", lm.Layer, lm.Metric)
	}
	mc := d.Multiple
	w("- 多重比較: 事前登録した全比較数 %d、実行した比較数 %d（各層の主比較 %d 組。探索を含む）、補正後の水準 %.5f", mc.Registered, mc.Executed, mc.Primary, mc.AlphaAdjusted)
	if mc.FamilyExceeded {
		w("- **比較数が事前登録を超えた**: %s", mc.FamilyExceededReason)
	}
	if len(d.UnsetMinimums) > 0 {
		w("- **未設定の事前登録値: %s** — 提案のレビューが済むまで、すべての判定は優劣未確定。", strings.Join(d.UnsetMinimums, "、"))
	} else {
		mm := m.Minimums
		w("- 事前登録の最小: 日数 %d、件数 %d、検出力 %.2f、δ（ラベル）%.2f bps、δ（再生）%.4f（日次リターン）", *mm.MinDays, *mm.MinCount, *mm.Power, *mm.DeltaLabelBps, *mm.DeltaReplayDailyReturn)
	}
	w("- 初期状態・区間の規則: 初期資金 %.0f 円・連敗 %d。%s", m.Replay.InitialState.CapitalJPY, m.Replay.InitialState.ConsecutiveLosses, m.Replay.SegmentRule)
	w("")
}

// resultRow is one line of a comparison table.
func resultRow(b *strings.Builder, r stats.Result, unit string) {
	ci, mde := "なし", "なし"
	if r.Estimate.CI != nil {
		ci = fmt.Sprintf("[%.3f, %.3f]", r.Estimate.CI.Lower, r.Estimate.CI.Upper)
	}
	if r.MDE != nil {
		mde = fmt.Sprintf("%.3f", *r.MDE)
	}
	verdict := string(r.Verdict)
	if r.Reference {
		verdict = "参考（判定なし）"
	}
	reasons := strings.Join(r.Reasons, ", ")
	c := r.Counts
	fmt.Fprintf(b, "| %s | %.3f %s | %s | %s | %d | %d | %d/%d | %d | %d (%s) | %s | %s |\n",
		r.Name, r.Estimate.Mean, unit, ci, mde, c.Days, c.EligibleCandidates, c.TakesFrom, c.TakesTo, c.EntriesTo, c.Discordant, c.BasisName, verdict, reasons)
}

const resultHeader = "| 比較 (To − From) | 平均差 | 区間 | 検出可能な最小の差 | 日数 | 適格候補 | take (From/To) | entry (To) | 対の不一致 | 判定 | 理由 |\n|---|---|---|---|---|---|---|---|---|---|---|\n"

func layerHeader(b *strings.Builder, title string, available bool, reason string) bool {
	fmt.Fprintf(b, "## %s\n\n", title)
	if !available {
		fmt.Fprintf(b, "**実行不可**: %s\n\n", reason)
	}
	return available
}
