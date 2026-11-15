# API 仕様: Paper 実績 MCP の候補の台帳ツール

`docs/api/endpoints/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1090。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。設計は `docs/architecture/research-tools/paper-results-mcp-candidate-ledger.md`。

## 3. ツール

### `candidate_ledger`

ライブの Jev（AI）と、置き換え・併用の候補（別モデルのコホート・シャドーのプロンプト版・お試しのアーム・研究用モデル・単純なベースライン）を、**同じ期間・同じ判断の行・同じコスト控除後ラベル**で並べる読み取り専用の台帳。日クラスタの区間・ライブとの差・多重比較の補正後の p を返す。

**位置づけ**: 研究用。**何も決めない**: 勝者・順位・多数決・推奨・しきい値の項目は応答のどこにも無い（`candidates[]` は種類 → id の固定の順で、値では並べない。テストが応答の全キーを走査して固定している）。Jev・Risk・Policy・注文・メタモデルの学習には使われず、何も書き込まない。

引数:

| 引数 | 内容 |
|---|---|
| `from` / `to` | 他のツールと同じ期間（JST の暦日、`[from, to)`、最大 366 日、既定は直近 30 日）。判断の時刻（`jev_decisions.timestamp`）で切る |
| `horizon` | 任意。`10m`（既定。既存の triple barrier ラベルを 10 分で。`jev_evaluation` と同じ付け直し）・`30m`・`60m`・`close`（`research_horizon_labels` の行を、判断の `(instrument_id, timestamp)` に結んだもの）。30m・60m・close は**両側のラベルが無い判断を全員から除外**し、件数を `excluded_decisions` に出す |
| `alignment` | 任意。`common_all`（既定。基準と、答えのある**全候補**が答えた行だけで全員を比べる）/ `pairwise`（各候補を、ライブと共通の行だけで比べる。候補ごとに行が違う） |
| `candidates` | 任意。候補の id の配列（最大 60。重複・形の誤りは不可）。省略は期間内に見つかった全候補。形は下の表 |
| `usd_jpy` / `notional_jpy` | 任意（正の数）。**両方渡したときだけ** AI の費用を bps に換算する。既定値は置かない |
| `model_id` | 任意。基準（ライブ）をこの `model_id` の判断だけにする（100 文字まで）。省略は、ライブの版の全モデル |

不明な引数・形の誤り・範囲外は `invalid_argument`（422）、期間の誤りは `invalid_period`（422）。期間内の判断が 10,000 件（`evaldiag.MaxDecisions`）を超える、見つかった候補が 60 を超える、読む答えが 20 万行を超えるときは `limit_exceeded`（422。部分的な結果は返さない）。

**候補の id**:

| id | 内容 |
|---|---|
| `live_cohort:<model_id>/<version>` | 期間内の、基準ではない (model_id, 版) のコホート。**別の判断**を見ているので、自分の統計だけを返し、差は null（`diff_reason=different_decisions`） |
| `shadow:<prompt_version>` | `jev_shadow_decisions`。方向は判断ごと（NONE は 0 bps）。費用は `request_cost`（USD。`cost_is_estimate=1` の行は推定として別に数える） |
| `trial:<arm_id>` | `jev_trial_decisions`。`status='ok'` の行だけが答え。failed / refused / partial / 行なしは**欠測**。費用は `counted_cost_usd`、`cost_source` 別に数える |
| `baseline:<arm>` | `random` / `momentum` / `contrarian` / `none` / `long_leg` / `short_leg`（`jev_evaluation` と同じ定義。費用 0） |
| `research_model:<algorithm>:<30m\|60m\|close>` | `research_model_predictions` JOIN `research_model_runs`。予測方向（LONG / SHORT はその側のラベル、NONE は 0）。同じ判断に複数の run の予測があれば最新の run |

候補の 1 行の net bps = **その候補の方向のラベル**（LONG は買いの net、SHORT は売りの net、NONE・取引なしは 0）。全候補が同じコスト控除後ラベルなので、そのまま比べられる。

#### 応答の `data`

| キー | 内容 |
|---|---|
| `from` / `to` / `horizon` / `alignment` | 解決した期間と使った引数 |
| `notice` | 固定の注意書き（研究用・何も決めない・何も書かない） |
| `llm_cutoff` | `date` / `status` / `model` / `applied`（`jev_evaluation` と同じ。仮置きなら除外せず `caveats` に出る） |
| `reference` | 基準（`id=live`）。`prompt_version`（`jev.ResolveLiveVersion` が返すライブの版）・`model_id_filter`・`model_ids`（期間内の `jev_decisions.model_id`）・`multiple_model_ids`（**2 種類以上ならモデルが途中で変わっている印**）・`rows`（基準の行数＝全欠測率の分母）・`days`・`mean_net_bps`・`ci`・`ai_cost`・`common_rows`（`common_all` のときの共通の行数。`pairwise` は null） |
| `excluded_decisions` | `decisions`（期間の trader LONG/SHORT 判断）・`before_cutoff`・`no_label_both_sides`・`no_bar_for_momentum`（30m 以上のみ）・`evaluated`。全候補に共通の除外 |
| `statistics` | `alpha`（0.05）・`power`（0.8）・`resamples`（2000）・`seed`（1073）・`correction`（Holm）・`family_size`（補正の族の大きさ） |
| `candidates[]` | 候補ごとの列（下記） |
| `definitions` / `caveats[]` | 定義文と数字の限界 |

**`candidates[]` の要素**:

| キー | 内容 |
|---|---|
| `id` / `kind` | 候補 id と種類 |
| `no_answers` | 基準の行に 1 件も答えが無い。表には載せるが `common_all` の積集合からは外す（1 つの空の候補で他の表が空にならないように） |
| `n_answered` | 基準の行のうち候補が答えた行数（`live_cohort` は自分の行数） |
| `n_common` | 値を計算した行数（`common_all` では全候補が同じ。`pairwise` では候補ごと。`live_cohort` は null） |
| `missing_rate` | `1 - n_common / reference.rows`。**答えなかった行（記録なし・trial の failed / refused / partial）は欠測**。`common_all` では他の候補が欠測の行も共通から外れるので、1 つの候補の欠測が全員の欠測率を上げる |
| `missing` | `absent`（記録なし）・`failed_or_refused`（trial の ok 以外）・`outside_common`（答えたが共通の行に入らない）。合計 + `n_common` = `reference.rows` |
| `days` / `mean_net_bps` / `ci` | 共通の行での JST 日数・平均 net bps・日クラスタのブートストラップ 95% 区間 |
| `live_mean_on_common_rows` | 同じ行でのライブの平均 net bps |
| `diff_vs_live` | `n`・`days`・`mean_bps`・`ci`・`detectable_minimum_bps`（α=0.05 両側・検出力 0.8 の記述）・`p_value`（平均/標準誤差の正規近似の両側）・`holm_adjusted_p`・`family_size`。差は同じ行ごとの（候補 − ライブ）、日ごとの差の合計が日クラスタ。Holm は p を持つ全候補を 1 つの族として `stats.Holm`。2 日未満は区間・p が null |
| `ai_cost` | AI の費用（USD）を net bps とは**別に**: `rows`・`usd_total`（金額のある行の合計）・`usd_per_row`（全行に金額があるときだけ）・`complete`・`rows_without_amount`・`by_source[]`（`reported` / `list_price_estimate` / `usage_estimate` / `unknown` / `recorded` / `none` ごとの行数と金額。`unknown` に金額は無い）・`notes[]` |
| `combined` | `usd_jpy` と `notional_jpy` が両方あるときだけ: `cost_bps_per_row`（= `usd_per_row × usd_jpy / notional_jpy × 10000`）・`mean_net_after_ai_cost_bps`・`live_cost_bps_per_row`・`diff_after_ai_cost_bps`（差の平均 −（候補の費用 − ライブの費用））。出せないときは null と `reason`（`usd_jpy_and_notional_jpy_required` / `cost_unknown` / `reference_cost_unknown` / `no_common_rows`） |
| `distinct_model_ids` | 期間内に答えたモデルの種類（ライブ = `jev_decisions.model_id`、shadow = `model_id`、trial = `served_model`、research_model = `<algorithm>:<horizon>`、baseline = `baseline:<arm>`） |

**費用の扱い**: 費用が不明な行（trial の `cost_source=unknown` で金額が無い行、`jev_decisions.request_cost` が NULL の行）は**0 円にしない**。合計に入れず、`complete=false`・`rows_without_amount` に数え、1 行あたりの費用と bps への換算は出さない。`counted_cost_usd` の金額がある `unknown` 行（最悪値側の額）は、その額で合計に入れ `by_source` の `unknown` に別に出す。

応答に `response_json`・`state_json`・`raw_data_json`・`error_excerpt` の本文、`runtime_settings` の値（`metalabel.llm_cutoff*` と、ライブの版・バリア・コストの設定を除く）、API キーは含まれない。読む列はどの表でも方向・モデル名・費用・ラベルだけ。
