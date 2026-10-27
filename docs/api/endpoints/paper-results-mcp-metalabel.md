# API 仕様: Paper 実績 MCP のメタラベル診断ツール

`docs/api/endpoints/paper-results-mcp.md` §3.23〜の分割章（行数上限のため別ファイル）。issue #848・#849。共通の上限・トランスポート・成功ボディ `{"tool":"<名前>","data":{...}}` と期間（§1: JST・最大 366 日・既定は直近 30 日。`from` / `to` は両方か、どちらも省略）は元の文書、エラーの HTTP と `code` は `docs/architecture/paper-results-mcp-errors.md`、設計は `docs/architecture/paper-results-mcp.md` §5。どちらのツールも読み取り専用（接続は `mode=ro`）で、秘密・`jev_decisions.state_json` / `response_json` の中身は返さない。

## 3. ツール

### 3.23 `metalabel_training_readiness`

メタモデルの学習データがそろっているか（issue #848）。夜間の再学習（`internal/service/metalabel/training`）が実際に使う関数を同じ行に当てて数えるので、ここの数字は再学習の判定と一致する（実装の対応は `docs/architecture/paper-results-mcp.md` §5）。

引数: `from`, `to`（任意）。再学習が読む行は「直近 90 日（`lookback_days`）」なので、読む決定の窓は期間を 90 日に切り、期間の終わりか現在の早いほうまでにする（`rows_from` / `rows_to`）。**既定の直近 30 日だと再学習の窓より狭い**ので、`matches_nightly_retrain` が `false` になる。今夜の再学習が見る数字を知りたいときは、90 日以上を指定する（例: `from` = 90 日前、`to` = 明日）。

`data`:

| フィールド | 定義 |
|------------|------|
| `as_of` | 数えた時点（UTC RFC3339）。期間の終わりが過去ならその日の 00:00 JST、そうでなければ現在。分割（OOS 窓）はこの時点で行う |
| `rows_from` / `rows_to` | 学習行を読んだ `jev_decisions.timestamp` の窓 `[rows_from, rows_to)`（UTC RFC3339） |
| `lookback_days` | 再学習の読む日数（`training.DefaultLookback`。現在 90） |
| `matches_nightly_retrain` | 窓が今夜の再学習の窓と同じ（`rows_to` が現在で、`rows_from` が現在から 90 日前）か。`false` のときは窓が違う（数字は別の期間のもの） |
| `funnel[]` | 学習の条件を 1 つずつ足したあとに残る行数。`{step, condition, rows}`。`step` は固定コードで、この順: `trader_decisions`（`decision_type = 'trader'`）→ `long_short`（方向が LONG か SHORT）→ `label_v2`（`calibration_outcomes.label_version = 2` がある）→ `net_return`（`net_return_bps` あり。再学習が読む行＝学習行）→ `features_complete`（8 特徴量がそろう行＝`BuildRows` が残す行）。`condition` は日本語の説明。各行は前の行以下。1 つの判断に複数のラベルがあっても 1 件と数える |
| `features.training_rows` / `all_usable_rows` / `dropped_incomplete` | 学習行の件数、`BuildRows` が残す件数（**日付を問わない全部**。issue #913 で `usable_rows` から改名）、特徴量が欠けて捨てた件数（再学習のログの `dropped_incomplete` と同じ式）。`training_rows = all_usable_rows + dropped_incomplete`。見送りの記録の `usable_rows`（`last_retrain.latest_skip.usable_rows`・`meta_model_skips[]`・`retrain_skips[]`）は**別の数**で、OOS 窓より前の行数（`split.train.rows`。`too_few_rows_after_purge` では fit・較正の必要件数まで）。同じ名前でも `all_usable_rows` とは一致しない |
| `features.missing[]` | 特徴量ごとの欠け件数 `{feature, missing_rows}`。`metalabel.FeatureNames` の順（`ofi_short` / `ofi_mid` / `ofi_long` / `microprice_dev_bps` / `spread_bps` / `return_1m` / `jev_score` / `tdnet_score`）で、欠けが 0 件でも全部並べる。1 行が複数の特徴量を欠くので、合計は `dropped_incomplete` より大きくなりうる。`state_json` が読めない行は `spread_bps` と `return_1m`、`response_json` が読めない・採点できない行は `jev_score` に数える（`BuildRows` が捨てる理由はこれで全部） |
| `last_retrain.last_run` | 直近の再学習の結果: `skipped`（直近が見送り）/ `model_stored`（直近がモデルの保存）/ `none`（どちらの記録も無い）。期間によらない最新の記録 |
| `last_retrain.latest_skip` | 最新の `metalabel_retrain_skips`: `{skipped_at, reason_code, usable_rows, required_rows, dropped_incomplete}`。無ければ `null`。`dropped_incomplete` は見送りの行に残っている値で、**残っていなければ `null`**（0 ではない） |
| `last_retrain.latest_model_trained_at` | 最新の `metalabel_models.trained_at`（無ければ `null`） |
| `last_retrain.dropped_incomplete_status` | 見送りの `dropped_incomplete` の有無: `recorded`（`latest_skip.dropped_incomplete` に値がある）/ `not_recorded_in_schema`（この DB の `metalabel_retrain_skips` にその列が無い。列を足すマイグレーション（#862）より前）/`not_recorded_on_row`（列はあるが最新の見送りの値が `NULL`）/ `no_skip_recorded`（直近の再学習は見送りでなくモデルを保存した、または一度も走っていない）。いずれでも、今の学習行から数えた値は `features.dropped_incomplete` で読める |
| `excluded_prompt_versions[]` | 再学習が読まない Trader のプロンプトの版（ライブの版と別のコホート。issue #881）。`funnel` と学習行は、ライブの版と同じコホートの判断だけ。ライブの版が昇格で変わるとここも変わる |
| `prompt_cohort` | 除外の中身（issue #913）。`live_prompt_version`（ライブの版）、`latest_promotion`（`jev_prompt_promotions` の最新 1 行 `{at, from_version, to_version}`。無ければ `null`。接続元は返さない）、`excluded[]`（除外した版ごと、読んだ窓 `[rows_from, rows_to)` の trader の判断。窓に判断が無い版は載らない。最初の判断が古い順）: `{prompt_version, cohort, trader_decisions, long_short_rows, label_v2_rows, oldest_decision_date, newest_decision_date}`。`long_short_rows` は LONG/SHORT、`label_v2_rows` はそのうちトリプルバリアのラベルがあるもの（除外されなければ `funnel` に数えられたはずの行）、日付は最古・最新の判断の JST 日。`funnel` と学習行はライブのコホートだけなので、「学習行が 0」が別コホートの除外によるのか、`llm_cutoff` で OOS 側に寄ったためなのかは、ここと `split.oos_start_reason` で見分ける |
| `split.llm_cutoff` / `llm_cutoff_status` | 今の `metalabel.llm_cutoff`（`YYYY-MM-DD`）とその状態（`placeholder` / `unverified` / `confirmed`）。OOS 窓はこの日付より前に始まらない |
| `split.oos_start` | OOS 窓の始まり（UTC RFC3339） |
| `split.oos_start_reason` / `oos_floor` / `oos_floor_source` | OOS 窓の始点を決めているもの（issue #913）。`cutoff_floor`＝下限（`oos_floor`）以後の行が OOS の必要行数・日数に届かず、窓が `llm_cutoff` そのものから始まる（その手前の行だけが学習側になる）。`max_window`＝同じだが下限が最長の窓（`OOSMaxWindow`＝21 日前）。`min_rows`＝窓が必要行数ちょうどを含む位置から始まる。`min_days`＝新しい側の必要日数分の最初の行から始まる（行数は必要数より多い）。`oos_floor` は窓が遡れる限界（`llm_cutoff` と最長の窓のうち新しいほう）で、`oos_floor_source` はそのどちらか（`cutoff_floor` / `max_window`）。窓の決め方は `SplitWalkForward` と同じ規則を読むだけで、判定には使わない（一致は `metalabeldiag` のテストが全形で確認する） |
| `split.train_ready` / `train_skip_code` | `training.SplitWalkForward` が学習側（fit＋較正）を作れたか。作れないときの `train_skip_code` は再学習が記録するのと同じ固定コード（`too_few_rows_before_oos` / `too_few_rows_after_purge`）。作れたら `null` |
| `split.train` | 学習側: `rows`（分割が通れば fit＋較正の行数、通らなければ見送りが記録する `usable_rows`）、`required_rows`（`WalkForwardConfig` の `MinFit + MinCalibration`）、`fit_rows` / `calibration_rows`（分割が通らなければ `null`）、`required_fit_rows`（`MinFit`）、`required_calibration_rows`（`MinCalibration`） |
| `split.oos` | OOS 側: `rows`（OOS 窓の行数）、`required_rows`（採用条件 `AcceptanceConfig.MinOOSSamples`）、`days`（OOS 行の JST の取引日数）、`required_days`（`MinOOSDays`）、`required_takes`（`MinTakes`。参考で、take 数はモデルの出力に依るのでここでは判定しない）、`ready`（`rows` と `days` が必要件数以上） |
| `split.shortfall` | 学習・採用判定に足りない行数と、その解消の見積もり（issue #913）。`oos_window_rows`（`oos_floor` 以後の行数）、`oos_rows_missing`（OOS の必要行数に足りない数）、`purged_rows`（窓の境でパージされる行。label の窓が窓に届く行）、`train_rows_missing`（パージ後の窓の前の行が fit＋較正の必要件数に足りない数）、`additional_rows_needed`（両方を満たすのに要る新しい行数＝`max(OOS 必要行数 − oos_window_rows, OOS 必要行数 + fit・較正の必要件数 + purged_rows − 全 usable 行, 0)`。日数は含めない）。`rows_per_trading_day`（直近 `rate_trading_days`＝5 取引日の 1 日あたりの usable 行。行の無い日も数える）、`estimated_trading_days`（`additional_rows_needed` をその割合で割って切り上げ。不要なら 0、割合が 0 なら `null`。目安）、`oos_window_trading_days`（`oos_floor` から as-of までの取引日数）、`oos_min_rows_per_trading_day`（その日数で OOS の必要行数に届く 1 日あたりの下限。取引日が無ければ `null`）、`rate_reaches_oos_bar`（`rows_per_trading_day` がその下限以上か） |

必要件数（130・740・370・7 日など）はこのツールに書いていない。`DefaultWalkForwardConfig` と `DefaultAcceptanceConfig` の値をそのまま返すので、既定が変われば（#862）数字も変わる。学習側が足りている夜でも OOS 側が足りなければ、モデルは学習されても採用条件で却下される（`oos_samples_below_minimum`）。

上限: 窓の中の学習行が 20,000 行（`metalabeldiag.MaxReadinessRows`）を超えたら `limit_exceeded`（部分の数字は返さない。期間を狭める）。応答時間は索引検索 1 回＋プロセス内の組み立てで、30 日・90 日の計測は `metalabeltests/readiness_perf_test.go`（`go test -v` が所要時間と `EXPLAIN QUERY PLAN` を出す）。

返さない: `jev_decisions` の `state_json` / `response_json` と特徴量の値そのもの（特徴量名と件数だけ）、`calibration_outcomes` の価格・リターン、`metalabel_models` の中身。

### 3.24 `jev_models`

Jev が実際に応答しているモデル名（issue #849）。`metalabel.llm_cutoff` を正しく埋める（`metalabel.llm_cutoff_model` に確認したモデル名を記録する）ために、`jev_decisions.model_id`（Jev の応答の model。例 `jev-1.13.0`）を読む。モデルの指定は secrets の `JEV_MODEL` だけ（暗号化されていて読めない）で、バックエンドが変わると設定を変えなくても `model_id` が変わる。

引数: `from`, `to`（任意。§3.23 と同じ）。件数は期間 `[from, to)` の `jev_decisions.timestamp`（scout と trader の両方）で数える。`cutoff` だけは期間によらず、最新の判断で比べる。

`data`:

| フィールド | 定義 |
|------------|------|
| `models[]` | 期間内の `model_id` ごと: `{model_id, decisions, trader_decisions, first_seen, last_seen}`。`first_seen` が早い順。`first_seen` / `last_seen` は期間内でそのモデルが最初・最後に出た判断の時刻（JST、RFC3339 `+09:00`）。`trader_decisions` は `decision_type = 'trader'` の件数（再学習が使う判断） |
| `daily[]` | JST 日別の `model_id` ごとの件数 `{date, model_id, decisions}`（日付順、同じ日は `model_id` 順）。判断が 1 件も無い日は並べない |
| `cutoff.llm_cutoff_model` | 今の `metalabel.llm_cutoff_model`（未設定は空文字） |
| `cutoff.latest_model_id` / `latest_at` | 期間によらず、最新の判断（`timestamp`、同時刻なら `id` の大きいほう）の `model_id` と時刻（JST）。判断が 1 件も無ければ `null` |
| `cutoff.match` | `llm_cutoff_model` と `latest_model_id` の比較: `match`（完全一致）/ `mismatch`（違う）/ `unset`（どちらかが空で比べられない） |

比較は再学習の cutoff 判定（`internal/service/metalabel/training/cutoff.go` の `cutoffGate`）と**同じ関数**（`training.CutoffModelMatches` = 完全一致。前後の空白・大文字小文字・前方一致は同じとみなさない。`jev-1.13` と `jev-1.13.0` は `mismatch`）で行う。`training.CutoffModelStatus` が `match` / `mismatch` / `unset` を返し、`cutoffGate` も同じ `CutoffModelMatches` を使う。`mismatch` のときは、再学習が `llm_model_changed` で採用を止める（`TestJevModelsAgreesWithTheTrainersCutoffGate`）。

上限: 期間内の別々の `model_id` が 50 を超えたら `limit_exceeded`（`metalabeldiag.MaxJevModels`）。読むのは `jev_decisions` の `timestamp` の索引範囲検索（`jev_decisions_timestamp_idx`）と `model_id` の集計、最新 1 件、`runtime_settings` の `metalabel.*` キーだけ。

返さない: `secrets` の値（`JEV_MODEL` の暗号化された値も読まない）、`state_json` / `response_json`、価格・数量。`runtime_settings` は `metalabel.*` 以外のキーを SQL の前に拒否する。

`JEV_MODEL` のモデル**名**は、#885（マイグレーション `000048`）から `jev_model_changes` に平文で残る（Settings 画面で切り替えたとき。モデル名は秘密情報ではない）。この記録は `experiment_compare` の `confounders[].values` に出ることがあるが、`jev_models` はこの表を読まず、返すのは `jev_decisions` から求めたモデル ID（`model_id`）と `runtime_settings` の `metalabel.llm_cutoff_model` だけである。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-21 | 新規作成。§3.23 `metalabel_training_readiness` の入出力と、再学習の判定との一致・既定の窓・上限・`dropped_incomplete` が残っていない場合の表し方 | issue #848 |
| 1.1 | 2026-10-21 | §3.24 `jev_models` を追加（`model_id` ごとの件数・初回/最後の時刻・日別・`llm_cutoff_model` との完全一致の判定） | issue #849 |
| 1.2 | 2026-10-23 | §3.24 `jev_models` の「返さない」を実装に合わせて訂正。「`JEV_MODEL` を含む秘密は DB にも無い」は #885 以降は誤りで、モデル名は `jev_model_changes` に平文で残る（`secrets` の値は返さない。`jev_models` はこの表を読まず `jev_decisions` 由来のモデル ID だけを返す） | issue #885 |
| 1.3 | 2026-10-22 | §3.23 に `excluded_prompt_versions` を追加。`funnel` と学習行は、再学習と同じくライブの Trader の版と同じコホートの判断だけを数える | issue #881 |
