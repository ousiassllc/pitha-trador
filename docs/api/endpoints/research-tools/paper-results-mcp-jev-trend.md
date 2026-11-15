# API 仕様: Paper 実績 MCP の Jev 評価の推移ツール

`docs/api/endpoints/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1089（その 2）。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。**読み取り専用**（`readOnlyHint: true`・`destructiveHint: false`。何も書かず、設定も変えない）。設計は `docs/architecture/research-tools/paper-results-mcp-jev-trend.md`、表は `docs/architecture/er/tables-research-eval.md`。

## 3. ツール

### 3.36 `jev_evaluation_trend`

`jev_evaluation`（§3.33）の主な数字を**日ごと**に並べた推移。夜間の研究ジョブ（`research.eval.enabled=1` のときだけ動く）が、JST の日が終わった後にその日の評価を `(model_id, question_version, horizon_minutes)` ごとに `jev_evaluation_daily` へ 1 行ずつ記録したものだけを読む。ラベルや足は読み直さない。記録は**書いた日の設定・データのまま**で、あとから設定を変えても書き換わらない。

**位置づけ**: 研究用の読み取り。採用・昇格の基準ではなく、しきい値・合否・推奨も当てない（出力の `notice` に固定文で出る）。Jev・Risk・Policy・注文・Paper ガード・メタモデルの学習には使わない。読むのは `jev_evaluation_daily` と、`runtime_settings` の `research.eval.enabled` / `research.eval.retention_days` の 2 キーだけ。

引数:

| 引数 | 内容 |
|---|---|
| `from` / `to` | 他のツールと同じ期間（JST の暦日、`[from, to)`、最大 366 日、既定は直近 30 日）。記録の `day`（判断の JST 日）で切る |
| `horizon_minutes` | 任意（1〜60）。記録時のホライズンがこの値の行だけ。省略は全ホライズン（ホライズンごとに別の系列） |
| `prompt_version` | 任意。この版（`question_version`。例 `trader-v4`）の系列だけ。省略は全版 |
| `model_id` | 任意（100 文字まで）。この `model_id` の系列だけ。省略は全モデル |

不明な引数・型の違い・範囲外は `invalid_argument`（422）、期間の誤りは `invalid_period`（422）。期間内の記録行が 366 日 × 50 コホート = 18,300 行を超えると、部分結果を返さず `limit_exceeded`（422。期間か `model_id` / `prompt_version` / `horizon_minutes` で狭める）。

#### 応答の `data`

| キー | 内容 |
|---|---|
| `from` / `to` | 解決した期間（JST の日付） |
| `horizon_minutes` / `prompt_version` / `model_id` | 渡した絞り込み（渡さなければ項目なし） |
| `notice` | 固定の注意書き（研究用・採用基準ではない・しきい値を当てない・何も書かない） |
| `recording` | 記録の状態（下記） |
| `series[]` | `(model_id, question_version, horizon_minutes)` ごとの系列（下記）。モデル・版・ホライズンの順。該当なしは空配列 |
| `definitions` | 記録・系列・各数字・累積の定義文 |
| `caveats[]` | 数字の限界（日が飛びうる・書いた日の設定のまま・検出できる最小の差は記述的・日クラスタの区間は日数が少ないと出ない・random は期待値。スイッチがオフのときはその旨） |

**`recording`**（期間ではなく表全体と、いまの設定）: `enabled`（`research.eval.enabled`。既定 false）・`retention_days`（`research.eval.retention_days`。既定 365）・`first_recorded_day` / `last_recorded_day`（記録がなければ `null`）・`days_recorded`（記録のある JST 日の数）。

**`series[]` の要素**:

| キー | 内容 |
|---|---|
| `model_id` / `question_version` / `horizon_minutes` | 系列のキー。版・モデル・ホライズンが変わると別の系列（数え直し） |
| `first_day` / `last_day` / `days_recorded` | 期間内の記録の最初・最後の日と日数（記録のない日は飛ぶ） |
| `decisions` / `evaluated` / `excluded` | 期間内の合計（`excluded = decisions − evaluated`） |
| `label_settings_changed` | 隣り合う記録日で `label_settings`（take_profit・stop_loss・コスト・ホライズン）のどれかが違えば true。累積は異なるラベルの日を混ぜる |
| `label_settings_changed_on[]` | 前の記録日と違った日（なければ空配列） |
| `cumulative` | 期間の累積（下記） |
| `daily[]` | 記録日ごとの行（古い日から。下記） |

**`daily[]` の要素**: `day`・`decisions`（その日の trader LONG/SHORT 判断。除外前）・`evaluated`（全アーム共通の比較の行）・`excluded`（`decisions − evaluated`。約定できる気配なし・窓がホライズンに届かない・足なし・学習 cutoff より前を含む）・`entered`（`trade_signals` に `risk_passed=1` の行がある判断。約定ではない）・`win_rate`（llm の net > 0 の割合）・`llm_mean_net_bps` / `random_mean_net_bps` / `momentum_mean_net_bps` / `contrarian_mean_net_bps`（日の合計 net bps / `evaluated`。コスト控除後）・`llm_minus_random_mean_bps`・`label_settings`（`horizon_minutes` / `take_profit_bps` / `stop_loss_bps` / `barrier_cost_bps`）。`evaluated=0` の日は割合と平均が `null`。

**`cumulative`**: 日の行 `(evaluated, 合計, 勝ち数)` を日クラスタにした、`jev_evaluation` と同じ日クラスタのブートストラップ（alpha=0.05 両側・検出力 0.8・2000 回・固定シード）。`evaluated=0` の日は含めない。
- `llm`: `n`・`days`・`mean_net_bps`・`ci`（95%。2 日未満は `null`）・`win_rate`・`detectable_minimum_edge_bps`・`days_to_detect[]`（§3.33 の `Metrics` と同じ。`daily` はなし）
- `llm_minus_random`: llm − random の `n`・`days`・`mean_diff_bps`・`ci`（日ごとの差の合計を日クラスタ）・`detectable_minimum_difference_bps`・`days_to_detect[]`（§3.33 の `Diff` と同じ）

これらは、同じ判断の行から `jev_evaluation` と同じ式で出した値と一致する（記録時と設定が同じなら）。

#### 例（抜粋）

```json
{"from":"2026-10-05","to":"2026-10-10","recording":{"enabled":true,"retention_days":365,"first_recorded_day":"2026-10-05","last_recorded_day":"2026-10-09","days_recorded":5},
 "series":[{"model_id":"model-1","question_version":"trader-v3","horizon_minutes":10,"days_recorded":5,"decisions":10,"evaluated":9,"excluded":1,
   "label_settings_changed":false,"label_settings_changed_on":[],
   "cumulative":{"llm":{"n":9,"days":5,"mean_net_bps":-1.11,"ci":{"lower":-20,"upper":15,"level":0.95}},"llm_minus_random":{"n":9,"days":5,"mean_diff_bps":3.33}},
   "daily":[{"day":"2026-10-08","decisions":2,"evaluated":1,"excluded":1,"entered":0,"llm_mean_net_bps":-20,"random_mean_net_bps":-10,"llm_minus_random_mean_bps":-10}]}]}
```

（数値は説明用。実際の区間は `ci` の値）

#### 使うときの注意

- 記録がないとき（スイッチがオフ・記録前・保持期間を過ぎた）は `series` が空で、`recording` に状態が出る。スイッチは Settings 画面の `research.eval.enabled`（既定オフ）。
- 日が飛ぶ・設定が途中で変わる・版が変わる、のどれでも系列の読み方が変わるので、`label_settings_changed` と `days_recorded` を先に見る。
