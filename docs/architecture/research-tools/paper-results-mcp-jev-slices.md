# 設計: Paper 実績 MCP の Jev 評価の切り分けツール

`docs/architecture/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1089（前半: 切り分け）。ツールの引数と出力は `docs/api/endpoints/research-tools/paper-results-mcp-jev-slices.md`。母集団・ラベルは `docs/architecture/paper-results-mcp-jev-evaluation.md`（#1073）と同じ。

## 1. 位置づけ

`jev_evaluation_slices` は、全判断の評価（`jev_evaluation`）の llm アームが全体でマイナスでも、特定の地合い・時間帯・方向・確信度・銘柄の特徴ではプラスかもしれない、という問いを**記述的に**見るための読み取り専用のツールである。結果は採用・昇格・取引の判断に使わない（しきい値・合否・推奨・「最良のバケット」を持たない）。

- 読む表: `jev_evaluation` と同じ。追加で `jev_decisions` の `market_regime`・`regime_volatility_bps`・`confidence`、判断の足の `market_snapshots.spread_bps` / `turnover_5m`、`instruments.sector`（いずれも `evaldiag` の判断の読み込みの SELECT に載せたもの。本文・JSON の列は読まない）
- 書くもの: 無い。MCP の読み取り接続の読み取りトランザクションの中だけで動く。研究用テーブルにも書かない
- 取引への影響: 無い。Jev・Risk・Policy・注文・Paper ガード・資金・既存のシャドー・メタモデルの学習・既存の表は読みも変えもしない。出力を他の経路へ渡さない
- 実装: `internal/service/paperresultsmcp/slicediag`（切り口・集計・補正・文言）。母集団は `evaldiag.Rows`、統計は `evaldiag.Summarize` と `internal/service/metaablation/stats`（`Holm`）。登録は `research_tools.go`。テストは `slicetests`

## 2. 同じ母集団・同じラベル

`evaldiag.Rows` が返す行（`jev_evaluation` の比較に使える行そのもの。同じ除外・cutoff・付け直したラベル・コホートの分け方）を、コホートごとに切る。**ラベルや母集団をここで作り直さない**ので、`all`（コホート全体）は `jev_evaluation` の llm の `all` と同じ数字になる（`slicetests` が確かめる）。コホート数・判断数の上限も `evaldiag` のもの（`limit_exceeded`）。

## 3. 切り口の登録（事前登録）

切り口はコード（`slicediag/dimensions.go` の `dimensions`）に固定した表で持ち、出力の順もその表の順。**`preregistered`（`regime`・`time_of_day`・`direction`・`confidence_band`）は 2026-11-15、issue #1089 で、結果を見る前に固定した。** バケットの境界（時間帯・確信度の帯）もその日の固定値で、結果を見て動かさない。`exploratory`（`weekday`・`volatility_band`・`turnover_band`・`spread_band`・`sector`）は結果を見た後に足した切り口で、出力の `exploratory_note` に「仮説であり確認ではない」と固定文で出す。切り口を足すときは `exploratory` に入れ、`preregistered` へは移さない（事後に事前登録にはできない）。

- 時間帯: JST の判断時刻の半開区間 `[開始, 終了)`。昼休み（11:30–12:30）・時間外は `other`
- 確信度: 下端を含む固定の帯。範囲外・NULL は `unknown`
- 曜日: JST。土日は `other`（実運用では空）
- 三分位（ボラティリティ・売買代金・スプレッド）: コホートの評価行のうち値のある行を値の昇順・同値は判断 id の昇順に並べ、`i×3÷k` で段に入れる（ランクに基づき決定的。同値が境界をまたぐと同じ値が 2 つのバケットに入りうる）。NULL は `unknown`。境界はデータから決まるので、`edges` に出す
- 業種: 行数の多い上位 20（同数は名前順）、残りは `other`、NULL は `unknown`。バケットの並びは名前順（行数や結果で並べない）
- どの切り口も、行を**ちょうど 1 つのバケットに**入れる（合計が `evaluated` に一致する）

## 4. 統計と多重比較

- バケットごとの要約は `evaldiag.Summarize`（日クラスタのブートストラップ・シード 1073・2000 回・95% 百分位区間・検出できる最小の差・勝率・両側 p 値）。2 日未満は区間・p なし
- `low_sample`: `n < 30`（`MinBucketRows`）または `days < 5`（`MinBucketDays`）。理由を `low_sample_reasons` に出す。バケットは除外せず、family に入れたまま印だけ付ける（印の付いたバケットを黙って落とすと補正が甘くなる）
- 多重比較: `stats.Holm` を、コホートごと・family（`preregistered` / `exploratory`）ごとに、その family の**全切り口の全バケット**のうち p 値のあるものに適用する。`dimensions` で出力を絞っても family 全体で計算する（見る切り口を絞って補正を甘くできないように）。`family_size` が Holm の m。`all` は family に入れない。補正後の p は元の p 以上
- 並べ替えない・最良を示さない・しきい値を当てない・推奨を返さない。補正後の p も記述であり、合否ではない
- 計算量: 1 コホートで最大 60 前後のブートストラップ（2000 回 × 日数）。判断数の上限（`evaldiag.LimitFor`）で日数も縛られ、コンテキストのキャンセルをバケットごとに見る

## 5. 触れないもの

取引・Risk・発注・Paper ガード・資金・Policy の閾値・ライブの Jev・既存のシャドー・メタラベリングの学習・既存の表・反実仮想ラベルは読みも変えもしない。日次の記録・推移（#1089 の後半）は別ツール・別ドキュメント。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1089 |
