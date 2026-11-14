# 設計: Paper 実績 MCP の Jev 評価ツール

`docs/architecture/paper-results-mcp.md` §5 の分割章（行数上限のため別ファイル）。issue #1073。ツールの引数と出力は `docs/api/endpoints/paper-results-mcp-jev-evaluation.md` §3.33。

## 1. 位置づけ

`jev_evaluation` は、Jev（AI）の LONG/SHORT 判断を**通過・却下に関係なく全件**、同じ行の単純なベースラインと並べて見る読み取り専用のツールである。既存の Calibration 画面・メタモデルの学習は、エントリー判定や Risk を通った判断（または学習対象の行）の結果を見ているので、「AI が良いか」は通過後の数字では答えられない。このツールは通過前の全判断で答える。

- 読む表: `jev_decisions`（全 trader の LONG/SHORT 判断）、`market_snapshots`（ラベルの価格経路・判断の足の `return_5m`）、`trade_signals`（`risk_passed=1` の有無だけ。entered / skipped の分割）、`calibration_outcomes`（保存済みラベルとの突き合わせのみ）、`runtime_settings`（`calibration.barrier_*`・`cost.*`・`metalabel.*` の固定キーを 1 キーずつ）
- 読まない表: `counterfactual_labels`・`calibration_outcome_origins`（`internal/repository/counterlabel/isolation_test.go` の許可リストを増やさない）、トライアルの表
- 書くもの: 無い。MCP の読み取り接続（`mode=ro` + `query_only`）の読み取りトランザクションの中だけで動き、書き込み用の接続を開かない。MCP の書き込みホワイトリスト（`mcpwritesettings`）も変えない
- 取引への影響: 無い。Jev・Risk・Policy・注文・Paper ガード・資金・既存のシャドー（`jev_shadow*`）・メタラベリングの学習・`calibration_outcomes` / `counterfactual_labels` の中身・メタモデルは、読みも変えもしない。出力を他の経路へ渡さない
- 実装: `internal/service/paperresultsmcp/evaldiag`（引数・読み込み・集計）、登録は `internal/service/paperresultsmcp/jev_models_tool.go`（`queryTool`）、ラベルは `internal/service/calibration.LabelWindow`、統計は `internal/service/metaablation/stats`。テストは `evaltests`・`evaldiag`・`calibration/labelwindowtests`

## 2. ラベルは既存ラベラーと同じ関数

結果ラベルは `calibration.LabelWindow`（`Labeler.Compute` から取り出した純粋関数）で付ける。`Compute` もこれを呼ぶので、エントリーの探索（`FindEntry`。応答から `EntryWaitTolerance` 以内の最初の約定できる気配。買いは ask、売りは bid）・経路の切り詰め・ホライズンの被覆の確認（`HorizonBarTolerance`）・`labelPath`（triple barrier）は**同じコード**で、挙動は変わらない（`labelwindowtests` が `Compute` と `LabelWindow` の一致を確かめる）。

MCP 側は、判断ごとの窓（`market_snapshots` の `[応答時刻, 応答時刻 + EntryWaitTolerance + ホライズン + 1 秒)`。応答時刻は `ResponseTime` と同じ「`created_at` と `timestamp` の遅い方」）を読み取りの 1 本の結合で順に読み、`domain.Snapshot`（時刻・価格・bid・ask）を作って `LabelWindow` に渡す。バリアの幅とコストは `barriersettings.Load` と `costsettings.LabelBarriers`（方向ごと）で読み取り専用の設定の窓口から取り、`Validate` も `Labeler` と同じく通す。

**公平な比較（全アームを付け直す）**: 保存済みラベルは作成時の設定で作られているので、設定を変えた後は行ごとに設定が違う。そこで llm も買い・売りの脚もベースラインも、**いまの設定で同じ関数で付け直し**、同じ設定の数字だけを並べる。保存済みラベルとの差は `stored_label` に別に出す（設定が同じなら 0）。

## 3. 同じ行のベースライン

| アーム | net の作り方 |
|---|---|
| `llm` | 判断自身の方向のラベル |
| `long_leg` / `short_leg` | 同じ判断で買い / 売りのラベル |
| `momentum` | 判断の足（同じ `instrument_id`・`timestamp` の `market_snapshots`）の `return_5m` の符号で買い / 売り。0 または NULL は見送り（0 bps） |
| `contrarian` | `momentum` の逆 |
| `none` | 0 bps |
| `random` | 買いと売りの net の平均（期待値）。乱数を使わないので決定的 |

判断が比較の行に入るのは、llm・買い・売りのラベルがすべて付けられ、判断の足の行がある場合だけ（全アームが同じ行になる）。付けられない判断は黙って落とさず、理由別に数える（`llm_no_entry_quote` / `llm_window_short` / `opposite_leg_no_entry_quote` / `opposite_leg_window_short` / `no_bar_for_momentum`）。`NONE` の判断は対象外（方向がない）。

## 4. 版・モデルの分割と cutoff

- `(model_id, question_version)` ごとに完全な結果ブロックを持つ。版やモデルが変わると別のブロックになり、件数は数え直しになる（最初・最後の判断の日付つき）。上限は 50 組
- LLM の評価は `metalabel.llm_cutoff` の日（JST の日の始まり）以降の判断だけ。それより前の判断は `excluded_before_cutoff` に数え、ベースラインも同じ行なので同じく除く。cutoff が仮置き（`placeholder`）のときは除外せず、`llm_cutoff.applied=false` と `caveats` で知らせる（LLM がその期間の値動きを知っていた可能性がある）。cutoff の値・状態（`CutoffStatus`）・確認したモデルを出力に出す。設定の読み取りは `metalabelsettings.Load` で、`metalabel.*` のキーだけを許す窓口を通す

## 5. 統計（記述的。採用基準ではない）

- 各アーム・各方向（全体 / 買い / 売り）の平均 net bps は、判断の JST 日を単位にした**日クラスタのブートストラップ**（`stats.Bootstrap`。固定のシード 1073・2000 回・95% 百分位区間）。2 日未満は区間なし
- llm とベースラインの差は同じ行ごとに引き、日ごとの差の合計を日クラスタとして同じ手順で出す
- 検出できる最小の差 = `stats.DetectableMinimum(alpha=0.05, power=0.8, 標準誤差)` = (z₍0.975₎ + z₍0.8₎) × ブートストラップの標準誤差。メタアブレーションの判定（`stats.Decide`）と同じ関数だが、alpha と power はこのツールの固定の記述用の定数で、採用のしきい値ではない（`stats` パッケージの「採用基準を持たない」ガードは変えない）
- `days_to_detect`: 標準誤差が日数の平方根に反比例して縮む前提で、+3 / +5 bps を検出できるまでの追加の JST 営業日数 = max(0, ceil(日数 × (最小値 / target)² − 日数))。最小値が target 以下なら 0 で `already_detectable=true`、標準誤差が未定義なら `null`
- どの数字にもしきい値は当てず、勝者・推奨・合否の項目は無い

## 6. 読み取りの上限

期間内の LONG/SHORT trader 判断は 20,000 件（`evaldiag.MaxDecisions`）まで、(`model_id`, `question_version`) は 50 組まで。超えたら部分結果を返さず `limit_exceeded`（422）にする。判断ごとの窓は結合 1 本でストリームに読み、判断の数に比例した SQL 文は出さない（3 秒の `query_timeout` の中で 20,000 件が終わることを確かめてある）。

## 7. 触れないもの

取引・Risk・発注・Paper ガード・資金・Policy の閾値・ライブの Jev・既存のシャドー・RAG・メタラベリングの学習・較正の保存ラベル・反実仮想ラベル・メタモデルは読みも変えもしない。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-14 | 新規 | issue #1073 |
