# ER / データモデル: テーブル定義（見送り候補の反実仮想ラベル）

`docs/architecture/er.md` から分割した章。対象: `counterfactual_labels` と、ビュー `calibration_outcome_origins`（マイグレーション `000050`。issue #1052、親 #752）。型・規約と全体 ER 図は `docs/architecture/er.md` を参照。仕様は `docs/requirements/functional/components-pipeline.md` の FR-META-8。実装は `internal/repository/counterlabel`（表）・`internal/service/calibration/counterfactual`（記録部品）・`internal/repository/metalabel/training_skipped.go`（学習行への追加）。

## 位置づけ

ガードで見送った候補に「もし入っていたらどうなったか」の結果ラベルを残す、**取引に使わない**記録。Policy・Risk・発注・Paper ガード・Live・Jev のプロンプトのどれも読まない（`internal/repository/counterlabel/isolation_test.go` が、読めるファイルを学習・記録部品・配線に限って固定する）。`calibration_outcomes` と `jev_decisions` には列も制約も足さず、既存行の数値も書き換えない。

すでに `calibration_outcomes` にラベルが付いた見送り候補（Outcome Labeling は trader 判断なら見送りでも対象にする）は、**この表に計算し直さない**。この表に入るのは、そのラベルが付かないまま残った見送りだけである。

## counterfactual_labels

ガードで見送った LONG/SHORT の Jev 判断のうち、per-minute の Outcome Labeling が取り残したもの（再試行窓の 24 時間を過ぎて取り残された判断など）の、トリプルバリアのラベル。行の定義は `calibration_outcomes` の `label_version = 2` と同じ（同じ関数 `Labeler.Compute` が計算する: エントリー参照価格・コスト控除後 bps・ホライズン・バリアの判定ロジックは重複させていない）。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | |
| jev_decision_id | integer | NOT NULL, FK → `jev_decisions.id` | 見送った判断。`direction` は LONG か SHORT だけ（NONE は表に入れない。行を作る側が選ばず、`ListTrainingRowsFrom` 側も LONG/SHORT に絞る） |
| horizon_minutes | integer | NOT NULL, CHECK (`> 0`) | ラベルの窓（分）。Settings のトリプルバリアのタイムアウト（`calibration_outcomes.horizon_minutes` と同じ） |
| reject_reason | text | NOT NULL DEFAULT `''` | 判断の最新の `trade_signals.reject_reason`（`entry_quality_below_threshold` など。シグナルが無ければ空） |
| barrier_hit | text | NOT NULL, CHECK (`tp` / `sl` / `timeout`) | 先に触れたバリア |
| net_return_bps | numeric | NOT NULL | コスト控除後のリターン（bps）。`calibration_outcomes.net_return_bps` と同じ定義 |
| entry_ref_price | numeric | NOT NULL, CHECK (`> 0`) | 執行できる基準価格（LONG は ask、SHORT は bid） |
| label_version | integer | NOT NULL, CHECK (`= 2`) | トリプルバリア版だけ。終値だけの旧ラベル（1）は入れない |
| future_return / max_adverse_excursion / max_favorable_excursion | numeric | NOT NULL | `calibration_outcomes` と同じ意味（%） |
| llm_cutoff | text | NOT NULL | 記録したときの確認済み `metalabel.llm_cutoff`（`YYYY-MM-DD`）。仮置きのあいだは記録しないので、仮置きの日付は入らない |
| created_at | text | NOT NULL | 記録した時刻（UTC） |

制約・索引: `UNIQUE (jev_decision_id, horizon_minutes)`、`(jev_decision_id, id)`。あわせて `trade_signals (jev_decision_id)` の索引を足す（列・制約は変えない。見送りかどうかの判定が判断ごとにシグナルを引くため）。

書き手: 夜間再学習ジョブ（`internal/bootstrap/metalabeljob`）が、再学習の前に `counterfactual.Recorder` を 1 回動かす。対象は次をすべて満たす判断:

- `decision_type = 'trader'` で方向が LONG か SHORT（`jev_none` の NONE は方向を補完せず、対象にしない）
- 判断時刻が、確認済みの `metalabel.llm_cutoff` の日以降（`placeholder` のあいだは何も記録しない。後追い投入で LLM の学習期間に重なるラベルを作らない）で、25 時間より古い（Outcome Labeling の 24 時間の再試行窓の外。窓の中は Outcome Labeling が自分でラベルを付けるので競合しない）
- Policy・Risk を通ったシグナルが無い（`risk_passed = 1` の行が無い）
- `calibration_outcomes` に `label_version = 2` で `net_return_bps` のある行が無い（二重に計算しない）、`calibration_label_skips` にそのホライズンの印が無い、この表に同じホライズンの行が無い

ホライズン分の足が揃わない判断は、Outcome Labeling と同じく `calibration_label_skips` に印を付け、ラベルは書かない（短い窓の結果は記録しない）。1 回の実行は古い順に最大 2000 件で、残りは翌晩に回す。バックフィルのための別経路は無い。

読み手: 夜間再学習の行取り出し（`metalabel.include_skipped_labels` がオンのときだけ。FR-META-8）。

## calibration_outcome_origins（ビュー）

`calibration_outcomes` の各行が、エントリーした判断のものか、見送った判断のものかを見分ける印。行は増やさず、`calibration_outcomes` の列も足さないので、既存行の数値は変わらない。読むたびに `trade_signals` から導く（トリガ・複製は持たない）。1 行は 1 つの `calibration_outcomes` 行に対応する。

| カラム | 説明 |
|-------|------|
| calibration_outcome_id / jev_decision_id / horizon_minutes | `calibration_outcomes` の行の識別 |
| origin | `entered`: 判断のシグナルが Policy・Risk を通った（`risk_passed = 1`）。`skipped`: LONG/SHORT と答えたが通らなかった（シグナルが無い判断を含む）。`no_direction`: Jev の方向が NONE（`jev_none`）。学習行には入らない |
| reject_reason | 判断の最新のシグナルの `reject_reason`（無ければ NULL） |

分析用の読み取り専用で、学習に使う行は `origin` によらず従来どおり（`ListTrainingRowsFrom` の条件は変わらない）。

## 選択バイアス

ガードで見送られた候補は、ガードを通った（エントリーした）候補と同じ分布ではない（ガードがすでに質の低い候補・流動性の低い銘柄などを落としている）。この表の行を学習に混ぜると、メタモデルの採用・AUC・EV が「ガードを通った候補」ではなく「通る前の候補を含む母集団」の意味に変わる。そのため学習へ混ぜる設定 `metalabel.include_skipped_labels` は既定オフで、オンのあいだは夜間再学習が警告を残す。
