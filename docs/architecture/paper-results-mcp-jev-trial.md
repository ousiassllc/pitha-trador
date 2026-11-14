# 設計: Paper 実績 MCP の意思決定モデル比較（お試し）ツール

`docs/architecture/paper-results-mcp.md` §5 の分割章（行数上限のため別ファイル）。issue #1060。ツールの引数と出力は `docs/api/endpoints/paper-results-mcp-jev-trial.md` §3.32、記録の表は `docs/architecture/er/tables-jev-trial.md`、記録する側（ランナー）は `docs/architecture/overview/integrations-jev-trial.md`。

## 1. 位置づけ

`jev_trial_compare` と `jev_trial_cost` は、トライアル（ライブの Jev の判断と同じ入力を別モデルに聞く Paper 限定のお試し）の記録を**読むだけ**の 2 ツールである。既存の `jev_shadow_compare` / `jev_shadow_cost`（プロンプトの版のシャドー）とは別の系統で、`jev_shadow_decisions` も `jev.shadow.*` のキーも読まない。

- 読む表: `jev_trial_decisions`（本体）、`jev_decisions`（ライブの判断。時刻・方向）、`calibration_outcomes`（実現した結果の既存ラベル）、`counterfactual_labels`（見送り候補の反実仮想の既存ラベル）、`runtime_settings` の `jev.trial.*`（固定キーを 1 キーずつ。上限・止まりの記録の表示）
- 書くもの: 無い。MCP の読み取り接続（`mode=ro` + `query_only`）の読み取りトランザクションの中だけで動き、書き込み用の接続を開かない
- 返さない列: `response_json`・`error_excerpt`（読みもしない）。API キーは MCP プロセスに無い。エラー本文の全文は表に残さない設計（`error_excerpt` は 200 バイトまで）で、ツールはその抜粋も返さない
- `jev.trial.enabled`・`jev.trial.arms`・`jev.trial.daily_cost_cap_usd`・各止まりのキーは `mcpwritesettings` のホワイトリストに入れない。トライアルは費用が発生するので、オンにできるのはアプリの Settings 画面だけ
- 実装: SQL と集計は `internal/service/paperresultsmcp/query/trialquery`（`jev_shadow_*` の `query/shadowquery` と同じ分け方）、引数と説明は `internal/service/paperresultsmcp/trialtool`、登録は `jev_models_tool.go`（`diagTool`）。テストは `internal/service/paperresultsmcp/trialtests`

## 2. 比較の考え方（公平さ）

比較は「成功した例だけを並べて勝ったように見せる」ことを避けるために、次の規則で作る。

| 概念 | 定義 | 理由 |
|---|---|---|
| コホート | 期間内のライブの trader 判断のうち、選んだアーム**すべて**に聞いた判断（行の status は問わない）。選んだアームのどれかが**見送った**判断（お試しが呼ばなかった判断）は入らない | アームごとに母集団が違うと比べられない。失敗した判断を落とすと、よく失敗するアームが有利になる。見送りで落ちた判断は `fairness.skipped` に理由別の件数で並べて見せる |
| 共通の成功 | コホートのうち、全アームの行が `ok` の判断。その数が `common_success_count` | 全員が答えた同じ入力での比較の母集団。ただし少なくなりがちなので参考値（`informational`）で、順位の元にしない |
| 欠測率 | `(failed + refused + partial) / total`。分母は総呼び出し数（呼んだ行だけ） | 失敗・拒否・一部拒否を分母に含める。成功だけの率にしない。**見送りは分母にも分子にも入れない**（行が無く、成功・失敗・欠測のどれでもないため。足すと欠測率の意味が変わる。見送りは別の指標 `asked_share` で見せる） |
| 見送り | お試しが、ライブの判断に対してアームを**呼ばなかった**こと。1 判断 × 1 アームにつき 1 件（`jev_trial_skips`）。理由別に数える（下記） | 同時実行枠が満杯・最短間隔・日次上限・キーなし・プロンプト未登録などで呼ばなかった判断が、行も残さずコホートから静かに抜けると、ライブ判断が短い時間に集中したとき成績の良い局面だけが残る選択バイアスになり得る。抜けた件数を見せる |
| `over_cohort` | 分母 = コホート（結果の記録がある判断）。答えなし・NONE・採点できない方向を分母に含む | 順位はこちらで付く。答えない・NONE ばかりのアームは当たり率が下がる（取りこぼしを隠さない） |
| `over_answered` | 分母 = 採点できた答え。参考値 | 答えた分だけが良く見えるので、単独で比べない |

`rank` は `hit_rate_over_cohort` の高い順の競技順位で、結果の記録のある判断が 20 件（`MinOutcomeRecordsForRank`）未満のスライス、およびアームが 1 つだけのときは付けない。ラベルはライブの方向のものなので当たり率にはライブとの一致度も含まれ、比べる基準としてスライスごとに `live_baseline`（ライブ自身の同じ採点）を返す。`rank` は 1 つの指標の並べ替えであり、昇格・採用の推奨ではない。多数決・合議・「採用すべきアーム」・勝者・自動昇格の項目は、出力にも内部の型にも無い（テストが出力の全キーを走査する）。

**公平でない条件は除外せず数える**: `truncation_suspected = 1` の行（入力が切り詰められた疑い。Clef 系の切り詰めの有無が実測できるまで、同じ入力の公平比較とは扱わない）、`rag_includes_live_model_answers = 1` の行（RAG の文脈にライブの Jev の過去の答えが入っている。完全な独立条件ではない）、`llm_cutoff_status` が `unverified` の行（モデルの学習データの終了日が未確認）は、`RowCounts` と `fairness.caveats` に件数・内訳で出し、行は指標から外さない。外すと「どれだけ公平でないか」が見えなくなるため、判断は読み手に任せる。`notice` が、これらが残る間は公平な同じ入力の比較ではないと明示する。

**見送りの件数（`fairness.skipped`）**: コホートの前提（「選んだアームすべてに聞けた判断だけ」）と並べて、落ちた判断を理由別に出す。数え方:

- 見送りは、ライブの判断ごと・アームごとに 1 件。お試し側は `jev_trial_skips` に 1 行ずつ残す（答え・費用・status を持たない別表。`jev_trial_decisions` には入れず、成功や失敗の行としては扱わない。ライブの `jev_decisions` と決定経路は変えない）。
- 理由コード（固定。`by_reason` には常に全コードを 0 を含めて出す）: `concurrency_full`（同時実行枠が満杯）・`min_interval`（アームごとの最短間隔 2 秒）・`daily_cap_overall`（全体の日次上限に到達。飛んでいる呼び出しの予約で余白が無い場合も含む）・`daily_cap_arm`（アームの日次上限）・`key_not_set`（OpenRouter のキーが無い）・`prompt_version_unregistered`（プロンプト版が未登録）・`stopped_payment_required`（HTTP 402 による全体停止）・`stopped_forbidden`（HTTP 403 によるアーム停止）・`stopped_failure_cap`（アームの失敗回数の上限）・`stopped_other`（この版が知らない理由の停止記録）・`throttled`（429/529 の待機）・`check_failed`（停止記録や費用が読めず呼ばなかった。設定が読めなかったときを含む）。停止の記録が出た後の見送りは、記録の理由で分類する（全体上限で止まった後は `daily_cap_overall`）。
- 項目: `recorded_decisions`（選んだアームのどれかに行か見送りの記録がある判断の数）= `cohort_decisions` + `dropped_decisions`（選んだアームのどれかが見送った判断。コホートの外）+ `outside_cohort_without_skip`（一部のアームにだけ行があり、見送りの記録が無い判断。アームを途中で足した・停止やシャットダウンで記録が残らなかった等）。`arm_decisions_skipped` と `by_reason`（判断 × アームの件数）、`per_arm[]`（`arm_id`・`asked`＝呼んだ行数・`skipped`・`asked_share`＝`asked / (asked + skipped)`・`by_reason`）。アームを選ばずに、行が無く見送りだけがあるアームは `arms_without_rows[]` に出す（比較するアームには入らないのでコホートは変わらないが、見えなくはしない）。`counted_in_missing_rate` は常に `false`。
- 期間は、ライブの判断の時刻で決める（コホートと同じ）。お試しが無効・アーム無し・Paper 以外・設定を読めなかった（アームが分からない）判断は、行も見送りの記録も無く、どの数にも入らない。シャットダウンで取り消された呼び出しも数えない。
- 記録は書ければ書く（失敗はログだけ。見送り自体は変わらない）。件数は選択バイアスの目安であり、補正や重み付けはしない。

## 3. 結果の付け方（既存の記録だけ）

新しいラベルは計算しない。結果は、ライブの判断に付いた既存の `calibration_outcomes`（`future_return`。ライブの方向のラベル）を `jev_decision_id` で結ぶ。ラベルの版（`label_version`）とホライズンは混ぜず、スライスごとに出す。

アームの方向を採点できるのは、結果の終端がライブの方向に依存しないときだけである（`jev_shadow_compare` と同じ規則。`docs/architecture/paper-results-mcp-jev-prompt.md` §5.1）: アームの方向がライブの方向と同じ（ライブの方向のバリアで決まる）か、ライブが NONE（ホライズンの終端）。ライブが LONG/SHORT でアームが逆なら、ライブのラベルがアームの結果を決めてしまうので採点せず `excluded_disagreement` に数える。当たりは、方向で符号を付けた `future_return`（SHORT は反転）が 0 を超えること（コスト控除前。`HitThresholdPct`）。数字の閾値は評価側の固定値で、トライアルの行には閾値が無い。

### NONE と反実仮想（proxy）

- NONE は「入らない」答えで、方向の結果が存在しない。当たり外れに数えず（`none_count`）、方向を補完して当たりにしない。分母（コホート）には入るので、NONE を多く出すアームの `over_cohort` は下がる
- 見送りの反実仮想ラベル（`counterfactual_labels`。見送られたライブの LONG/SHORT 候補を、ライブの方向で入っていたらどうなったか）が、アームが NONE と答えた（または答えなかった）判断に既存の記録として付いていれば、`counterfactual_proxy`（`proxy: true`）に別に出す。ラベルの件数・`coverage`・バリアの内訳・平均 `net_return_bps` だけで、当たり外れ・順位・実現した結果の指標には**一切混ぜない**。実現した結果ではなく、見送りが結果として良かったかの目安だから
- 反実仮想のラベルを新しく計算する・過去の判断に付け足す処理は、このツールの対象外（読み取りだけ）

## 4. 費用の出し方

`jev_trial_cost` は `jev_trial_decisions.cost_source` を区別する（記録側の定義は `integrations-jev-trial.md`）。

- `reported`（応答の `usage.cost`）と `list_price_estimate`（定価×入力トークン）は、それぞれ件数と `cost_usd` の合計（`usd`）、上限に数えた額の合計（`counted_usd`）を出す。`reported` は両者が同じ。`list_price_estimate` は、処理先を固定していないアーム（`provider_only` なし）を定価と最悪単価の高い方で数えるので、`counted_usd` が `usd` 以上になる
- `unknown`（どちらも無い）は、実費が分からないので `actual_usd` を `null` にする。0 円にしない。上限の判定に数えた額 `unknown_counted_usd`（最悪値側の単価 `0.24` USD/M で数えた上限の見積り）は別の欄に置き、`reported` / `list_price_estimate` の合計には混ぜない。課金されない前提の 4xx（413 を除く）・529 応答は 0 と数えるので、その件数 `counted_as_zero_calls` を並べて出し、低い数えた額が「無料」に見えないようにする
- `counted_total_usd` は上限の判定に数えた額（`counted_cost_usd` の合計 ＝ `reported.counted_usd` + `list_price_estimate.counted_usd` + `unknown_counted_usd`）で、保存済みの行の分。飛んでいる呼び出しの予約（メモリ上）は含まない（`integrations-jev-trial.md`「飛んでいる呼び出しと、上限を超え得る幅」）。1 日の上限（全体・アームごと）の基準と一致する
- `limits.skipped_today`（全体）と `limits.arms[].skipped_today`（アームごと）に、今日（JST。`jev_trial_skips.created_at`）の見送り（呼ばなかった比較）の件数を理由別に出す（全理由を 0 を含めて返す）。見送りは呼び出し数にも費用にも入らない。判断との対応は `jev_trial_compare` の `fairness.skipped`
- 日の区切りは JST（`created_at` を +9 時間して日付に直す）。上限と止まりの記録は `runtime_settings` から読み取りで取り、`limits` に出す（止まりは理由と日だけ。金額・時刻・ステータスは返さない）

## 5. 読み取りの上限

`jev_trial_decisions` の読み取りは 1 回の呼び出しで 100,000 行まで、結果の記録は `calibration_outcomes`・`counterfactual_labels` それぞれ 200,000 行まで、アームは 8 個まで、(ホライズン, ラベルの版) のスライスは 16 まで。超えたら部分結果を返さず `limit_exceeded`（422）にする（期間を狭める）。3 秒の呼び出しの締切（`query_timeout`）は他のツールと同じ。

## 6. 触れないもの

取引・Risk・発注・Paper ガード・資金・Policy の閾値・ライブの Jev・既存のシャドー・RAG・較正・メタモデルの学習の経路は読みも変えもしない。トライアルの行はこれらのどこにも入らず、このツールも出力を他の経路へ渡さない。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-13 | 新規 | issue #1060 |
