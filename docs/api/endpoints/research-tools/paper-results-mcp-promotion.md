# API 仕様: Paper 実績 MCP の昇格チェックリストツール

`docs/api/endpoints/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1091。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。設計と 11 項目・数値の基準は `docs/architecture/research-tools/paper-results-mcp-promotion.md`。

## 3. ツール

### 3.44 `promotion_checklist`

候補（シャドーのプロンプト版・お試しのアーム・研究用モデル・ベースライン・別コホート・調整の実験）を、いまのもの（既定はライブの Jev）に置き換える**提案の証拠**を、固定の 11 項目で並べる読み取り専用のチェックリスト。**何も適用しない**（`applied` は常に `false`）。設定・プロンプト版・モデルを変えず、何も書き込まない。`eligible_as_evidence` は指示ではなく、採用は人間（あおいの提案と悠太さんの OK）が決める。

引数:

| 引数 | 内容 |
|---|---|
| `from` / `to` | 他のツールと同じ期間（JST の暦日、`[from, to)`、最大 366 日、既定は直近 30 日） |
| `candidate` | 必須。`{kind, id}`。`kind` は `shadow`（id = プロンプト版）・`trial`（アーム id）・`research_model`（`<algorithm>:<30m\|60m\|close>`）・`baseline`（`random` / `momentum` / `contrarian` / `none` / `long_leg` / `short_leg`）・`live_cohort`（`<model_id>/<version>`）・`experiment`（id = 調整の実験の id の整数）・`setting`（id = MCP の設定ホワイトリストのキー。そのキーの最新の実験）。`experiment` / `setting` は `experiment_compare` の前後比較を同じ 11 項目で見る（下の「調整の実験を候補にしたとき」） |
| `current` | 任意。置き換え元。同じ形で、`kind=live`（id は `live`）・`shadow`・`trial`・`research_model`・`baseline`。省略はライブの Jev。`live_cohort`・`experiment`・`setting` は不可（`unsupported_candidate_kind`）。`candidate` が `experiment` / `setting` のときは指定できない（実験の前の期間で決まる。`unsupported_candidate_kind`） |
| `horizon` | 任意。`10m`（既定）・`30m`・`60m`・`close`（`candidate_ledger` と同じ）。`candidate` が `experiment` / `setting` のときは指定できない（`invalid_argument`） |
| `usd_jpy` / `notional_jpy` | 任意（正の数）。**両方**渡したときだけ `after_ai_cost` を判定する。既定値は置かない。`experiment` / `setting` では指定できない（`invalid_argument`。`from` / `to` も同じ。実験自身の前後の期間を使う） |
| `family_size_extra` | 任意（0〜10000、既定 0）。他の場所で試した候補の数。Holm の族に p=1 の比較として足し、補正後の p を強める |
| `min_common_rows` / `min_days` / `min_diff_ci_lower_bps` / `min_positive_day_pct` / `max_adjusted_p` | 任意。基準を**厳しくする方向にだけ**指定できる（フロア 300 / 10 / 0 / 50 / 0.05 か、Settings の `research.promotion.*` の厳しい値より緩いと `invalid_argument`）。整数の引数に小数は不可 |

エラー: 不明な引数・形の誤り・`candidate` なし・候補といまのものが同じ・実験の `id` が正の整数でない・基準を緩める値は `invalid_argument`（422。応答に原因は出ない）。候補・いまのものの**種類や組み合わせを比べられない**ときは専用コード `unsupported_candidate_kind`（422。`invalid_argument` とは別のコード）で、`error.data.detail` に規則の固定文が付く（引数の値・id は引用しない）。対象は、`current` に `experiment` / `setting` / `live_cohort`、`candidate` が `experiment` / `setting` なのに `current` を指定、`candidate` が `setting` でホワイトリストにないキー、の 4 つ。`kind=experiment` で実験が無ければ `experiment_not_found`（404）。期間の誤りは `invalid_period`（422）。上限は `candidate_ledger` と同じ（`limit_exceeded`）。エラー表は `docs/architecture/paper-results-mcp-errors.md`。

#### 応答の `data`

| キー | 内容 |
|---|---|
| `from` / `to` / `horizon` / `candidate` / `current` | 解決した期間と、候補・いまのもの（`experiment` / `setting` のときは、`from` / `to` が実験の前の期間の始めと後の期間の終わり、`horizon` は無く、`current` は `{kind: "experiment_before", id: <実験の id>}`） |
| `notice` | 固定の注意書き（証拠であって指示ではない・何も適用しない・採用は人間が決める） |
| `criteria` | `effective`（有効な 5 つの値）・`floor`（フロア）・`source`（キーごとに `floor` / `setting` / `call`）・`min_coverage`（0.8）・`note` |
| `items[]` | 固定の順の 11 項目 `{name, status, actual, required, comparator, detail}`。`status` は `pass` / `fail` / `unknown`（下表） |
| `first_unmet` | `pass` でない最初の項目名。全て `pass` なら null |
| `eligible_as_evidence` | 全項目が `pass`。**指示ではない**。候補が研究用モデルで、その学習ホライズン（id 末尾）と `horizon` が違うときは常に `false`。`experiment` / `setting` のときも常に `false`（`evidence_note`） |
| `horizon_note` | 上の不一致のときだけ付く説明（学習していない時間軸のラベルで採点した数字なので根拠にしない。`horizon` を合わせて呼び直す） |
| `applied` | 常に `false` |
| `key_numbers` | `reference_rows`・`n_common`・`coverage`・`days`・`candidate_mean_net_bps`・`current_mean_net_bps`・`diff_mean_bps`・`diff_ci`・`p_value`・`holm_adjusted_p`・`family_size`・`family_size_extra`・`positive_day_pct`。`experiment` / `setting` のときは付かない（`experiment_numbers`） |
| `definitions` | 各項目の定義文 |
| `experiment` | `experiment` / `setting` のときだけ。読んだ実験 `{id, status, variable_kind, variable_name, verdict, judged, confounders, before_from, before_to, after_from, after_to}`（`verdict` / `judged` は `experiment_compare` のもの。`setting` で実験が無いときは付かない）。変数の before / after の値は出さない |
| `experiment_numbers` | `experiment` / `setting` のときだけ。`before_trades` / `after_trades`・`before_days` / `after_days`・`before_mean_jpy_per_trade` / `after_mean_jpy_per_trade` / `diff_mean_jpy_per_trade`・`diff_ci_lower_jpy_per_trade` / `diff_ci_upper_jpy_per_trade`・`p_value`・`holm_adjusted_p`・`family_size`（記録された全実験 + `family_size_extra`）・`family_size_extra`・`positive_day_pct`・`before_mean_day_jpy`・`after_minus_before_without_best_day_jpy`・`before_max_drawdown_jpy` / `after_max_drawdown_jpy`。出せない値は null |
| `evidence_note` | `experiment` / `setting` のときだけ。根拠に使えない理由の固定文（前後は別の取引で、適用外の項目があるため `eligible_as_evidence` は常に `false`）。実験が無い `setting` では実験を記録してから呼び直す案内 |

**`items[]`**（この順）: `same_rows_and_period`・`common_rows`・`common_days`・`diff_ci_lower`・`multiple_comparison`・`positive_days`・`not_one_day_wins`・`max_drawdown`・`after_ai_cost`・`after_training_cutoff`・`model_version_stable`。各項目の合格の条件・`unknown` になる場合・数値の基準は設計の §2〜§3。`comparator` は `actual` が `required` に対して満たすべき関係（`>=` / `>` / `<=` / `==`）。`detail` は短いコード（`ok`・`below_required`・`above_required`・`no_common_rows`・`different_decisions`・`ci_undefined_fewer_than_two_days`・`p_undefined`・`total_depends_on_the_best_day`・`candidate_drawdown_worse_than_current`・`llm_cutoff_is_placeholder`・`more_than_one_model_id` ・`usd_jpy_and_notional_jpy_required`・`cost_unknown`・`reference_cost_unknown` ほか）。

#### 調整の実験を候補にしたとき（`kind=experiment` / `setting`）

設計は `docs/architecture/research-tools/paper-results-mcp-promotion.md` §6。`experiment_compare` と同じ前後比較（後 = 実験の開始から終了、前 = 直前の同じ長さ）を、同じ 11 項目で見る。

- `items[]` は同じ 11 項目・同じ順・同じ形。件数・日数は前後の少ない方、`diff_ci_lower` は 1 取引あたりの純損益（円）の差の 95% 区間の下端 > 0（`min_diff_ci_lower_bps` を 0 より厳しくすると円と比べられないので `unknown`: `criterion_in_bps_not_comparable_to_yen_per_trade`）、`multiple_comparison` は記録された全実験を族にした補正後の p、`positive_days` / `not_one_day_wins` / `max_drawdown` は前の期間との比べ方（設計 §6.3）、`model_version_stable` は実験の変数以外の Jev のモデル・プロンプトの版が変わっていないこと。
- 前後は別の取引なので成り立たない 3 項目（`same_rows_and_period`・`after_ai_cost`・`after_training_cutoff`）は、`fail` にせず `status: "unknown"`・`not_applicable: true`・`detail`（`not_applicable_unpaired_periods` / `not_applicable_ai_cost_not_in_trade_pnl` / `not_applicable_trades_not_cutoff_filtered`）で返す。`first_unmet` は適用外でない最初の `pass` でない項目。
- 実験自身の判定が保留（`aborted`・`confounded`・`switch_unconfirmed`・`insufficient_trades`・`not_computable`）のときは、区間や損益を読む 5 項目（`diff_ci_lower`・`multiple_comparison`・`positive_days`・`not_one_day_wins`・`max_drawdown`）が `unknown`（`detail`: `experiment_<判定>`）。`setting` の実験が無いときは全項目 `unknown`（`no_experiment_result`）。
- 結果は常に `eligible_as_evidence: false`・`applied: false`。数字は参考で、適用は人間が決める。ツールは設定・プロンプト版・モデル・実験の表を変えない。
- 基準 5 つと `family_size_extra` は他の候補と同じに指定でき、厳しくする方向にだけ変えられる。

応答に `response_json`・`state_json`・`raw_data_json`・`error_excerpt` の本文、API キー、`research.promotion.*` 以外の `runtime_settings` の値は含まれない。
