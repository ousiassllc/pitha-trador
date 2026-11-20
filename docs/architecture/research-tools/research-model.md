# 設計: 研究用の特徴量モデル（ウォークフォワード学習・予測の記録）

issue #1092・#1123。表は `docs/architecture/er/tables-research-model.md`（マイグレーション `000064`。#1123 でテーブルの変更はない）。実装は `internal/repository/featuremodel`（表の読み書き）・`internal/service/featuremodel`（学習・予測・実行部品。リッジ回帰）・`internal/service/featuremodel/gbt`（木のモデルの純 Go 実装）・`internal/bootstrap/nightlyjobs/model.go`（夜間タスク）。モデルは 2 種類（リッジ回帰 `ridge`、勾配ブースティングの木 `gbt`）×3 つのラベルのホライズン（`30m`・`60m`・`close`）の 6 つ（`featuremodel.Specs()`）。

## 1. 目的と位置づけ

Jev（AI）が出した方向に優位性があるかを測るには、「AI を使わず、特徴量だけで学習したモデル」が同じ判断の足でどれだけ取れたかと並べる必要がある。このモデルは**研究専用**で、予測は Jev の各判断（`jev_decisions`）の行と並べて記録し、候補台帳（`candidate_ledger`、別ツール）が同じ行で比べる。MCP ツールはこの設計には無い。

**取引に影響しない**ことが条件で、次で担保する:

- **既定はオフ**: Settings の `research.model.enabled`（`runtime_settings` の JSON 整数 1 / 0、既定 0。`researchjobsettings.Model`）がオンのときだけ動く。オフ（未設定・0・壊れた値）の間は、夜間タスクが `maintenance.ErrSkipped` を返し、学習も読み取りもせず、表にも `runtime_settings`（ランナーの最終成功日 `system.maintenance.research_model.last_success_date`）にも何も書かない。16:00 JST より前も同じ（今日を半端に学習しない）。
- **状態の表示（issue #1126）**: Settings のカード `research-jobs` の「夜間タスクの状態」が、このタスクと `jev_evaluation_daily` の最終成功日・失敗・オフを表示する（`internal/service/researchjobstatus`、読み取りだけ）。失敗は、`Task.TrackFailure` を付けたこの 2 タスクが失敗したときだけランナーが `system.maintenance.research_model.last_failure`（日付と 120 文字までの 1 行の理由）に書き、成功・`ErrMore` で消す。オフ・16:00 JST 前の `ErrSkipped` は失敗に数えず、何も書かない。表示は取引・昇格に使わない。
- 書くのは新しい 2 表（`research_model_runs` / `research_model_predictions`）だけ。`calibration_outcomes`・メタモデルの表・`jev_decisions`・取引の表には書かない。既存の表に列も制約もトリガも足さない。Live の Jev（trader-v3）・Risk・発注・Paper ガード・既存のシャドー（trader-v5）・メタモデルの学習と採用は変えない。
- 参照を固定する: `internal/repository/featuremodel/isolation_test.go` が、表名・パッケージ名のシンボルを、このモデルの 2 パッケージ・夜間タスク（`nightlyjobs/model.go`）・候補台帳の読み取り・`researchjobsettings`（スイッチの説明）・`db/migrations` のファイルだけに限り、Policy・Risk・発注・実行・Paper ガード・Live・Jev プロンプト・RAG・メタモデル・自己改善が参照すると失敗する。このモデルのパッケージが書く表が 2 表だけで、読む表が `research_horizon_labels`・`research_label_days`・`market_snapshots`・`jev_decisions` だけ（`runtime_settings` を含まない）ことも同じテストが固定する。`services_build.go` の該当は夜間タスクの登録 1 行だけ。
- 有料 API・ネットワーク・キーは使わない（SQLite の読み取りと純 Go の数値計算だけ）。

## 2. 特徴量

`market_snapshots` の数値列の固定リスト（`featuremodel.Features`）。足そのものの派生特徴量だけで、板の生の値（bid・ask・depth・`raw_data_json`）と Jev の出力は使わない。

`return_1m` / `return_5m` / `return_15m` / `return_30m` / `price_vs_vwap_bps` / `volume_ratio_5m` / `orderbook_imbalance` / `realized_vol_5m` / `spread_bps` / `market_return_5m` / `sector_return_5m` / `vwap_slope`

- 欠測（NULL）は**学習時の平均で補完**する（標準化すると 0 = 予測への寄与が 0）。
- 標準化は学習行の観測値の平均・母標準偏差（Welford の逐次計算）。分散が 0 の特徴量は std = 1（標準化後は常に 0、係数は 0）。
- 特徴量の変更は新しいモデル版（`version` の接頭辞）で行い、各学習の `features_json` に並びを記録する。

## 3. モデルと学習

- **データ**: `research_horizon_labels` の、そのモデルのホライズン（`horizon='30m'` / `'60m'` / `'close'`）・`sample_bucket < 10`（`snapshot_id` の混ぜ関数による決定的な約 10% の標本）の行を `market_snapshots`（`snapshot_id`）の特徴量に結ぶ。目的変数は `long_net_bps` と `short_net_bps`（コスト控除後）で、その側に値のある行だけをその側の学習に使う。`market_snapshots` が保持期間で消えた行は特徴量が無いので入らない。各ホライズンは自分のラベルだけで学習し、別のホライズンのラベルは混ぜない。
- **リッジ回帰の閉形式（`ridge`）**: 標準化した特徴量にたいして、LONG 用・SHORT 用に 1 つずつ。切片は罰則なし、`lambda = 1000`。正規方程式 `X'X + λD` と `X'y` を**行を保持せず逐次に積算**して、Cholesky 分解で解く（純 Go）。標準化の平均・標準偏差を出す 1 巡目と、正規方程式を積算する 2 巡目の 2 回、同じ行を読む。
- **木のモデル（`gbt`、`internal/service/featuremodel/gbt`）**: 二乗誤差の勾配ブースティング。LONG 用・SHORT 用に 1 組ずつ、深さ 3 の回帰木を 60 本、学習率 0.1。分割は特徴量ごとの学習行の分位点で切った最大 32 ビンのヒストグラムで探し、分散の減りが最大の分割を取る（引き分けは番号の小さい特徴量、次に小さいビン）。葉は 100 行以上。欠測は学習時の平均で補完する。**乱数を使わない**（行・特徴量の抽出なし）。行を保持するので、窓の行数が `TreeMaxRows`（200000）を超えるときは固定の順で決まった幅（k 行に 1 行）に間引く（`stride_rows` を記録）。1 巡目で窓・平均・行数を数え、2 巡目で行を集める。
- **定数**（`hyperparams_json` に記録）: 共通 `sample_buckets=10`、`min_train_rows=2000`（各側）、`min_train_days=5`、`max_train_days=60`、`label_horizon`（`30m` / `60m` / `close`）。リッジは `ridge_lambda=1000`。木は `trees=60`・`depth=3`・`learning_rate=0.1`・`min_leaf=100`・`bins=32`・`max_rows=200000`・`stride_rows`・損失・分割・乱数なし・補完・パージの説明。
- **記録**: 学習ごとに 1 行の `research_model_runs`: `algorithm`（`ridge` / `gbt`）・`label_horizon`・`features_json` / `hyperparams_json` / `coefficients_json`（リッジは `mean`・`std`・各側の `n`・`intercept`・`coef`。木は `mean`・各側の木の配列・`stride_rows`・`n_long`・`n_short`。どちらも、これだけで予測を再現できる）/ `train_from`・`train_to` / `n_train` / `n_predicted` / `train_digest`（モデルの版の接頭辞を含むので、同じ行でもモデルごとに違う）/ `version`（`rm-<algorithm>1-<horizon>-<予測する日>`。例 `rm-ridge1-30m-2026-10-12`（これまでと同じ）、`rm-gbt1-60m-2026-10-12`、`rm-ridge1-close-2026-10-12`）。モデルの方式・定数の変更は版の数字（`1`）を上げる。

## 4. ウォークフォワードと漏れの防止

予測する JST の日を D とする。

1. 学習に使う日は **`research_label_days` に完了の印があり**、そのホライズンの件数が 0 より大きく（`labeled_30m` / `labeled_60m` / `labeled_close` > 0）、**D より前**の日のうち新しい方から最大 60 日（拡張窓を上限で切る）。印の無い日（記録が途中の日）・D と D より後の日は入らない。
2. その日の行のうち、**`exit_at` が D の開始（JST 0:00）より前**のものだけを学習に使う（パージ）。`60m`・`close` のラベルも出口は同じ日の内なので、実際にはほぼ働かない守りだが、ラベルが D に食い込む行があっても学習に入らない（`Trained.Purged` に件数が出る）。学習のラベル期間と予測する日は重ならない。
3. 窓が 5 日未満、または各側の行数が 2000 未満なら、モデルは作らない（`ErrInsufficient`。ログに理由を出し、run も予測も書かない）。木は、間引いたあとの各側の行が葉の最小行数の 2 倍に満たない場合も作らない。
4. 表の制約 `train_to < predict_day` でも、学習が予測する日より前だけであることを固定する。

テスト（`internal/service/featuremodel`・`featuremodel/treetests`・`featuremodel/gbt`）: D・D+1 に極端な値を入れた日、印の無い日、D に食い込む出口の行をそれぞれ加えても、**6 つのモデルすべて**で D の学習のモデル・ダイジェスト・窓が変わらないこと、`exit_at` が D の開始ちょうどの行はパージされ 1 秒前の行は残ること（ホライズンごと）、ホライズンごとの完了の件数が 0 の日は学習日に数えないこと、学習が必要な日数・行数に満たないとモデルを作らないこと。

## 5. 予測と記録

- 対象: D の JST の日の `jev_decisions` のうち `decision_type='trader'` かつ `direction` が LONG / SHORT で、判断の足（`market_snapshots` の同じ銘柄・同じ時刻。`evaldiag` と同じ結合）がある判断。足の無い判断・NONE・scout・他の日の判断には予測を作らない。
- 各判断について、その足の特徴量から `pred_long_net_bps` / `pred_short_net_bps` を計算する。`direction` は `pred_long > pred_short` かつ `pred_long > 0` なら LONG、`pred_short > pred_long` かつ `pred_short > 0` なら SHORT、それ以外は NONE。
- run と全予測を **1 トランザクション**で書く。`INSERT OR IGNORE`（`version` が UNIQUE）なので、同じ日の再実行は何も書かない（冪等）。`jev_decision_id`・`instrument_id`・`snapshot_at`（= 判断の時刻）・`day`（JST）を埋める。

## 6. 決定性

行は `ORDER BY day, snapshot_id` の固定の順で読み、積算の順が決まるので、**同じデータ・同じ定数なら係数・木・ダイジェスト・予測がビット単位で同じ**になる（積は `float64(...)` で包み、融合積和による環境差を避ける。木は乱数・マップの反復順に依存せず、引き分けの順も固定）。許容は**なし（完全一致）**で、テストは `==` で比べる。再現手順: 同じ DB（学習に使う日の `research_horizon_labels`・`market_snapshots`）と同じ定数（`hyperparams_json`）で `featuremodel.TrainRidge` / `featuremodel.TrainTree` を呼ぶ。同じ CPU アーキテクチャ内で固定する（別アーキテクチャ間は `float64(...)` で融合を避けているが、テストでは確認していない）。`train_digest` は学習行（スナップショット ID・目的変数・特徴量の値。欠測は別の印）の SHA-256。テストは同じ DB の 2 回の学習と、同じ内容の別の DB の学習が（6 つのモデルすべてで）一致すること、データが変わるとダイジェストが変わることを固定する。

## 7. 夜間タスク（nightlyjobs.ModelTask）

メンテナンスタスク `research_model`（`internal/bootstrap/services_build.go` の `scheduler.WithMaintenanceTask` に 1 行で登録済み）。

- 16:00 JST より前、およびスイッチがオフの間は `maintenance.ErrSkipped`（何も読まず・書かない）。設定は実行ごとに読むので、保存すると再起動なしで次のチェックから効く。
- 1 回の実行 = 1 チャンク: **直近 7 日（今日を含む。今日は 16:00 から）**のうち、trader の LONG / SHORT の判断があり、まだ run が無い（日, モデル）の組を、古い日から、同じ日の中は `Specs()` の順（`30m` のリッジ・木、`60m` のリッジ・木、`close` のリッジ・木）で **1 つ**学習・予測する。履歴が足りない組はログに理由を出して飛ばす（次の実行でも再判定する）。残りの組があれば `maintenance.ErrMore` を返し、ランナーが保守のロックを放して呼び直す。`featuremodel.NewRunner` の既定は従来どおりリッジの `30m` だけで、夜間タスクだけが全 6 つを指定する（オフの間は 6 つとも学習も書き込みも無い）。
- 日の処理が済んだあと（残りの日が無いとき）に、保持期間（`research.model.retention_days`、既定 90 日、1〜3650）より古い行を、**この 2 表だけ**から、予測 → run の順に、1000 行ずつ最大 10 バッチの 1 チャンクで削除する。残りがあれば `ErrMore`。run は予測がすべて消えてから消す。
- パニックはエラーとして返す（取引と同じプロセスを落とさない）。失敗はランナーが他のタスクから隔離して再試行する。
- 夜に前日のラベル（`research_labels`）がまだ印付けされていなければ、その日は学習窓に入らない。run は日ごとに 1 回だけ（`version` が UNIQUE）なので、後からラベルが増えても同じ日は学習し直さない。

## 8. 候補台帳からの読み方

台帳（別担当のツール）は `research_model_predictions` を `research_model_runs`（`run_id`）に結び、`jev_decision_id` で `jev_decisions` に、`(instrument_id, snapshot_at)` で `research_horizon_labels`（台帳のリクエストの `horizon`。`30m`・`60m`・`close`）に結んで、Jev の方向・モデルの方向・同じ足の正解（`long_net_bps` / `short_net_bps`）を同じ行で比べる。候補は run の `algorithm` と `label_horizon` ごとに 1 つ（`research_model:ridge:30m`・`research_model:gbt:60m`・`research_model:gbt:close` など。台帳は同じ判断に複数の run があれば最新の run を取る）で、木のモデルも 60 分・引けも追加の読み取りなしでそのまま選べる（`ledgertests/research_model_test.go` が `horizon=60m` / `close` の行・平均をテストで固定する）。台帳の `horizon` は正解のラベルを、候補の ID の時間軸はモデルが学習したラベルを決めるので、同じ時間軸で揃えて比べる。読み取り専用で、この 2 表に書かない。モデルの予測は取引に戻さない。
