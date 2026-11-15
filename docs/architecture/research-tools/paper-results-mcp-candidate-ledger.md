# 設計: Paper 実績 MCP の候補の台帳（candidate_ledger）

`docs/architecture/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1090。ツールの引数と出力は `docs/api/endpoints/research-tools/paper-results-mcp-candidate-ledger.md`。昇格チェックリスト（`promotion_checklist`）は同じ計算を使う（`docs/architecture/research-tools/paper-results-mcp-promotion.md`）。

## 1. 位置づけ

ライブの Jev と、別モデル・別プロンプト版・お試しのアーム・研究用モデル・単純なベースラインを、**同じ期間・同じ判断の行・同じコスト控除後ラベル**で並べる読み取り専用のツール。日クラスタの区間、ライブとの差、多重比較の補正後の p を出す。

- **何も決めない**: 勝者・順位・多数決・推奨・しきい値は無い。候補は種類 → id の固定の順で、値で並べ替えない。
- 読む表: `jev_decisions`（基準の行・`request_cost`・モデル名）、`market_snapshots`（判断の足の `return_5m`）、`jev_shadow_decisions`・`jev_trial_decisions`（方向・状態・モデル名・費用だけ。本文の列は読まない）、`research_horizon_labels`（30m・60m・close のラベル）、`research_model_predictions` / `research_model_runs`（予測方向）、`runtime_settings`（`metalabel.*` と、`evaldiag` が読むバリア・コストの設定、ライブの版）。
- 書くもの: 無い。MCP の読み取り接続（`mode=ro` + `query_only`）の読み取りトランザクションの中だけで動く。
- 取引への影響: 無い。Jev・Risk・Policy・注文・Paper ガード・既存のシャドー（`trader-v5`）・メタモデルの学習・採用は読まない・変えない。
- 実装: `internal/service/paperresultsmcp/ledgerdiag`（`Compute` が型付きの結果を返し、`Run` がツール）、登録は `research_tools.go`（`queryTool`）。テストは `internal/service/paperresultsmcp/ledgertests`。
- 隔離: `ledgertests/isolation_test.go` が、2 つのパッケージ（`ledgerdiag`・`promotiondiag`）を参照できるのが `research_tools.go` だけであること、import が読み取り専用の部品に限られること、SQL が SELECT だけで上の表だけを読み、本文の列を名指さないことを固定する。

## 2. 基準と行

**基準（`reference`）** = ライブの版（`jev.ResolveLiveVersion`。`jev_calibration` と同じ解決）の、期間内の `decision_type=trader` かつ LONG/SHORT の判断の llm アーム（その判断自身の方向のラベル）。`model_id` を渡すとそのモデルの判断だけ。ライブの版に複数の `model_id` が混ざれば `multiple_model_ids=true`。

**行**: 期間の全 trader LONG/SHORT 判断のうち、(1) `metalabel.llm_cutoff` が仮置きでなければ cutoff 前を除き、(2) ホライズンのラベルが両側（買い・売り）に付くものだけを全員共通の行にする。除外は `excluded_decisions` に件数で出す（黙って落とさない）。

| `horizon` | ラベル |
|---|---|
| `10m` | `evaldiag.Rows`（`jev_evaluation` と同じ `calibration.LabelWindow` の付け直し。`HorizonMinutes=10`） |
| `30m` / `60m` / `close` | `research_horizon_labels` の `long_net_bps` / `short_net_bps` を `(instrument_id, snapshot_at = 判断の timestamp)` で結ぶ。どちらかが NULL・行なしなら除外。判断の足が無ければ momentum を作れないので除外（`no_bar_for_momentum`） |

**候補の net**: 候補の方向のラベル（LONG = 買いの net、SHORT = 売りの net、NONE・取引なし = 0）。ベースラインは `jev_evaluation` の定義（`random` = 買いと売りの平均、`momentum` = 判断の足の `return_5m` の符号、`contrarian` = その逆、`none` = 0、`long_leg` / `short_leg`）で、`10m` 以外も同じ式。

## 3. 並べ方（alignment）

- `common_all`: 基準と、答えが 1 件でもある全候補が答えた行の積集合。全候補の `n_common` が同じ。答えが 1 件も無い候補は `no_answers=true` で積集合から外す（空の 1 候補で他の表が空にならない）。`live_cohort` は別の判断を見ているので積集合に入れない。
- `pairwise`: 各候補を、基準と共通の行だけで比べる。
- 欠測率 = `1 - n_common / 基準の行数`。trial の failed / refused / partial と行なし、shadow・研究用モデルの予測なしは欠測。`missing` に `absent` / `failed_or_refused` / `outside_common` の内訳。

## 4. 統計

- 平均・区間: `evaldiag.Summarize`（日クラスタのブートストラップ、`alpha=0.05`・`resamples=2000`・`seed=1073`。同じ入力なら同じ結果）。
- 差: 同じ行ごとの（候補 − ライブ）の系列を同じ関数で要約。p 値 = `stats.PValue(平均, 標準誤差)`（正規近似の両側）。
- 多重比較: p を持つ全候補を 1 つの族として `stats.Holm`。`family_size_extra`（昇格チェックリストの引数）は、他で試した候補を **p=1 の比較として族に足す**（族を広げて補正を強める方向にだけ動き、弱める方向には動かない）。
- 2 日未満の系列は区間・標準誤差・p が null（検出できる最小の差も null）。

## 5. 費用

AI の費用（USD）は net bps と**別**に報告する。基準の費用は `jev_decisions.request_cost`。shadow は `request_cost`（`cost_is_estimate=1` は `usage_estimate`）、trial は `counted_cost_usd` を `cost_source` 別に、研究用モデル・ベースラインは有料 API なし（`none`、0）。

- **不明を 0 にしない**: `request_cost` が NULL、または trial の `cost_source=unknown` で金額が 0 の行は金額なし。合計に入れず `complete=false` にして、1 行あたりの費用・bps 換算を出さない。
- `usd_jpy` と `notional_jpy` が**両方**あるときだけ `cost_bps_per_row = usd_per_row × usd_jpy / notional_jpy × 10000` と、`mean_net_after_ai_cost_bps`・`diff_after_ai_cost_bps`（差の平均 −（候補の費用 − ライブの費用））を出す。既定値は置かない。

## 6. 上限

期間の判断は `evaldiag.MaxDecisions`（10,000）、候補は 60、shadow・trial・研究用モデルの答えは各 20 万行まで。超えたら `limit_exceeded`。読みは索引のある範囲（判断の時刻・`(instrument_id, snapshot_at)`・予測の `day`）で、3 秒の予算内に収まる。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1090 |
