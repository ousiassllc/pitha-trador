# API 仕様: Paper 実績 MCP の Jev プロンプトのツール（§3.29）

`paper-results-mcp.md`（API 仕様）の分割章（ファイルの行数上限のため。章番号は元の文書の §3.29）。Jev の trader のプロンプトの版と、候補の版をライブと並べて動かす「シャドー」の比較・費用・昇格の 3 ツールを定める（issue #872）。共通の引数（期間 §1）・上限（§2）・エラー（`paper-results-mcp-errors.md`）は元の文書と同じ。仕様の背景は `architecture/paper-results-mcp.md` §5.1・§7.3。

シャドーを**有効にする・候補の版を選ぶ・費用の上限を決める**ツールは無い（Settings 画面だけ。`settings_set` のホワイトリストにも無い）。**1 日の費用の上限（既定 0.10 USD。Settings 画面で上げ下げできる）と 1 日の失敗回数の上限（20 回。定数）はユーザー決定の確定値**である（`internal/config/jevshadowsettings.DefaultDailyCostCapUSD` / `DailyFailureCap`）。単価（1M 入力トークンあたり 0.042 USD）と同時実行数（4）も確定値だが、費用は公表単価による**見積り**で請求書とは突き合わせていない。

## 3.29.1 `jev_shadow_compare`（読み取り専用）

引数: 期間（`from` / `to`。§1）、`prompt_version`（省略可。候補の版の名前。省略は存在するすべての版）。`data.versions[]`（候補の版ごと。最大 10 版、超えたら `limit_exceeded`）:

| フィールド | 定義 |
|------------|------|
| `prompt_version` | 候補の版（`jev_shadow_decisions.prompt_version`） |
| `pairs` | 期間内（ライブの判断の `jev_decisions.timestamp`）の、ライブの trader の判断とシャドーの判断の組の数 |
| `live_versions[]` | その組のライブの判断が使った版（`jev_decisions.question_version`）ごとの組の数（`version`・`pairs`） |
| `entry_quality[]` | `poor` / `fair` / `good` / `strong` / `exceptional` の 5 行（件数 0 でも常に出す）。各行は `live` と `shadow` の `count` と `share`（`count / pairs`。組が 0 なら `null`）。ライブの `entry_quality` は `jev_decisions.response_json` の `entry_quality`（`jev_decisions` に `entry_quality` の列は無い） |
| `direction_agreement` | `agree`（シャドーとライブの `direction` が同じ組の数）と `rate`（`agree / pairs`） |
| `cost` | `calls`（= `pairs`）・`request_cost_usd`（その組のシャドーの `request_cost` の合計）・`avg_latency_ms` |
| `outcomes[]` | `calibration_outcomes` を `jev_decision_id` で結んだ結果（`JevCalibration` と同じ結び方）。`horizon_minutes` と `label_version` ごと（ラベル版は混ぜない。FR-CAL-4）に `by_entry_quality[]`（5 行）。各行は `live` と `shadow` の結果の統計 |

`data.outcome_note`（固定の文）: 結果の読み方の注意。`calibration_outcomes` はライブの方向に対するラベルで `future_return` はライブのバリアで止まるため、**方向がライブと食い違う行は採点しない**（偏る）。

結果の統計（`live` / `shadow` は同じ形。計算方法は `architecture/paper-results-mcp.md` §5.1）:

| フィールド | 定義 |
|------------|------|
| `samples` | `calibration_outcomes` の行がある組の数 |
| `directional` | そのうち、その側の方向が `NONE` でない数 |
| `comparable` | `directional` のうち採点できる行の数（その側の方向がライブと同じ、またはライブが `NONE`。ライブの終端が結果を決めない行） |
| `excluded_disagreement` | `directional` − `comparable`。方向がライブの方向と食い違い、`future_return` がライブのバリアで止まるため採点しない行（ライブ側は常に 0） |
| `hit_rate` | `comparable` のうち、その側の方向で符号を付けた `future_return` が 0 より大きい割合。ライブ側にも同じ式を使う |
| `avg_directional_return_pct` | `comparable` の、その側の方向で符号を付けた `future_return` の平均（%。コストを引かない総リターン） |
| `recorded_samples` | ラベルに記録された値が、その側の方向にも当てはまる行の数（その側の方向がライブの方向と同じ行だけ） |
| `recorded_hit_rate` | その行の `was_direction_correct`（コスト控除後）の割合 |
| `recorded_barrier_counts` | その行の `barrier_hit` の件数（`label_version` 2 だけ） |
| `recorded_avg_net_return_bps` | その行の `net_return_bps` の平均（`label_version` 2 だけ） |

返さないもの: `response_json`・`state_json` の本文（`entry_quality` の 1 文字列だけを取り出す）。シャドーは取引に使われず、この結果は**ラベルから計算し直した比較**であって、シャドーの取引の損益ではない（シャドーは取引しない）。

## 3.29.2 `jev_shadow_cost`（読み取り専用）

引数なし。`data`:

| フィールド | 定義 |
|------------|------|
| `enabled` | シャドーの有効（`runtime_settings` の `jev.shadow.enabled`）。既定 `false` |
| `candidate_version` | 候補の版（`jev.shadow.candidate_version`）。無ければ `null` |
| `live_version` | ライブの版（`jev.trader_prompt_version`。保存が無い・壊れている・このビルドに無い版のときはビルドの既定 `trader-v3`） |
| `daily_cap_usd` / `cap_is_default` / `cap_source` | 1 日あたりの費用の上限（JST の日）。`cap_source` は `default`（保存値が無く、効いているのはビルドの既定値 0.10 USD。`cap_is_default` が `true`）か `stored`（Settings 画面で保存された値）。どちらも確定した運用の値で、Settings で上げ下げできる |
| `failure_cap` / `failure_cap_source` / `failures_today` | 1 日（JST）に許す失敗した呼び出し（応答が不正・保存の失敗・タイムアウトなど。課金されても費用の合計に入らない。終了（`Shadow.Close`）の取り消しで途中になった呼び出しは数えない。ベンダーが課金した可能性はあるが保存されないので費用にも入らず、1 回の終了で最大 4 回）の回数の上限と、今日の回数。`failure_cap_source` は常に `fixed`（確定した定数 20 回。Settings では変えられない） |
| `today` | JST の今日の `date`・`calls`・`request_cost_usd`・`estimated_calls`・`estimated_cost_usd`（後ろの 2 つは「うち見積り」＝応答に `usage` が無く、入力トークン数をリクエストの大きさから推定して数えた呼び出しの件数と費用。`calls` / `request_cost_usd` に含まれる） |
| `total` | 累計の `calls`・`request_cost_usd`・`estimated_calls`・`estimated_cost_usd` |
| `daily[]` | JST の日ごとの費用（`date`・`calls`・`request_cost_usd`・`estimated_calls`・`estimated_cost_usd`）。新しい順、最大 31 日 |
| `halt_today` / `last_halt` | 上限で止まった記録（`date`・`reason`（固定コード `daily_cost_cap` または `daily_failure_cap`）・`spent_usd`・`cap_usd`・`failures`・`failure_cap`（後ろの 2 つは `daily_failure_cap` のとき）・`at`）。止まっていなければ `null` |
| `throttle_active` / `last_throttle` / `throttles_today` / `throttle_cooldown_seconds` / `min_call_interval_seconds` | Jev API が 429（レート制限）・529（過負荷）を返したことによるシャドーの停止（issue #883）。シャドーはライブと同じ API キー・同じ制限を共有するので、429/529 を受けたら待たず・再試行せずに止まり、`throttle_cooldown_seconds`（900 秒。定数）のあいだ何も送らない。ライブの呼び出しが受けた 429/529 でも止まる。`throttle_active` は今止まっているか、`last_throttle` は最後の記録（`date`・`count`＝その日の停止回数・`status`（429 / 529）・`reason`（固定コード `api_rate_limited` / `api_overloaded`）・`source`（`shadow`＝シャドー自身の呼び出し／`live`＝ライブの呼び出し）・`at`・`until`＝再開の時刻。無ければ `null`）、`throttles_today` は今日（JST）の停止回数。`min_call_interval_seconds`（2 秒。定数）はシャドーの呼び出しの最小間隔（それより短い比較は捨てる）。失敗回数・`halt_*` には数えない |
| `recent_promotions[]` | `jev_prompt_promotions` の新しい順、最大 10 件（`id`・`at`・`actor`（`mcp` / `gui`）・`client`（IP だけ）・`from_version`・`to_version`） |
| `registered_versions[]` | このビルドが定義する trader の版（昇格できる版） |

`request_cost` はシャドーの呼び出しだけの合計である。API は課金額を返さないので、アプリが入力トークン数に公表単価（Jev は 1M 入力トークンあたり 0.042 USD、出力は無料。2026-10-22 時点の `docs.typesafe.ai/models`）を掛けた**見積り**で、請求書と突き合わせた値ではない（単価と上限の数字は確定、見積りが請求と一致することは未確認）。応答に `usage` が無い呼び出しは **0 USD と数えず**、リクエスト本文のバイト数を保守的な比（2 バイト = 1 トークン）で割った入力トークン数の推定で費用に入れ、`estimated_calls` / `estimated_cost_usd` に分けて見せる（0 と数えると 1 日の費用の上限をすり抜けるため）。ライブの判断の `jev_decisions.request_cost` は従来どおり常に NULL で、この合計に含まれない。

## 3.29.3 `jev_prompt_promote`（書き込み）

引数: `version`（必須。`registered_versions[]` の 1 つ）。ライブの trader のプロンプトの版を `version` に切り替える。`runtime_settings` の `jev.trader_prompt_version` の upsert と、`jev_prompt_promotions` への監査 1 行を**同じトランザクション**で書く。処理の順:

1. 認証（Bearer。失敗なら SQL 0 件）
2. 引数の形（`version` が空・長すぎる・空白や制御文字を含む → `invalid_argument`）
3. 書き込み許可のスイッチ（オフなら `write_disabled`。`settings_set` と同じスイッチ。`architecture/paper-results-mcp.md` §7.2）
4. 版の検査（登録されていない版・すでにライブの版 → `invalid_value`。`error.data.detail` に理由）
5. 保存

**昇格のあとの集計・学習**（issue #881）: 昇格すると、RAG の事例・Calibration の指標と PnL・メタモデルの学習・`jev_calibration` は、新しいライブの版と同じコホートの判断だけを使い、貯め直す（旧版の行は消えず、戻せば再び使われる）。本番での昇格は、この扱いを含むビルドが本番で動くまで人が見送る。Policy の信頼度帯の標本数の確認も同じコホートで数え（新しい版の標本が足りない間、Paper だけ全版の合計で判断してよく、判断記録 `trade_signals.reject_reason` に `calibration_all_versions` を残す。Live は戻さず `not_calibrated`）、採用中のメタモデルは学習に使ったコホートがライブの版と違えば、Paper は警告して使い（`meta_model_cohort_mismatch`）、Live は使わない（`not_calibrated`）（`architecture/jev-prompt-versions-in-data.md` §4）。

応答: `promotion_id`・`from_version`・`to_version`・`at`。監査の「誰が」は `mcp` と接続元ホスト（IP だけ。ヘッダ・API キーは残さない）。このツールは**自動では呼ばれない**。自己改善ループ（Sol/Opus）にはこの経路が無い（`domain.ValidatePolicyChanges` は `policy.*` のキーだけを受け付け、Jev の版・シャドーのキーは拒否する。テストが固定する）。Settings 画面にも同じ操作がある（`POST /settings/jev-prompt/promote`。セッション Cookie と CSRF が必要）。

## 受け入れ条件

- 3 ツールは `tools/list` に出る。`jev_shadow_compare` / `jev_shadow_cost` は `readOnlyHint: true`、`jev_prompt_promote` は `false`
- 読み取り接続（`mode=ro`）のまま 2 つの読み取りが動き、書き込みの SQL を発行しない
- `jev_shadow_compare` は、同じ入力の組について `entry_quality` の分布（5 水準・件数と割合）・`direction` の一致率・シャドーの費用・`calibration_outcomes` による結果を、ライブとシャドーで並べて返す。シャドーの方向が違う行の結果は、`future_return` とシャドーの方向から計算し直す
- `jev_shadow_cost` は、JST の日の境界で今日・日ごと・累計を集計し、上限（確定値・値の出どころ）・うち見積り・止まった記録を返す
- `jev_prompt_promote` は、書き込み許可がオフなら `write_disabled` で何も書かず、登録されていない版・すでにライブの版を `invalid_value` で拒否し、成功したときだけ設定 1 行と監査 1 行を同じトランザクションで書く。ほかの表・文を発行しない（`shadowtests` が実行された文を固定する）

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-22 | 新規作成。`jev_shadow_compare` / `jev_shadow_cost` / `jev_prompt_promote`（元の文書の版 1.19 で追加） | issue #872 |
| 1.1 | 2026-10-22 | `jev_shadow_compare` に `comparable` / `excluded_disagreement` / `outcome_note` を追加し、方向がライブと食い違う行を採点しない形に改めた（`future_return` がライブのバリアで止まる偏りを避けるため）。`jev_shadow_cost` の `daily_cap_approved` を `cap_source` に置き換え、失敗回数の上限（`failure_cap` ほか）と `daily_failure_cap` の止まった記録を追加 | issue #872 レビュー対応 |
| 1.2 | 2026-10-22 | 費用の上限 0.10 USD・失敗回数の上限 20 回・単価 0.042 USD/1M 入力トークン・同時実行 4 をユーザー決定の確定値にし、`cap_source` の値を `default` / `stored`、`failure_cap_source` を `fixed` に改めた（`proposed_unapproved` を廃止）。`usage` が無い応答を推定の入力トークン数で数え、`jev_shadow_cost` に `estimated_calls` / `estimated_cost_usd` を追加 | issue #872 ユーザー決定 |
| 1.3 | 2026-10-22 | `jev_shadow_cost` に `throttle_active` / `last_throttle` / `throttles_today` / `throttle_cooldown_seconds` / `min_call_interval_seconds` を追加（Jev API の 429/529 によるシャドーの停止と理由。ツール数は変わらない） | issue #883 |
| 1.4 | 2026-10-22 | `jev_shadow_cost` の失敗回数の説明に、終了（`Shadow.Close`）の取り消しで途中になった呼び出しは数えないことを追記 | issue #882 |
| 1.5 | 2026-10-22 | §3.29.3 に、昇格のあとの集計・学習の扱い（ライブの版と同じコホートだけを使う）と、本番での昇格を見送る範囲を追記。Policyの標本数とメタモデルの扱いを決定済みに | issue #881 |
