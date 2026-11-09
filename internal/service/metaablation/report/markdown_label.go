package report

import (
	"fmt"
	"strings"
)

func labelSection(b *strings.Builder, s LabelSection) {
	if !layerHeader(b, "表 1: ラベル層（(ii) 独立な行の適格候補・proxy ラベル・Risk と資金なし）", s.Available, s.Reason) {
		return
	}
	r := s.Result
	w := func(format string, args ...any) { fmt.Fprintf(b, format+"\n", args...) }
	w("- %s", r.Boundary)
	w("- 行の分類: 独立 %d / 参考（元 OOS）%d / 除外 %d", r.Independence.Independent, r.Independence.Reference, r.Independence.Excluded)
	for _, reason := range sortedKeys(r.Independence.ByReason) {
		w("  - %s: %d", reason, r.Independence.ByReason[reason])
	}
	if r.PopulationLimit != "" {
		w("- **母集団の制限**: %s", r.PopulationLimit)
	}
	w("- 日数 %d（取引日ごとに再抽出する）", len(r.Days))
	w("")
	w("### セル別（net bps。1 適格候補あたりは skip を 0 とし、1 take あたりと別の列）")
	w("")
	w("| セル | 適格候補 | take | M の skip | 合計 bps | 1 適格候補あたり bps | 1 take あたり bps |")
	w("|---|---|---|---|---|---|---|")
	for _, c := range r.Cells {
		perTake := "なし"
		if c.PerTakeBps != nil {
			perTake = fmt.Sprintf("%.3f", *c.PerTakeBps)
		}
		w("| %s | %d | %d | %d | %.2f | %.3f | %s |", c.Name, c.EligibleCandidates, c.Takes, c.MetaSkips, c.SumNetBps, c.PerEligibleBps, perTake)
	}
	w("")
	w("### 段階ごとの件数（実際の適用順・最初に落ちた段で数える）")
	w("")
	w("| セル | 判断 | ラベル | 復元不能(unknown) | 適格条件 | Q | continuation/toxic/liquidity | M | 通過(take) |")
	w("|---|---|---|---|---|---|---|---|---|")
	for _, c := range r.Cells {
		d := map[string]int{}
		for _, x := range c.Funnel.Drops {
			d[x.Stage] = x.Count
		}
		w("| %s | %d | %d | %d | %d | %d | %d | %d | %d |", c.Name, d["decision"], d["labeled"], c.Funnel.Unknown, d["eligibility"], d["quality"], d["flow_guards"], c.MetaSkips, c.Takes)
	}
	w("")
	w("Q は M より前の段なので、Q off のセルでは entry_quality で落ちていた行が後ろの段に進み、段ごとの内訳が Q on と変わる。復元不能な行は 4 セルから共通に除外している。")
	w("")
	w("### 判断 ID で対にした差（1 適格候補あたり bps、日を単位とした再抽出の区間）")
	w("")
	b.WriteString(resultHeader)
	for _, c := range r.Comparisons {
		resultRow(b, c.Result, "bps")
	}
	w("")
	w("### 別掲: VWAP 逆張りの候補（Q の差に含めない）")
	w("")
	w("行数 %d、M の段まで進んだもの %d（モデルの take %d / skip %d）。Q の対象外で、ラベルは Jev の方向のものであり逆張りの側の損益ではないため、セルの平均にも含めない。", r.Reversion.Rows, r.Reversion.ReachedMeta, r.Reversion.ModelTakes, r.Reversion.ModelSkips)
	w("")
	w("### (i) 元のモデル評価（参考・独立ではない）")
	w("")
	ref := r.Reference
	rowAvg, takeAvg := "なし", "なし"
	if ref.MeanNetBpsPerRow != nil {
		rowAvg = fmt.Sprintf("%.3f", *ref.MeanNetBpsPerRow)
	}
	if ref.MeanNetBpsPerTake != nil {
		takeAvg = fmt.Sprintf("%.3f", *ref.MeanNetBpsPerTake)
	}
	w("元 OOS %d 行を保存済みモデルで再採点（再採点できた行 %d、不完全 %d）。take %d、1 行あたり %s bps、1 take あたり %s bps。ガード前の母集団で、表 1 とは別。優劣・採用の根拠にしない。", ref.Rows, ref.Rescored, ref.Incomplete, ref.Takes, rowAvg, takeAvg)
	w("")
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
