# API 仕様: Paper 実績 MCP の昇格チェックリストツール

`docs/api/endpoints/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1091。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。設計と 11 項目・数値の基準は `docs/architecture/research-tools/paper-results-mcp-promotion.md`。

## 3. ツール

### `promotion_checklist`

候補（シャドーのプロンプト版・お試しのアーム・研究用モデル・ベースライン・別コホート）を、いまのもの（既定はライブの Jev）に置き換える**提案の証拠**を、固定の 11 項目で並べる読み取り専用のチェックリスト。**何も適用しない**（`applied` は常に `false`）。設定・プロンプト版・モデルを変えず、何も書き込まない。`eligible_as_evidence` は指示ではなく、採用は人間（あおいの提案と悠太さんの OK）が決める。

引数:

| 引数 | 内容 |
|---|---|
| `from` / `to` | 他のツールと同じ期間（JST の暦日、`[from, to)`、最大 366 日、既定は直近 30 日） |
| `candidate` | 必須。`{kind, id}`。`kind` は `shadow`（id = プロンプト版）・`trial`（アーム id）・`research_model`（`<algorithm>:<30m\|60m\|close>`）・`baseline`（`random` / `momentum` / `contrarian` / `none` / `long_leg` / `short_leg`）・`live_cohort`（`<model_id>/<version>`） |
| `current` | 任意。置き換え元。同じ形で、`kind=live`（id は `live`）・`shadow`・`trial`・`research_model`・`baseline`。省略はライブの Jev。`live_cohort` は不可 |
| `horizon` | 任意。`10m`（既定）・`30m`・`60m`・`close`（`candidate_ledger` と同じ） |
| `usd_jpy` / `notional_jpy` | 任意（正の数）。**両方**渡したときだけ `after_ai_cost` を判定する。既定値は置かない |
| `family_size_extra` | 任意（0〜10000、既定 0）。他の場所で試した候補の数。Holm の族に p=1 の比較として足し、補正後の p を強める |
| `min_common_rows` / `min_days` / `min_diff_ci_lower_bps` / `min_positive_day_pct` / `max_adjusted_p` | 任意。基準を**厳しくする方向にだけ**指定できる（フロア 300 / 10 / 0 / 50 / 0.05 か、Settings の `research.promotion.*` の厳しい値より緩いと `invalid_argument`）。整数の引数に小数は不可 |

エラー: 不明な引数・形の誤り・`candidate` なし・候補といまのものが同じ・`experiment` / `setting`（`unsupported_candidate_kind`。設定の組は同じ行で比べられない）・`live_cohort` を `current` にする・基準を緩める値は `invalid_argument`（422。応答に原因は出ない）。期間の誤りは `invalid_period`（422）。上限は `candidate_ledger` と同じ（`limit_exceeded`）。

#### 応答の `data`

| キー | 内容 |
|---|---|
| `from` / `to` / `horizon` / `candidate` / `current` | 解決した期間と、候補・いまのもの |
| `notice` | 固定の注意書き（証拠であって指示ではない・何も適用しない・採用は人間が決める） |
| `criteria` | `effective`（有効な 5 つの値）・`floor`（フロア）・`source`（キーごとに `floor` / `setting` / `call`）・`min_coverage`（0.8）・`note` |
| `items[]` | 固定の順の 11 項目 `{name, status, actual, required, comparator, detail}`。`status` は `pass` / `fail` / `unknown`（下表） |
| `first_unmet` | `pass` でない最初の項目名。全て `pass` なら null |
| `eligible_as_evidence` | 全項目が `pass`。**指示ではない** |
| `applied` | 常に `false` |
| `key_numbers` | `reference_rows`・`n_common`・`coverage`・`days`・`candidate_mean_net_bps`・`current_mean_net_bps`・`diff_mean_bps`・`diff_ci`・`p_value`・`holm_adjusted_p`・`family_size`・`family_size_extra`・`positive_day_pct` |
| `definitions` | 各項目の定義文 |

**`items[]`**（この順）: `same_rows_and_period`・`common_rows`・`common_days`・`diff_ci_lower`・`multiple_comparison`・`positive_days`・`not_one_day_wins`・`max_drawdown`・`after_ai_cost`・`after_training_cutoff`・`model_version_stable`。各項目の合格の条件・`unknown` になる場合・数値の基準は設計の §2〜§3。`comparator` は `actual` が `required` に対して満たすべき関係（`>=` / `>` / `<=` / `==`）。`detail` は短いコード（`ok`・`below_required`・`above_required`・`no_common_rows`・`different_decisions`・`ci_undefined_fewer_than_two_days`・`p_undefined`・`total_depends_on_the_best_day`・`candidate_drawdown_worse_than_current`・`llm_cutoff_is_placeholder`・`more_than_one_model_id` ・`usd_jpy_and_notional_jpy_required`・`cost_unknown`・`reference_cost_unknown` ほか）。

応答に `response_json`・`state_json`・`raw_data_json`・`error_excerpt` の本文、API キー、`research.promotion.*` 以外の `runtime_settings` の値は含まれない。
