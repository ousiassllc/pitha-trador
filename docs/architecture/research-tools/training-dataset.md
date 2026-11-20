# データ仕様: 学習用データセット（training_dataset_export）

issue #1094。Paper 実績 MCP の `training_dataset_export` が書き出す**学習用データセット**の仕様（スキーマの版・列・ラベルの版・分割の規則・manifest・除外・ハッシュの確かめ方）。ツールの引数と出力は `docs/api/endpoints/research-tools/paper-results-mcp-dataset-export.md`、設計・安全の担保は `docs/architecture/research-tools/paper-results-mcp-dataset-export.md`。実装は `internal/service/datasetexport`、列の許可リストは同パッケージの `Columns`（このファイルの列の表とテストで突き合わせる）。

## 1. 位置づけ

Jev の判断（1 判断 = 1 行）に、判断時点の特徴量・Jev の構造化された答え・ゲート結果・各種ラベル・シャドーとお試しの答えを付けた CSV と Parquet。**研究・学習の素材**で、取引の入力ではない。ファイルはこの PC のローカルフォルダにだけ書き、外部へは送らない。秘密・生の板・気配・価格水準・応答の本文は含まない（§8）。ラベルは判断の**後**の値動きから付いた将来の情報なので、学習の入力（特徴量）にしてはいけない。

## 2. スキーマの版

`schema_version` = `"2"`（版 1 は CSV だけで、`dataset_id` が期間・分け方を含まなかった）。列の追加・削除・並びや意味の変更、ファイル構成（Parquet の型・書き込みの設定を含む）やハッシュの仕様の変更は版を上げる。版が変わると `dataset_sha256` の入力も変わるので、`dataset_id` も変わる。

## 3. 行

- 期間 `[from, to)`（JST の日）の `jev_decisions` のうち `decision_type = 'trader'` の**全判断**（`NONE` を含む。scout は含まない）。1 判断 = 1 行。1 回に 20000 判断まで（超えたら `limit_exceeded`）。
- 並びは `decided_at`、次に `decision_id` の昇順（決定的）。同じ入力は同じバイト列になる。
- CSV は UTF-8・改行 `\n`・ヘッダー 1 行。数値は 10 進（指数表記なし）、空欄は欠測（NULL・該当なし）。値が想定の型・列挙の外なら空欄にする（本文や自由文は混ざらない）。
- ファイル: 分割なしは `dataset.csv` と `dataset.parquet`、分割ありは `dataset_train.csv`・`dataset_train.parquet`・`dataset_eval.csv`・`dataset_eval.parquet`。すべて `manifest.json` と同じフォルダ `exports/<dataset_id>/` に置く。Parquet は同じ側の CSV と**同じ行・同じ列・同じ順**（行数が一致する）。

### Parquet の形式（issue #1124）

- ライブラリは `github.com/parquet-go/parquet-go`（v0.26.4。Apache-2.0、cgo なしの Go。amd64 の一部にアセンブラがあるが `purego` タグで純 Go に切り替えられる。Windows でも `go build` / `go vet` が通る。依存は `klauspost/compress`・`andybalholm/brotli`・`pierrec/lz4`・`parquet-go/bitpack`・`parquet-go/jsonlite` で、すでに入っている `google/uuid` 以外は新規）。書き込みは `internal/service/datasetexport/parquetenc` だけが行い、他のパッケージはこのライブラリを import しない（`TestOnlyTheParquetWriterImportsTheParquetLibrary`）。
- 列はすべて optional（空欄 = null）。型は列ごとに固定: `decision_id` `instrument_id` `signal_count` `cf_horizon_minutes` と 0 / 1 のフラグ（`has_bar` `has_signal` `risk_passed` `cf_cutoff_unverified`）は INT64、SQL 側で数値として検査済みの列（特徴量・ラベル・確信度・OFI など）は DOUBLE、それ以外（識別子・日付・列挙・版名）は UTF-8 の BYTE_ARRAY。0 / 1 のフラグを BOOLEAN にしないのは、ライブラリが BOOLEAN のページのビットを詰めるときに最後のバイトの未使用ビットを使い回しのバッファの値のまま残し、同じ入力でもバイト列が変わることがあるため（実測で確認）。型に合わない値や NaN / Inf はエラーにし、黙って別の値にしない。
- DOUBLE は CSV の 10 進表記を `ParseFloat` で読んだ値で、CSV と数値として一致する。`decided_at` は CSV と同じ UTF-8 の文字列（タイムスタンプ型にしない。ナノ秒を落とさず、型の解釈の差を作らない）。
- 決定的なバイト列: 書き込みの設定をすべて固定する（圧縮なし、データページ v2、1 行グループ 10000 行、統計あり、ページのバッファは呼び出しごとに新しいもの（共有のプールを使わない）、`created_by` は固定文字列、key-value メタデータは `pitha_dataset_schema_version` だけ）。ファイルに時刻・乱数・ホスト名・ライブラリの版は入れない。行グループが複数になる大きさでも同じバイト列になる（`TestDatasetSchemaIsDeterministicAndKeepsColumnOrder`）。ライブラリの更新でバイト列が変わりうるので、更新するときはこのテストと manifest のハッシュの扱いを確認し、変わるなら `schema_version` を上げる。

## 4. 列

列の順序は下の表の順（グループの並び）で、`manifest.columns`・各 CSV のヘッダー・各 Parquet の列が同じ。許可リストにない列は書かない。

### 識別子と分割

| 列 | 意味 | 元 | 単位・値 |
|---|---|---|---|
| `decision_id` | 判断の ID（1 行 = 1 判断） | jev_decisions.id |  |
| `instrument_id` | 銘柄 ID | jev_decisions.instrument_id |  |
| `symbol` | 銘柄コード | jev_decisions.symbol |  |
| `decided_at` | 判断の時刻 | jev_decisions.timestamp | UTC RFC3339（ナノ秒 9 桁固定） |
| `decision_day` | 判断の日（split の境界はこの日で決める） | jev_decisions.timestamp | JST の日付 YYYY-MM-DD |
| `question_version` | Jev のプロンプトの版 | jev_decisions.question_version |  |
| `model_id` | 応答したモデル | jev_decisions.model_id |  |
| `split` | 分割の区分（分割なしは all） | 分割の規則 | train / eval / all |

### Jev の答えと判断時点の値（jev_decisions）

| 列 | 意味 | 元 | 単位・値 |
|---|---|---|---|
| `answer_direction` | Jev の答え: 方向（LONG / SHORT / NONE） | jev_decisions.direction |  |
| `answer_confidence` | Jev の答え: 方向への確信度（検証済みの確率ではない） | jev_decisions.confidence | 0〜1 |
| `answer_regime` | Jev の答え: 相場の型（TREND / RANGE / BREAKOUT / CHAOTIC） | jev_decisions.response_json $.regime |  |
| `answer_entry_quality` | Jev の答え: エントリーの質（poor〜exceptional） | jev_decisions.response_json $.entry_quality |  |
| `answer_toxic_flow` | Jev の答え: 有害な約定の流れである確率 | jev_decisions.response_json $.toxic_flow | 0〜1 |
| `answer_liquidity_stressed` | Jev の答え: 流動性が逼迫している確率 | jev_decisions.response_json $.liquidity_stressed | 0〜1 |
| `answer_continuation_probability` | Jev の答え: 動きが続く確率 | jev_decisions.response_json $.continuation_probability | 0〜1 |
| `data_age_ms` | 判断時点の市場データの古さ | jev_decisions.data_age_ms | ミリ秒 |
| `market_regime` | 判断時点の相場状態（市場指数から判定） | jev_decisions.market_regime |  |
| `ab_arm` | 判断した A/B アーム | jev_decisions.ab_arm |  |
| `regime_volatility_bps` | 相場状態の判定に使った市場指数のボラティリティ | jev_decisions.regime_volatility_bps | bps |
| `regime_autocorr` | 相場状態の判定に使った自己相関 | jev_decisions.regime_autocorr | -1〜1 |
| `vwap_z` | 平均回帰レジームでの VWAP 乖離の z スコア | jev_decisions.vwap_z | z スコア |
| `ofi_short` | 判断時点の Order Flow Imbalance（短い窓。買い圧力が正） | jev_decisions.ofi_short | 株 |
| `ofi_mid` | 同（中の窓） | jev_decisions.ofi_mid | 株 |
| `ofi_long` | 同（長い窓） | jev_decisions.ofi_long | 株 |
| `ofi_window_short_seconds` | ofi_short の窓の幅 | jev_decisions.ofi_window_short_seconds | 秒 |
| `ofi_window_mid_seconds` | ofi_mid の窓の幅 | jev_decisions.ofi_window_mid_seconds | 秒 |
| `ofi_window_long_seconds` | ofi_long の窓の幅 | jev_decisions.ofi_window_long_seconds | 秒 |
| `microprice_dev_bps` | マイクロプライスの中値からの乖離（板の生の値は出さない） | jev_decisions.microprice_dev_bps | bps |

Jev の答えは `response_json` の**固定の 5 キー**だけを `json_extract` で取り出す（本文そのものは読まない）。`answer_direction` / `answer_confidence` は同じ値の `jev_decisions` の列から取る。壊れた JSON・型違い・列挙の外の値は空欄。

### 判断の足の派生特徴量（market_snapshots）

| 列 | 意味 | 元 | 単位・値 |
|---|---|---|---|
| `spread_bps` | 気配のスプレッド | market_snapshots.spread_bps | bps |
| `return_1m` | 1 分リターン | market_snapshots.return_1m | 比（0.01=+1%） |
| `return_3m` | 3 分リターン | market_snapshots.return_3m | 比 |
| `return_5m` | 5 分リターン | market_snapshots.return_5m | 比 |
| `return_15m` | 15 分リターン | market_snapshots.return_15m | 比 |
| `return_30m` | 30 分リターン | market_snapshots.return_30m | 比 |
| `price_vs_vwap_bps` | VWAP からの乖離 | market_snapshots.price_vs_vwap_bps | bps |
| `vwap_slope` | VWAP の 5 分変化率 | market_snapshots.vwap_slope | 比 |
| `vwap_cross_direction` | 前の足から VWAP を上抜け（1）・下抜け（-1）・なし（0） | market_snapshots.vwap_cross_direction | -1 / 0 / 1 |
| `volume_ratio_1m` | 1 分出来高の平常比 | market_snapshots.volume_ratio_1m | 倍 |
| `volume_ratio_5m` | 5 分出来高の平常比 | market_snapshots.volume_ratio_5m | 倍 |
| `high_distance_5m` | 直近 5 分高値からの距離（0 以下） | market_snapshots.high_distance_5m | 比 |
| `low_distance_5m` | 直近 5 分安値からの距離（0 以上） | market_snapshots.low_distance_5m | 比 |
| `session_high_distance` | 当日高値からの距離 | market_snapshots.session_high_distance | 比 |
| `session_low_distance` | 当日安値からの距離 | market_snapshots.session_low_distance | 比 |
| `atr_1m` | 1 分 ATR | market_snapshots.atr_1m | 円 |
| `atr_5m` | 5 分 ATR | market_snapshots.atr_5m | 円 |
| `realized_vol_5m` | 5 分の実現ボラティリティ | market_snapshots.realized_vol_5m | 比 |
| `realized_vol_15m` | 15 分の実現ボラティリティ | market_snapshots.realized_vol_15m | 比 |
| `volatility_expansion_ratio` | ボラティリティの拡大率 | market_snapshots.volatility_expansion_ratio | 倍 |
| `orderbook_imbalance` | 板の数量の偏り（板の生の数量は出さない） | market_snapshots.orderbook_imbalance | -1〜1 |
| `buy_trade_ratio` | 直近 5 分の買い約定の割合 | market_snapshots.buy_trade_ratio | 0〜1 |
| `sell_trade_ratio` | 直近 5 分の売り約定の割合 | market_snapshots.sell_trade_ratio | 0〜1 |
| `trade_flow_imbalance` | 買い約定と売り約定の偏り | market_snapshots.trade_flow_imbalance | -1〜1 |
| `market_return_1m` | 市場指数の 1 分リターン | market_snapshots.market_return_1m | 比 |
| `market_return_5m` | 市場指数の 5 分リターン | market_snapshots.market_return_5m | 比 |
| `sector_return_5m` | 同業種指数の 5 分リターン | market_snapshots.sector_return_5m | 比 |
| `stock_vs_sector_relative_strength` | return_5m − sector_return_5m | market_snapshots.stock_vs_sector_relative_strength | 比 |
| `market_breadth` | 値上がり・値下がり銘柄数の偏り | market_snapshots.market_breadth | -1〜1 |
| `volume_1m` | 直近 1 分の出来高 | market_snapshots.volume_1m | 株 |
| `volume_5m` | 直近 5 分の出来高 | market_snapshots.volume_5m | 株 |
| `turnover_1m` | 直近 1 分の売買代金 | market_snapshots.turnover_1m | 円 |
| `turnover_5m` | 直近 5 分の売買代金 | market_snapshots.turnover_5m | 円 |
| `special_quote` | 特別気配か | market_snapshots.special_quote | 0 / 1 |
| `lendable` | 貸借銘柄か（空は不明） | market_snapshots.lendable | 0 / 1 |
| `price_limit` | ストップ高（up）・ストップ安（down）。空は該当なし | market_snapshots.price_limit |  |
| `has_bar` | 判断の足があるか（0 のとき足の特徴量は空） | market_snapshots（instrument_id と timestamp が判断と同じ足） | 0 / 1 |

足は `(instrument_id, timestamp)` が判断と同じ `market_snapshots` の 1 行（`has_bar` = 0 なら特徴量は空。90 日の保持で足が消えた判断など）。ここに出すのは派生特徴量だけで、生の板・気配・価格水準は出さない（§8）。

### 銘柄マスタ（instruments）

| 列 | 意味 | 元 | 単位・値 |
|---|---|---|---|
| `sector` | 業種（銘柄マスタ） | instruments.sector |  |
| `asset_class` | 資産クラス | instruments.asset_class |  |

### ゲート結果（trade_signals）

| 列 | 意味 | 元 | 単位・値 |
|---|---|---|---|
| `has_signal` | この判断に取引候補（trade_signals）があるか | trade_signals.jev_decision_id | 0 / 1 |
| `signal_count` | この判断の取引候補の数 | trade_signals | 件 |
| `risk_passed` | どれかの候補が Policy・Risk を通ったか（エントリーした） | trade_signals.risk_passed | 0 / 1 |
| `reject_code` | 最新の候補が通らなかったときの理由の固定コード（自由文は出さない。未知は other） | trade_signals.reject_reason の先頭の語 | 固定コード |
| `origin` | エントリーした判断か、見送りの種類か（calibration_outcome_origins と同じ規則） | trade_signals と jev_decisions.direction | entered / risk_rejected / guard_skipped / no_signal / no_direction |

`reject_code` は `trade_signals.reject_reason`（自由文を含む）の先頭の語から決めた固定コード（`Paper 実績 MCP` の `risk_rejections` と同じ `RejectCode`。Risk Engine の却下は `risk_engine_rejected`、知らない理由は `other`）。自由文・数値は出さない。`origin` は `calibration_outcome_origins` ビューと同じ規則（`no_direction` → `entered` → `no_signal` → `risk_rejected` → `guard_skipped`）。

## 5. ラベルの版

ラベルは 3 つの出どころをそのまま並べる（再計算しない）。どれも**判断より後**の値動きで、判断に対応するラベルが無ければ空欄。manifest の `label_version` に版とホライズンを書く。

### 5.1 保存済みの calibration（label_version 2、トリプルバリア）

`calibration_outcomes` の `label_version = 2` の行（コスト控除後 `net_return_bps`、先に触れたバリア `barrier_hit`）をホライズン別に並べる。ホライズンは固定の 5・10・15・30・60 分（ラベラーのタイムアウトは Settings の 1〜60 分で、既定 10 分）。この 5 つ以外のホライズンで保存された行は列がなく、`manifest.label_version.calibration.rows_at_other_horizons` に件数だけ出す。

| 列 | 意味・単位 |
|---|---|
| `calib_h5_net_bps` `calib_h10_net_bps` `calib_h15_net_bps` `calib_h30_net_bps` `calib_h60_net_bps` | そのホライズンのコスト控除後リターン（bps）。元は `calibration_outcomes.net_return_bps` |
| `calib_h5_barrier` `calib_h10_barrier` `calib_h15_barrier` `calib_h30_barrier` `calib_h60_barrier` | 先に触れたバリア `tp` / `sl` / `timeout`。元は `calibration_outcomes.barrier_hit` |

### 5.2 研究用の固定ホライズン（label_version 1）

`research_horizon_labels`（#1074。固定ホライズンで決済、TP/SL なし）を、判断の足（`instrument_id` と `snapshot_at = 判断の timestamp`）で結ぶ。ホライズンは `30m` / `60m` / `close`（同じ JST 日の最後の足）。LONG・SHORT の両側のコスト控除後リターン（bps）を出す（判断の方向の側だけではない）。

| 列 | 意味・単位 |
|---|---|
| `rh_30m_long_net_bps` `rh_30m_short_net_bps` | 30 分ホライズンで LONG / SHORT したときの `long_net_bps` / `short_net_bps` |
| `rh_60m_long_net_bps` `rh_60m_short_net_bps` | 同 60 分 |
| `rh_close_long_net_bps` `rh_close_short_net_bps` | 同 引け |

### 5.3 見送り候補の反実仮想（label_version 2）

| 列 | 意味 | 元 | 単位・値 |
|---|---|---|---|
| `cf_horizon_minutes` | 見送り候補の反実仮想ラベルのホライズン（見送り以外は空） | counterfactual_labels.horizon_minutes | 分 |
| `cf_net_bps` | もしエントリーしていたときのコスト控除後リターン | counterfactual_labels.net_return_bps | bps |
| `cf_barrier` | 同 先に触れたバリア | counterfactual_labels.barrier_hit | tp / sl / timeout |
| `cf_skip_kind` | 見送りの種類 | counterfactual_labels.skip_kind | risk_rejected / guard_skipped / no_signal |
| `cf_cutoff_unverified` | 記録時の LLM cutoff が未確認だったか（1 は学習・評価に使わない扱い） | counterfactual_labels.cutoff_unverified | 0 / 1 |

`counterfactual_labels`（#1052）は、ガードで見送った LONG/SHORT のうちラベルが付かなかったものに「入っていたら」を残した表。判断に複数行（ホライズン違い）があれば最新の行（`id` 最大）を使う。理由の自由文（`reject_reason`）は出さない。

## 6. シャドーとお試しの答え

判断ごとの `jev_shadow_decisions`（候補の版ごと）と `jev_trial_decisions`（アームごと）を、固定の個数の**スロット列**に並べる。本文（`response_json`）・失敗コード・エラー抜粋は出さない。

- シャドー: 期間内の件数の多い順（同数は版名の昇順）に**最大 3 版**。スロット `n`（1〜3）の列は `shadowN_version`・`shadowN_direction`・`shadowN_confidence`・`shadowN_entry_quality`。
- お試し: 同じ順で**最大 4 アーム**。スロット `n`（1〜4）の列は `trialN_arm_id`・`trialN_status`（`ok` / `failed` / `refused` / `partial`）・`trialN_direction`・`trialN_confidence`。
- どのスロットがどの版・アームかは `manifest.shadow_versions` / `manifest.trial_arms`（スロット順）。上限を超えた版・アームの数は `shadow_versions_omitted` / `trial_arms_omitted`。行の `shadowN_version` / `trialN_arm_id` にも名前が入る。
- 版・アームの順は期間ごとに変わりうるので、データセットをまたいで比べるときは名前の列で突き合わせる。

列の一覧（意味は上の説明のとおり。元は `jev_shadow_decisions` の `prompt_version` / `direction` / `confidence` / `entry_quality`、`jev_trial_decisions` の `arm_id` / `status` / `direction` / `confidence`）:

| スロット | 列 |
|---|---|

| シャドー 1 | `shadow1_version` `shadow1_direction` `shadow1_confidence` `shadow1_entry_quality` |
| シャドー 2 | `shadow2_version` `shadow2_direction` `shadow2_confidence` `shadow2_entry_quality` |
| シャドー 3 | `shadow3_version` `shadow3_direction` `shadow3_confidence` `shadow3_entry_quality` |
| お試し 1 | `trial1_arm_id` `trial1_status` `trial1_direction` `trial1_confidence` |
| お試し 2 | `trial2_arm_id` `trial2_status` `trial2_direction` `trial2_confidence` |
| お試し 3 | `trial3_arm_id` `trial3_status` `trial3_direction` `trial3_confidence` |
| お試し 4 | `trial4_arm_id` `trial4_status` `trial4_direction` `trial4_confidence` |

## 7. 分割（split）

`split_at`（JST の日付、期間の内側）を渡したときだけ分ける。判断の JST 日 `day` で決める:

| 区分 | 条件 | 書き出し |
|---|---|---|
| `train` | `day < split_at`、かつ行のラベルの窓の終わりが eval の開始より前 | `dataset_train.csv` |
| `eval` | `day >= split_at + embargo_days` | `dataset_eval.csv` |
| embargo | `split_at <= day < split_at + embargo_days` | 書き出さず `rows_in_embargo` に数える |
| purged | train の候補で、ラベルの窓の終わりが eval の開始（`split_at + embargo_days` の JST 0 時）以降 | 書き出さず `rows_purged` に数える |

- **ラベルの窓**: 判断の時刻 + その行に付いているラベルのうち最長のホライズン（calibration の `h` 分、研究用の 30 分・60 分、反実仮想のホライズン。`close` は判断と同じ JST 日の 15:30）。ラベルが 1 つも無い行は窓が判断の時刻だけ。窓が eval の開始に届く行は、そのラベルが eval 期間の値動きを使うので train から外す。立会時間内の判断では、日をまたぐ窓は通常ないので purge はまれで、時間外の判断や遅い時刻の長いホライズンで効く。
- 不変条件: `rows_train + rows_eval + rows_in_embargo + rows_purged = decisions`（期間の trader 判断の数）。train と eval の日は重ならず、train の最終日は `split_at` より前、eval の最初の日は `split_at + embargo_days` 以降。分割なしは `rows = decisions`。
- 引数の制約: `split_at` は `from` より後・`to` より前、`embargo_days` は 0〜30 で、`split_at + embargo_days` も `to` より前（eval が空にならない）。`split_at` を渡すときは `from` と `to` も明示する。

## 8. 除外（出さないもの）

許可リストの列だけを `SELECT` する（`SELECT *` はない）ので、次は読みもしない。`manifest.excluded_columns` に同じ一覧を出す。

| 除外 | 理由 |
|---|---|
| `bid` `ask` `bid_depth` `ask_depth` `microprice` `raw_data_json` | 生の板・気配（再配布の制約・サイズ）。派生の `spread_bps`・`orderbook_imbalance`・`microprice_dev_bps` などだけ出す |
| `price` `vwap` `volume` `turnover` | 価格・出来高の水準そのもの（特徴量は比・bps・直近窓の量だけ） |
| `response_json` `state_json` `payload_json` | Jev の入出力・ジョブの本文（RAG 文脈・プロンプトを含みうる）。答えは固定キーだけ取り出す |
| `error_excerpt` `failure_code` `reject_reason` `last_error` | 自由文・エラーの抜粋（`reject_code` の固定コードだけ出す） |
| `runtime_settings` `secrets` | 設定の値・API キー・パスワード・トークン。`research.export.enabled` 1 件だけを読んで、値は出さない |

## 9. manifest

`manifest.json`（および MCP の応答の `manifest`）。行・設定・秘密は含まない。

| キー | 内容 |
|---|---|
| `schema_version` | §2 |
| `dataset_id` | `ds-` + `dataset_sha256` の先頭 12 桁 |
| `dataset_sha256` | 全体のハッシュ（§10） |
| `created_at` | 作成時刻（UTC RFC3339。呼び出し側の時計） |
| `from` / `to` | 実際に使った期間の JST 日（`to` は含まない）。`dataset_sha256` に入る |
| `split_at` / `embargo_days` | 実際に使った分割（`split_at` は分割なしで `null`）。`dataset_sha256` に入る |
| `label_version` | `calibration`（`label_version` 2・`horizon_minutes`・`rows_at_other_horizons`）、`research_horizon`（1・`horizons`）、`counterfactual`（2）、`note` |
| `decisions` / `rows` | 期間の trader 判断の数 / 書き出した行数 |
| `rows_train` / `rows_eval` / `rows_in_embargo` / `rows_purged` | §7。分割なしは 0 |
| `train_days` / `eval_days` | 各側の最初と最後の判断の JST 日（`first` / `last`）。分割なしは `null` |
| `columns` / `excluded_columns` | 列の順 / 除外（§8） |
| `shadow_versions` / `shadow_versions_omitted` / `trial_arms` / `trial_arms_omitted` | §6 |
| `files[]` | `name` / `format`（`csv` / `parquet`）/ `rows` / `bytes` / `sha256`（ファイルのバイト列の SHA-256）。順は `dataset_train.csv`、`dataset_train.parquet`、`dataset_eval.csv`、`dataset_eval.parquet`（分割なしは `dataset.csv`、`dataset.parquet`）。同じ側の CSV と Parquet の `rows` は一致する |
| `save_dir` | 書き出したフォルダの絶対パス（パスは応答に出してよい） |
| `notice` | ローカルのみ・外部へ送っていない・データの中身は応答に含めない |

## 10. ハッシュの確かめ方

1. 各ファイル（CSV・Parquet）について `sha256sum <file>` が `files[].sha256` と一致し、バイト数が `files[].bytes` と一致する。
2. `dataset_sha256` = SHA-256（次の行を順に連結したもの。各行は `"\n"` で終わる）:
   - `<schema_version>`
   - `from=<from>`、`to=<to>`、`split_at=<split_at。分割なしは空>`、`embargo_days=<n>`
   - `files[]` の順に `<name> <sha256（小文字 16 進）>`

   分割なしの例: `printf '2\nfrom=2026-10-05\nto=2026-10-12\nsplit_at=\nembargo_days=0\ndataset.csv %s\ndataset.parquet %s\n' <csv の sha256> <parquet の sha256> | sha256sum`。
3. `dataset_id` は `dataset_sha256` の先頭 12 桁に `ds-` を付けたもの。`manifest.json` 自身は `created_at` と `save_dir` を含むのでハッシュに入れない。

期間と分け方がハッシュに入るので、期間・`split_at`・`embargo_days` のどれかだけが違う書き出しは、ファイルのバイト列が同じでも別の `dataset_id`（別のフォルダ）になり、古い指定の manifest を「同じデータセット」として返すことはない（`TestAnotherPeriodOrSplitIsAnotherDataset`）。同じ期間・分割・DB の内容なら、同じバイト列・同じ `dataset_id` になる（再実行は何も書き直さない。既存のフォルダは、各ファイルのハッシュ・manifest の `dataset_sha256`・manifest 自身の期間・分け方・ファイルのハッシュから計算し直したハッシュがすべて合うときだけ同じデータセットとして扱い、1 つでも違えば上書きせず `dataset_dir_conflict`）。ラベルや答えが後から付くと中身が変わり、別の `dataset_id` になる。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規（スキーマ版 1） | issue #1094 |
| 2.0 | 2026-11-19 | スキーマ版 2: `dataset_sha256` に期間（`from`・`to`）と分け方（`split_at`・`embargo_days`）を含める。Parquet を CSV と並べて書く（ファイルごとのハッシュ・`format`）。ハッシュの式を `<name> <sha256>` の行形式に変更 | issue #1121・#1124 |
