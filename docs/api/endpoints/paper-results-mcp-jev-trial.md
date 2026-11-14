# API 仕様: Paper 実績 MCP の意思決定モデル比較（お試し）ツール

`docs/api/endpoints/paper-results-mcp.md` §3.32 の分割章（行数上限のため別ファイル）。issue #1060（トライアルの記録は #1059、ワイヤ層は #1058）。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。**2 ツールとも読み取り専用**（`readOnlyHint: true`・`destructiveHint: false`。何も書かず、設定も変えない）。指標の定義と設計は `docs/architecture/paper-results-mcp-jev-trial.md`、記録の表は `docs/architecture/er/tables-jev-trial.md`。

## 3. ツール

### 3.32 `jev_trial_compare` / `jev_trial_cost`

ライブの Jev の trader の判断と**同じ入力**を別モデル（アーム）に聞いた結果 `jev_trial_decisions`（成功も失敗も 1 行）を、読み取りだけで集計する。両ツールとも `jev_decisions`・`calibration_outcomes`・`counterfactual_labels`・`runtime_settings` の固定キーを**読む**だけで、新しいラベルは計算しない。

**やらないこと（出力にも項目が無い）**: 多数決・合議・「採用すべきアーム」・勝者の自動決定・自動昇格。`rank` は 1 つの指標の並べ替えで、昇格や採用の推奨ではない。`jev.trial.enabled`・アーム・上限の書き込みは**できない**（これらのキーは `settings_set` のホワイトリストに無い。オンにできるのはアプリの Settings 画面だけ）。API キー（`OPENROUTER_API_KEY`）・応答本文（`response_json`）・エラーの抜粋（`error_excerpt`）・エラー本文は返さない（件数と固定コードだけ）。

引数（どちらも）:

| 引数 | 内容 |
|---|---|
| `from` / `to` | 他のツールと同じ期間（JST の暦日、`[from, to)`、最大 366 日、既定は直近 30 日）。`jev_trial_compare` では**ライブの判断の時刻**（`jev_decisions.timestamp`）で、`jev_trial_cost` では**トライアルの行の保存時刻**（`jev_trial_decisions.created_at`、JST の日で区切る）で期間を切る |
| `arms` | 任意。対象のアーム id（`jev.trial.arms` の id。最大 8 個・重複不可・`a-z0-9._-` の 1〜48 文字）。省略は期間内に行のある全アーム。形が違う・重複・9 個以上・他の引数は `invalid_argument`（422）、期間の誤りは `invalid_period`（422） |

#### `jev_trial_compare` の応答の `data`

| キー | 内容 |
|---|---|
| `notice` | 固定の注意書き: rank は並べ替えにすぎず昇格・採用の推奨ではない／truncation_suspected・`rag_includes_live_model_answers` の行・`llm_cutoff_status` が `unverified` の行が残る間は同じ入力の公平な比較ではない／失敗・拒否・一部拒否は「答えなし」として分母に入れる／NONE は当たり外れに数えない／反実仮想は proxy |
| `definitions` | `cohort` / `common_success` / `missing_rate` / `over_cohort` / `over_answered` / `none` / `hit` / `rank` の定義文（下記） |
| `arms` | 比較したアーム id（選んだ順。省略時は id の昇順） |
| `cohort` | `definition` / `common_success_definition`（定義文）・`size`（コホートの判断数）・`common_success_count`（共通の成功数）・`common_success_rate`（= `common_success_count / size`、`size` が 0 なら `null`）・`rows`（コホートの判断に対する全アームの行の `RowCounts`） |
| `per_arm[]` | アームごと。`arm_id`・`period_rows`（期間内のそのアームの全行の `RowCounts`）・`cohort_rows`（コホートの判断に対する行の `RowCounts`）・`direction`・`direction_agreement`・`entry_quality` |
| `fairness` | `cohort_size` と `caveats[]`（コホートの行に見つかった公平でない条件: `truncation_suspected` / `rag_includes_live_model_answers` / `llm_cutoff_unverified`。該当が無ければ空） |
| `outcomes[]` | (`horizon_minutes`, `label_version`) ごとのスライス（ラベルの版は混ぜない）。各要素は `horizon_minutes`・`label_version`・`live_baseline`（同じコホートのライブ自身の採点: `records`・`none_count`・`scorable`・`hits`・`hit_rate_over_cohort`。アームの当たり率はこれと並べて読む）・`arms[]`。`arms[]` は `arm_id`・`rank`・`cohort`（`OutcomeStats`）・`common_success`（`informational: true` と、同じ `OutcomeStats` の項目） |
| `counterfactual_proxy` | `proxy: true`（常に）・`note`・`arms[]`（`arm_id`・`no_direction_decisions`・`none_answers`・`no_answer`・`horizons[]`）。下の「NONE と反実仮想」 |

**`RowCounts`**: `total`（分母。失敗・拒否・一部拒否を含む）・`ok` / `failed` / `refused` / `partial`・`missing_rate`（= (`failed` + `refused` + `partial`) / `total`。`total` が 0 なら `null`）・`failure_codes`（`failed` の行の `failure_code` 別件数。固定コードのみ）・`truncation_suspected`（フラグが付いた行の数）・`rag_includes_live_model_answers`（同）・`llm_cutoff_status`（状態別の件数）。**フラグの付いた行は除外せず件数で出す。**

**コホートと共通の成功**: コホート = 期間内のライブの trader 判断のうち、**選んだアームすべて**に聞いた判断（各アームの行があること。status は問わない）。`common_success_count` = コホートのうち、選んだアームすべての行が `ok` の判断の数。アームを 1 つだけ選ぶと、そのアームが `ok` の判断の数になる。

**`direction`**: `cohort`（= `answered` + `no_answer`）・`answered`（`ok` の行）・`no_answer`（`failed` / `refused` / `partial` の行。答えが無い判断として分母に入れる）・`long` / `short` / `none_count`（NONE は別枠）。`partial` は一部の質問だけ答えているが、方向の答えを信用しないので「答えなし」に数える。

**`direction_agreement`**: `agree`（アームの方向 = ライブの方向。NONE と NONE も一致）・`over_cohort`（`agree` / コホート。答えなしは不一致）・`over_answered`（`agree` / 答えた判断。参考）。

**`entry_quality[]`**: poor〜exceptional の 5 水準（0 件も出す）。`count`・`share_over_cohort`・`share_over_answered`。

**`OutcomeStats`**（スライスごと。結果の記録＝`calibration_outcomes` の行がある判断だけが対象）:

| キー | 内容 |
|---|---|
| `records` | 結果の記録がある判断の数（コホート、または共通の成功の部分集合のうち）。次の 4 つのどれか 1 つに必ず入る |
| `no_answer` | アームの行が `ok` でない判断 |
| `none_count` | NONE と答えた判断。方向の結果が無いので当たり外れに数えない（方向を補わない）。`records` には入る |
| `excluded_disagreement` | 方向を答えたが採点できない判断: ライブが LONG/SHORT で、アームの方向がそれと違う（結果のラベルはライブの方向のものなので、ライブの結果がアームの結果を決めてしまう。`jev_shadow_compare` と同じ規則） |
| `scorable` | 採点できた判断: アームが LONG/SHORT で、ライブの方向と同じか、ライブが NONE |
| `hits` | `scorable` のうち、方向で符号を付けた `future_return`（SHORT は符号反転。コスト控除前の％）が 0 を超えた判断 |
| `hit_rate_over_cohort` | `{numerator: hits, denominator: records, rate}` — **順位の元**。答えなし・NONE・採点できない方向を分母に含む |
| `hit_rate_over_answered` | `{hits, scorable, rate}` — 参考。答えた分だけが良く見えるので、これだけで比べない |
| `coverage` | `{scorable, records, rate}` — 採点できた割合 |
| `by_entry_quality[]` | 5 水準ごとの `scorable`・`hits`・`hit_rate_over_answered`（entry_quality の較正の見方。水準の閾値は評価側の固定値で、`HitThresholdPct` = 0） |

`rank`: スライスごとに、`hit_rate_over_cohort` の高い順の競技順位（同率は同順位。1, 1, 3）。結果の記録のある判断が 20 件（`MinOutcomeRecordsForRank`）未満のスライス、およびアームが 1 つだけのときは `null`（1 つだけの 1 位は勝者に読めるため）。ラベルはライブの方向のものなので、ライブと逆向きの答えは採点されず（`excluded_disagreement`）、当たり率にはライブとの一致度も含まれる。基準は `live_baseline`。**成功した行だけで順位は作らない**: 少ない成功がすべて当たりでも、失敗の多いアームは分母（コホート）が大きいので上位にならない。`common_success` 側に `rank` は無い（参考値）。

**NONE と反実仮想**（`counterfactual_proxy`）: NONE の答えには方向の結果が無いので、`calibration_outcomes` の当たり外れには数えず（`none_count`）、方向を補完して勝たせない。分母（コホート）には入る。見送られたライブの LONG/SHORT 候補に、既存の記録として `counterfactual_labels`（「ライブの方向で入っていたらどうなったか」。このツールは新しく計算しない）が付いていれば、アームが NONE と答えた判断・答えなかった判断について、`proxy: true` のブロックに別に出す: `no_direction_decisions`（= `none_answers` + `no_answer`）と、ホライズンごとの `labelled`・`coverage`（`labelled / no_direction_decisions`）・`barrier_hit`（`tp` / `sl` / `timeout` の件数）・`avg_net_return_bps_of_live_direction`（ラベルの `net_return_bps` の平均。ライブの方向のコスト控除後の bps）。実現した結果ではなく、見送りが結果として良かったかの目安にすぎないので、`outcomes[]` の指標・順位には混ぜない。ラベルの無い判断は `coverage` に現れる。

#### `jev_trial_cost` の応答の `data`

| キー | 内容 |
|---|---|
| `notice` | 固定の注意書き（出所の違い・unknown の扱い） |
| `limits` | いまの上限と止まった状態（読み取りだけ。下記） |
| `total` | 期間全体の `CostBlock` |
| `per_arm[]` | アームごとの `CostBlock`（`arm_id` つき） |
| `daily[]` | JST の日ごと（古い順）。`date`・`overall`（`CostBlock`）・`arms[]`（アームごとの `CostBlock`） |

**`CostBlock`**: `calls`・`reported`（`calls`・`usd`: 応答が報告した `usage.cost` の合計・`counted_usd`: 上限に数えた額。`usd` と同じ）・`list_price_estimate`（`calls`・`usd`: 定価×入力トークンの推定の合計・`counted_usd`: 上限に数えた額。処理先を固定していないアームは定価と最悪単価の高い方で数えるので `usd` 以上になる）・`unknown`（下記）・`counted_total_usd`（上限の判定に数えた額の合計 = 1 日の上限の基準。`reported.counted_usd` + `list_price_estimate.counted_usd` + `unknown.unknown_counted_usd`）。

**`unknown`**: `calls`・`actual_usd`（常に `null`。実費は分からない。0 円ではない）・`unknown_counted_usd`（上限の判定に数えた額。最悪値側の単価で数えた**上限の見積り**で、`reported` / `list_price_estimate` の合計には混ぜない。0 に丸めない）・`counted_as_zero_calls`（課金されない前提の 4xx（413 を除く）・529 応答を 0 と数えた件数。低い `unknown_counted_usd` を「無料」と読まないために出す）・`basis`（`cap_counting_upper_bound`）。

**`limits`**: `enabled`（トライアルの有効。読み取りのみ）・`today`（JST の日）・`arms_config_invalid`（保存されたアーム設定が読めない。理由の文は返さない）・`overall_daily_cap_usd`・`overall_cap_source`（`default` / `stored`）・`overall_counted_today_usd`・`halt`（全体の止まりの記録 `{date, reason, active_today}`。無ければ `null`。`reason` は `daily_cost_cap` / `payment_required` など固定コード）・`failure_cap`・`throttle_cooldown_seconds`・`min_call_interval_seconds`・`worst_case_usd_per_mtok`（unknown を数える単価）・`arms[]`（設定されたアームごと: `arm_id`・`model`・`prompt_version`・`daily_cap_usd`・`list_price_known`・`counted_today_usd`・`failures_today`・`halt`（`{date, reason, active_today}` または `null`）・`throttle`（`{date, reason, active}` または `null`。429/529 の止まり））。止まりの記録は理由と日だけで、金額・時刻・ステータス・本文は返さない。

## 受け入れ条件

- 両ツールが `tools/list` に出て `readOnlyHint: true`。書き込み・発注・設定変更の手段が無く、`jev.trial.*` は `settings_set` が `key_not_allowed`（403）で拒否する
- 共通の成功数（`common_success_count`）と欠測率（`missing_rate`。分母に失敗・拒否・一部拒否を含む）が出る。成功した行だけの集計で順位を作らない（失敗の多いアームは上位にならない）
- 費用は出所別（`reported` / `list_price_estimate` / `unknown`）・アーム別・日別。unknown は 0 円にならず（`actual_usd` は `null`）、他の合計に混ざらない
- 出力のどの階層にも、多数決・合議・勝者・推奨・昇格の項目が無い（テストが出力の全キーを走査する）
- NONE は当たり外れに数えず、反実仮想は `counterfactual_proxy`（`proxy: true`）に別に出る。切り詰め疑い・RAG フラグの行は除外せず件数で出る
- 応答本文・エラーの抜粋・API キーが出力に無い。取引・Risk・発注・ライブ Jev・既存のシャドー・学習の経路は変わらない
- テスト: `internal/service/paperresultsmcp/trialtests`（HTTP の外から）

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-13 | 新規 | issue #1060 |
