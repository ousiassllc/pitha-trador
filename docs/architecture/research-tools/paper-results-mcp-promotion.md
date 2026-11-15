# 設計: Paper 実績 MCP の昇格チェックリスト（promotion_checklist）

`docs/architecture/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1091。ツールの引数と出力は `docs/api/endpoints/research-tools/paper-results-mcp-promotion.md`。数字は `candidate_ledger`（`docs/architecture/research-tools/paper-results-mcp-candidate-ledger.md`）と同じ計算。**Settings 画面の「昇格チェックリストの基準」（グループ `research-promotion`）は、基準の値をこの章に書いてある、と案内している。**

## 1. 位置づけ（何もしない）

候補（シャドーのプロンプト版・お試しのアーム・研究用モデル・ベースライン・別コホート）を、いまのもの（既定はライブの Jev）に置き換える**提案のための証拠**を、固定の 11 項目で並べる読み取り専用のツール。

- **何も適用しない**: `applied` は常に `false`。設定・プロンプト版・モデル・`jev_prompt_promotions`・シャドー・お試しの表を変えない。何も書き込まない（MCP の読み取りトランザクションの中だけ。テストが全表の前後の一致と、SELECT だけが実行されたことを固定する）。
- `eligible_as_evidence`（全項目が `pass`）は**指示ではない**。採用は人間が決める（あおいの提案と悠太さんの OK の後）。出力の `notice` に固定文で出る。
- Jev・Risk・Policy・注文・Paper ガード・既存のシャドー・メタモデルの学習には使われない。`runtime_settings` は `research.promotion.*` の 5 キーだけを読む（それ以外のキーは SQL を発行する前に拒否する読み取り口）。
- 実装: `internal/service/paperresultsmcp/promotiondiag`（`Check` が型付きの結果、`Run` がツール）、基準の読み込みと下限は `internal/config/promotionsettings`、登録は `research_tools.go`。テストは `promotiontests`（HTTP）と `promotiondiag` の単体、隔離は `ledgertests/isolation_test.go`。
- `experiment` / `setting`（設定の組）は同じ行で比べられないので、候補にも「いまのもの」にもできない（`unsupported_candidate_kind`。MCP のエラー形式では `invalid_argument`（422）。原因は応答に出さない）。別コホート（`live_cohort`）は別の判断を見ているので候補にはできるが比較の項目はすべて `unknown`（`different_decisions`）、「いまのもの」にはできない。

## 2. 数値の基準（フロア）

次の 5 つが数値の基準で、**この表の値がフロア（文書化された値）**。`promotionsettings` の `FloorMinCommonRows` ほかと同じ値で、一致をテストが固定する。

| 基準 | フロア | 向き | Settings のキー | 呼び出しの引数 |
|---|---|---|---|---|
| 共通の行数の最小 `min_common_rows` | **300** | 大きいほど厳しい | `research.promotion.min_common_rows` | `min_common_rows` |
| 共通の行が及ぶ JST 日数の最小 `min_days` | **10** | 大きいほど厳しい | `research.promotion.min_days` | `min_days` |
| 差（候補 − いまのもの）の日クラスタ 95% 区間の下端の最小（bps）`min_diff_ci_lower_bps` | **0** | 大きいほど厳しい | `research.promotion.min_diff_ci_lower_bps` | `min_diff_ci_lower_bps` |
| 候補が勝った日の割合（%）の最小 `min_positive_day_pct` | **50** | 大きいほど厳しい | `research.promotion.min_positive_day_pct` | `min_positive_day_pct` |
| Holm 補正後の p の最大 `max_adjusted_p` | **0.05** | 小さいほど厳しい | `research.promotion.max_adjusted_p` | `max_adjusted_p` |

固定の定数（設定ではない）: `MinCoverage = 0.8`（項目 1）。

**厳しくする方向にだけ変えられる**:

- Settings（グループ `research-promotion`、`runtime_settings` の `research.promotion.*`）は、フロアより緩い値を**保存できない**（`promotionsettings.Validate`）。保存済みの値がフロアより緩い・壊れているときは**無視してフロアを使う**（`promotionsettings.Effective`。ログに警告）。未設定はフロア。
- 呼び出しの引数は、その時点で有効な基準（フロアか、より厳しい Settings の値）より緩いと `invalid_argument`（422）。同じ値か厳しい値は受け付ける（`Criteria.Tighten`）。
- 応答の `criteria` に `effective`（有効な値）・`floor`（上の値）・`source`（キーごとに `floor` / `setting` / `call`）・`min_coverage` を出す。

## 3. 11 の項目（この順で固定）

各項目は `{name, status, actual, required, comparator, detail}`。`status` は `pass` / `fail` / `unknown`。`unknown` は判定に必要な入力・データが無いことを示し、`pass` ではない（`first_unmet` に出る）。`detail` は短いコード（`ok` ほか）。

| # | `name` | 合格の条件（`pass`） | `unknown` になる場合 |
|---|---|---|---|
| 1 | `same_rows_and_period` | 共通の行（候補といまのものの両方が答えた行）÷ 期間内の基準（ライブ）の行 **≥ 0.8**（`MinCoverage`） | 基準の行が無い |
| 2 | `common_rows` | 共通の行数 **≥ `min_common_rows`**（フロア 300） | `live_cohort`（`different_decisions`） |
| 3 | `common_days` | 共通の行が及ぶ JST 日数 **≥ `min_days`**（フロア 10） | 同上 |
| 4 | `diff_ci_lower` | 差（候補 − いまのもの。同じ行ごと・コスト控除後ラベル）の日クラスタ 95% 区間の**下端 > `min_diff_ci_lower_bps`**（フロア 0。厳密に超えること） | 2 日未満で区間が無い |
| 5 | `multiple_comparison` | Holm 補正後の p **≤ `max_adjusted_p`**（フロア 0.05）。族 = 台帳の p を持つ全候補 + `family_size_extra`（p=1 の比較として足す） | p が無い（2 日未満・ばらつきなし） |
| 6 | `positive_days` | 差の日合計が正の日の割合（%）**≥ `min_positive_day_pct`**（フロア 50） | 共通の行が無い |
| 7 | `not_one_day_wins` | 最も良かった 1 日を除いた差の合計（bps）**> 0** | 共通の行が無い |
| 8 | `max_drawdown` | 共通の行を時刻順に並べた候補の最大ドローダウン（bps。累積 net のピークからの最大の落ち込み。開始の 0 もピーク）**≤ いまのものの最大ドローダウン**（`required` はいまのものの値） | 共通の行が無い |
| 9 | `after_ai_cost` | AI の費用の差（候補 − いまのもの）を bps に換算して引いた後の差の平均 **> 0** | `usd_jpy` と `notional_jpy` が両方無い、または費用が不明な行がある（`detail`: `usd_jpy_and_notional_jpy_required` / `cost_unknown` / `reference_cost_unknown`） |
| 10 | `after_training_cutoff` | `metalabel.llm_cutoff` が仮置きでなく適用済み（`llm_cutoff.applied`。cutoff 前の判断は台帳が除外済み） | cutoff が仮置き（`llm_cutoff_is_placeholder`。LLM が評価期間の値動きを知っていた可能性を除けない） |
| 11 | `model_version_stable` | 期間内の `model_id` が、候補・いまのもの（ライブ）の**どちらも 1 種類**（`actual` に `candidate_model_ids` / `current_model_ids` の数。2 種類以上は `fail`: `more_than_one_model_id`） | どちらかに答えが無い |

全体: `first_unmet`（`pass` でない最初の項目名。全て `pass` なら null）・`eligible_as_evidence`（全項目が `pass`）・`applied: false`・`notice`・`key_numbers`（`reference_rows`・`n_common`・`coverage`・`days`・`candidate_mean_net_bps`・`current_mean_net_bps`・`diff_mean_bps`・`diff_ci`・`p_value`・`holm_adjusted_p`・`family_size`・`family_size_extra`・`positive_day_pct`）。

## 4. 計算

台帳（`ledgerdiag.Compute`）を `alignment=pairwise` で呼ぶ。候補は、いまのものと**両方が答えた行**（共通の行）で、同じ行ごとの差（候補 − いまのもの）を取る。Holm の族は、期間に見つかった全候補（`live_cohort` を除く、p を持つもの。提案する候補は期間に答えが無くても族に数える）。土台の行はいつも**ライブの版の判断の行**で、被覆率・欠測は基準（ライブ）の行数に対して数える。「いまのもの」がライブならそのまま、別の候補（シャドー・trial・研究用モデル・ベースライン）なら、その候補の答えを基準として差・ドローダウン・費用の差を取る。区間・p・Holm は台帳の値そのもの（`candidate_ledger` の `alignment=pairwise` の呼び出しと一致する。テストが `stats.Holm` で固定）。

## 5. 隔離とテスト

- 全表の前後の一致（`runtime_settings`・`jev_prompt_promotions`・シャドー・お試し・研究用の表）、実行した文が SELECT だけであること、本文の列を読まないこと、`runtime_settings` の他の値・API キーが応答に出ないこと（`promotiontests/criteria_test.go`）。
- 5 つの基準それぞれについて、緩い値の拒否・フロアの受理・厳しい値の反映と `source`。フロアより緩い保存値の無視。保存値より緩い呼び出しの拒否（`criteria_test.go`）。
- 強い合成候補が全項目 `pass`、弱い候補が正しい `first_unmet` と `actual` / `required`、`unknown` の扱い（費用・cutoff）、被覆率・行数・日数、モデルの変更、候補・いまのものの種類（`checklist_test.go`・`kinds_test.go`）。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1091 |
