# ER / データモデル: テーブル定義（研究用の特徴量モデル）

`docs/architecture/er.md` から分割した章。対象: `research_model_runs` / `research_model_predictions`（マイグレーション `000057`。issue #1092）。型・規約と全体 ER 図は `docs/architecture/er.md` を参照。設計は `docs/architecture/research-tools/research-model.md`。実装は `internal/repository/featuremodel`（表）・`internal/service/featuremodel`（学習・予測）・`internal/bootstrap/nightlyjobs/model.go`（夜間タスク）。

## 位置づけ

特徴量だけ（AI の出力を使わない）で学習する**研究専用**のモデルの、学習 1 回ごとの記録（`research_model_runs`）と、その学習が予測した Jev の各判断への予測値（`research_model_predictions`）。候補台帳（`candidate_ledger`）が、同じ判断の行で Jev とモデルを並べて比べるために読む。**取引には使わない**: Policy・Risk・発注・実行・Paper ガード・Live・Jev のプロンプト・RAG・メタモデルの学習と採用・自己改善は読まない（`internal/repository/featuremodel/isolation_test.go` が、参照できるファイルをこのモデルの 2 パッケージ・夜間タスク・候補台帳の読み取りに限って固定する）。

既存の表には列も制約もトリガも足さない。この 2 表だけを書き、読むのは `research_horizon_labels`・`research_label_days`・`market_snapshots`・`jev_decisions`（読み取り）だけ。`calibration_outcomes`・メタモデルの表・`runtime_settings` は読まない。

## research_model_runs

1 行は 1 回の学習（予測する JST の 1 日につき 1 行）。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | `research_model_predictions.run_id` が指す（外部キーにはしない: 保持期間の削除を止めない） |
| version | varchar(80) | NOT NULL, UNIQUE, CHECK (`<> ''`) | `rm-ridge1-30m-<予測する日>`（例 `rm-ridge1-30m-2026-10-12`）。同じ日の再実行は UNIQUE で何も書かない（冪等） |
| algorithm | varchar(20) | NOT NULL | `ridge`（リッジ回帰の閉形式） |
| label_horizon | text | NOT NULL, CHECK (`30m` / `60m` / `close`) | 学習するラベルのホライズン。現在は `30m` のみ |
| features_json | text | NOT NULL | 使った特徴量の名前の配列（`market_snapshots` の列名。ベクトルの並びの定義） |
| hyperparams_json | text | NOT NULL | ハイパーパラメータ（定数）: `ridge_lambda` / `sample_buckets` / `min_train_rows` / `min_train_days` / `max_train_days` / `label_horizon` / 標準化・欠測の補完・解法・パージの説明 |
| coefficients_json | text | NOT NULL | `mean` / `std`（特徴量ごとの学習時の平均・標準偏差）、`long` / `short`（各側の `n`・`intercept`・`coef`（標準化した特徴量あたりの係数））。これだけで予測を再現できる |
| train_from / train_to | text | NOT NULL, CHECK (長さ 10) | 学習に行が入った最初・最後の JST の日 |
| predict_day | text | NOT NULL, CHECK (長さ 10 かつ `train_to < predict_day`) | 予測する JST の日。学習は必ずこの日より前だけ（表の制約でも固定） |
| n_train | integer | NOT NULL, CHECK (`> 0`) | 学習に使った行数（片側でも結果のある行） |
| n_predicted | integer | NOT NULL, CHECK (`>= 0`) | この学習が書いた予測の行数 |
| train_digest | varchar(64) | NOT NULL | 学習行（スナップショット ID・目的変数・特徴量の値。固定の順）の SHA-256（16 進）。同じ行なら同じ値 |
| created_at | text | NOT NULL | 記録した時刻（UTC） |

## research_model_predictions

1 行は (学習, Jev の判断) の組: 判断の足の特徴量から出した、LONG・SHORT それぞれの予測の純 bps（コスト控除後・30 分保有）。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | |
| run_id | integer | NOT NULL（**外部キーなし**） | `research_model_runs.id` |
| jev_decision_id | integer | NOT NULL（外部キーなし） | `jev_decisions.id`。対象は `decision_type='trader'` かつ `direction` が LONG / SHORT の判断で、判断の足（`market_snapshots` の同じ銘柄・同じ時刻）がある行だけ。足の無い判断には行を作らない |
| instrument_id | integer | NOT NULL | 判断の銘柄 |
| snapshot_at | text | NOT NULL | 判断の `timestamp`（UTC・固定 9 桁）。足の時刻と同じ値 |
| day | text | NOT NULL, CHECK (長さ 10) | `snapshot_at` の JST の日付。日ごとの読み・保持期間の削除のキー |
| pred_long_net_bps / pred_short_net_bps | numeric | NOT NULL | LONG・SHORT を 30 分保有したときの純 bps の予測 |
| direction | varchar(5) | NOT NULL, CHECK (`LONG` / `SHORT` / `NONE`) | モデルの選択: `pred_long > pred_short` かつ `pred_long > 0` なら LONG、`pred_short > pred_long` かつ `pred_short > 0` なら SHORT、それ以外は NONE |
| created_at | text | NOT NULL | 記録した時刻（UTC） |

制約・索引: `UNIQUE (run_id, jev_decision_id)`、`(jev_decision_id)`（台帳が判断から引く）、`(day)`（保持期間の削除）。

## 読み方（候補台帳）

`research_model_predictions` を `jev_decision_id` で `jev_decisions` に、`(instrument_id, snapshot_at)` で `research_horizon_labels`（`horizon='30m'`）に結べば、Jev の方向とモデルの方向を、同じ判断・同じ足の正解（`long_net_bps` / `short_net_bps`）に当てて比べられる。モデルの学習は予測する日より前の日だけを使うので、その日の予測は将来の情報を含まない。
