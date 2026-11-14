# API 仕様: Paper 実績 MCP の Jev 評価ツール

`docs/api/endpoints/paper-results-mcp.md` §3.33 の分割章（行数上限のため別ファイル）。issue #1073。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。**読み取り専用**（`readOnlyHint: true`・`destructiveHint: false`。何も書かず、設定も変えない）。設計は `docs/architecture/paper-results-mcp-jev-evaluation.md`。

## 3. ツール

### 3.33 `jev_evaluation`

Jev（AI）が良いのかどうかを、**エントリー判定（entry_quality）や Risk を通ったかどうかに関係なく**、期間内の `decision_type=trader` かつ `direction` が `LONG` / `SHORT` の**全判断**で、同じ行の単純なベースラインと並べて見る。結果ラベルは既存ラベラーと同じ定義（コスト控除後の triple barrier の `net_return_bps`）を、いまの設定で全アームに同じ式で付け直す。

**位置づけ**: 研究用の読み取りの集計。採用・昇格の基準ではなく、しきい値も当てない（出力の `notice` に固定文で出る）。「検出できる最小の差」などは alpha=0.05 両側・検出力 0.8 の固定値での記述的な数字。Jev・Risk・Policy・注文・Paper ガード・メタモデルの学習・既存のシャドー・`calibration_outcomes` / `counterfactual_labels` の中身は変えず、読むのは `jev_decisions`・`market_snapshots`・`trade_signals`・`calibration_outcomes`・`runtime_settings` の固定キーだけ（`counterfactual_labels` は読まない）。

引数:

| 引数 | 内容 |
|---|---|
| `from` / `to` | 他のツールと同じ期間（JST の暦日、`[from, to)`、最大 366 日、既定は直近 30 日）。判断の時刻（`jev_decisions.timestamp`）で切る |
| `horizon_minutes` | 任意。ラベルの保有時間（分、1〜60）。省略は**いまの `calibration.barrier_timeout_minutes`**。保存済みラベルとの突き合わせも同じホライズンで行う |
| `prompt_version` | 任意。この版（`question_version`。例 `trader-v4`）の判断だけ。省略は全版（版ごとに別集計） |
| `model_id` | 任意。この `model_id` の判断だけ（100 文字まで）。省略は全モデル（モデルごとに別集計） |

不明な引数・型の違い・範囲外（`horizon_minutes` が 1〜60 でない、`prompt_version` の形が不正）は `invalid_argument`（422）、期間の誤りは `invalid_period`（422）。期間内の判断が 10,000 件（`MaxDecisions`）を超える（`horizon_minutes` が長いほど窓の行が増えるため、窓の行数の上限 `MaxWindowRows` = 200,000 から `200,000 ÷ (horizon_minutes + 4)` 件に下がる。ホライズン 60 なら 3,125 件）、または (`model_id`, `question_version`) の組が 50（`MaxCohorts`）を超えると、部分結果を返さず `limit_exceeded`（422。期間か `model_id` / `prompt_version` で狭める）。

#### 応答の `data`

| キー | 内容 |
|---|---|
| `from` / `to` | 解決した期間（JST の日付） |
| `horizon_minutes` | 使ったホライズン |
| `prompt_version` / `model_id` | 渡した絞り込み（渡さなければ項目なし） |
| `notice` | 固定の注意書き（研究用・採用基準ではない・しきい値を当てない・何も書かない） |
| `decisions` | 読んだ判断の数（`trader` かつ LONG/SHORT。`NONE`・`scout`・期間外は含まない） |
| `llm_cutoff` | `date`（`metalabel.llm_cutoff`）・`status`（`placeholder` / `unverified` / `confirmed`）・`model`（`metalabel.llm_cutoff_model`）・`applied`。`applied=false`（`placeholder`）のときは**何も除外せず**、`caveats` に「学習データに含まれうる日も除外していない」と出す |
| `label_assumptions` | `label_version`（2）・`horizon_minutes`・`take_profit_bps`・`stop_loss_bps`・`barrier_cost_bps`・`label_cost_bps`（`long` / `short`。`cost.*` で動かした後の値）・`settings`（いまの設定で付け直した旨。保存済みラベルは作成時の設定で作られている） |
| `statistics` | `method`・`alpha`（0.05）・`power`（0.8）・`resamples`（2000）・`seed`（1073。固定で、同じ入力なら同じ結果）・`targets_bps`（`[3, 5]`） |
| `cohorts[]` | `(model_id, question_version)` ごとの結果ブロック（下記）。最初の判断の時刻の順 |
| `definitions` | アーム・分割・勝率・差・`days_to_detect`・`stored_label` などの定義文（出力の中にも書く） |
| `caveats[]` | 数字の限界（設定の差・検出できる最小の差は記述的・日クラスタの区間でも楽観的になりうる・random は期待値・entered は約定ではない・cutoff が仮置き） |

**`cohorts[]` の要素**（版が変わると別の要素になるので、数え直しになる）:

| キー | 内容 |
|---|---|
| `model_id` / `question_version` | コホートのキー |
| `first_decision_day` / `last_decision_day` | コホートの最初・最後の判断の JST 日 |
| `decisions` | コホートの全 LONG/SHORT trader 判断（`excluded_before_cutoff` + `excluded_by_reason` の合計 + `evaluated`） |
| `excluded_before_cutoff` | cutoff の日（JST の日の始まり）より前で、LLM の評価から除く判断。ベースラインも同じ行なので同じく除く |
| `excluded_by_reason[]` | 全アームの共通の行にできない判断の理由別の件数（0 件も出す）: `llm_no_entry_quote`（判断の方向の約定できる気配が 3 分以内にない）・`llm_window_short`（データがホライズンに届かない）・`opposite_leg_no_entry_quote` / `opposite_leg_window_short`（逆向きのラベルが付かない）・`no_bar_for_momentum`（判断の時刻の足がない） |
| `evaluated` | 比べた判断の数。全アームが**同じ行**で集計される |
| `stored_label` | 保存済みラベルとの突き合わせ（下記） |
| `arms[]` | アームごと（`llm`, `random`, `momentum`, `contrarian`, `none`, `long_leg`, `short_leg`）。各要素は `arm` と、`all` / `long` / `short`（`Metrics`。long / short は**判断の方向**で分ける。ベースラインも同じ行の集合） |
| `llm_minus_baseline[]` | `baseline`（上の `llm` 以外の 6 つ）ごとに `all` / `long` / `short`（`Diff`） |
| `llm_by_entry[]` | llm アームを `entered`（`trade_signals` に `risk_passed=1` の行がある判断）と `skipped`（それ以外。信号なし・却下だけ）に分けた `all` / `long` / `short`（`Metrics`。`daily` なし） |

**アーム**（`definitions` にも同じ文）:

| アーム | 定義 |
|---|---|
| `llm` | その判断自身の方向のラベル |
| `long_leg` / `short_leg` | 同じ判断の時刻・銘柄で常に買った / 常に売ったラベル |
| `momentum` | 判断の足（同じ銘柄・同じ時刻の `market_snapshots`）の `return_5m` が正なら買い、負なら売り、0 または NULL なら見送り（0 bps） |
| `contrarian` | `momentum` の逆（正なら売り、負なら買い、0 または NULL なら見送り） |
| `none` | 常に見送り。net は 0 bps（取引しない） |
| `random` | 方向をランダムに選んだ期待値 = 買いのラベルと売りのラベルの平均。乱数は使わず決定的 |

**`Metrics`**: `n`・`days`（JST の日数）・`mean_net_bps`（行の平均。0 件は `null`）・`ci`（`lower` / `upper` / `level`。判断の JST 日を単位にした 95% のブートストラップ百分位区間。2 日未満は `null`）・`win_rate`（`net > 0` の件数 / `n`。見送りの 0 は勝ちに数えない）・`detectable_minimum_edge_bps`（いまの標準誤差で 0 と区別できる最小の平均）・`days_to_detect[]`・`daily[]`（`date` / `n` / `mean_net_bps` / `win_rate` / `total_net_bps`。古い日から）。

**`Diff`**: llm から baseline を同じ行ごとに引いた差。`n`・`days`・`mean_diff_bps`・`ci`（日ごとの差の合計を日クラスタにする）・`detectable_minimum_difference_bps`（= (z₍1−α/2₎ + z₍power₎) × ブートストラップの標準誤差。α=0.05・power=0.8）・`days_to_detect[]`。

**`days_to_detect[]`**: `target_bps`（+3 と +5。`Diff` では差、`Metrics` ではアーム自身の平均 0 に対する値）ごとに、いまの標準誤差から検出できるまでの**追加の JST 営業日数** `additional_days` = max(0, ceil(日数 × (検出できる最小値 / target)² − 日数))（標準誤差が日数の平方根に反比例して縮む前提）。検出できる最小値が target 以下なら `additional_days=0` と `already_detectable=true`。標準誤差が未定義（2 日未満）なら `additional_days=null`・`already_detectable=false`。`none` アームは分散が 0 なので常に `already_detectable=true`（意味のある数字ではない）。

**`stored_label`**（Calibration 画面と同じ数字との突き合わせ。`calibration_outcomes` だけを読む）: `label_version`（2）・`horizon_minutes`・`all_decisions` / `evaluated_rows`（`decisions`・`with_label`・`without_label`・`mean_net_bps` = `label_version=2` かつ `horizon_minutes` 一致の保存済み `net_return_bps` の平均。`all_decisions` は期間内の全 LONG/SHORT trader 判断、`evaluated_rows` は比べた行）・`recomputed_mean_on_labeled_rows`（保存ラベルのある評価行の、付け直した llm の平均）・`max_abs_difference_bps`（同じ行の付け直した値と保存値の差の最大。設定を変えていなければ 0）。

#### 出力の例（合成データ）

テストの固定データ（`evaltests`。1 日目〜3 日目、`model-1` / `trader-v3` の 6 件。TP/SL 30・コスト 10 bps・10 分）の `all`:

| アーム | n | 日数 | 平均 net bps | 95% 区間 | 勝率 | llm − アーム（平均） | 検出できる最小の差 | +3 / +5 bps まで追加日数 |
|---|---|---|---|---|---|---|---|---|
| llm | 6 | 3 | -5.0 | [-15.0, 0.0] | 0.333 | — | — | 41 / 13（アーム自身） |
| random | 6 | 3 | -1.67 | [-5.0, 0.0] | 0 | -3.33 | 7.62 | 17 / 4 |
| momentum | 6 | 3 | 20.0 | [0.0, 30.0] | 0.667 | -25.0 | 11.42 | 41 / 13 |
| contrarian | 6 | 3 | -20.0 | [-30.0, 0.0] | 0 | 15.0 | 34.27 | 389 / 138 |
| none | 6 | 3 | 0 | [0, 0] | 0 | -5.0 | 11.42 | 41 / 13 |
| long_leg | 6 | 3 | 5.0 | [-30.0, 30.0] | 0.5 | -10.0 | 46.84 | 729 / 261 |
| short_leg | 6 | 3 | -8.33 | [-30.0, 30.0] | 0.333 | 3.33 | 40.72 | 550 / 197 |

`stored_label.evaluated_rows.mean_net_bps` は -5.0（`recomputed_mean_on_labeled_rows` と同じ -5.0、`max_abs_difference_bps` は 0）。判断 11 件のうち 9 件に保存済みラベルがあり（`all_decisions.mean_net_bps` = -3.33、`without_label` = 2）、除外 5 件は理由別に 1 件ずつ。区間と最小の差は日クラスタのブートストラップで日数が 3 日と少ないため幅が広い。

**実際の 2026-11-11 の記録は再現できない**: 本番の DB のスナップショット（LONG/SHORT 約 1357 件、平均 約 -8.1 bps）はリポジトリに無く、テストは本番 DB を読まない。代わりに、同じ件数（1357 件）・同じ平均（約 -8.1 bps）の合成データを、本番の `calibration.Labeler`（`Compute` と `CalibrationRepository`）に `calibration_outcomes` を書かせて作り、**ツールの llm の見出しの平均が、同じ行の保存済み `net_return_bps` の平均と一致する**ことを確かめる（信号なし・却下・通過の判断を全部数える）。

## 受け入れ条件

- `tools/list` に出て `readOnlyHint: true`。書き込み・設定変更・発注の手段が無く、呼び出しの前後で DB の全表が変わらない（エラーになる呼び出しも含む）
- 期間内の trader の LONG/SHORT 判断が `trade_signals` や Risk に関係なく全件数えられる（信号なし・却下・通過）。`NONE`・scout・期間外は数えない
- llm の見出しの平均が、同じ行の保存済み `net_return_bps` の平均と一致する（設定が同じとき。保存ラベルの平均と最大差が `stored_label` に出る）
- llm・買い・売り・momentum・contrarian・none・random が、同じ判断の時刻・銘柄・同じラベルの式で出る。random は買いと売りの平均、none は 0 bps
- (`model_id`, `question_version`) ごとに別のブロックになり、cutoff より前の日は除外件数に出る（仮置きは除外せずフラグ）。同じ行で付かない判断は理由別の件数に出る
- `days_to_detect` の計算（既に検出できる・未定義を含む）とブートストラップが決定的
- テスト: `internal/service/paperresultsmcp/evaltests`（HTTP の外から）、`evaldiag`（検出日数・指標の手計算）、`internal/service/calibration/labelwindowtests`（`LabelWindow` が `Labeler.Compute` と同じ結果を返す）

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-14 | 新規 | issue #1073 |
