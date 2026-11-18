# API 仕様: Paper 実績 MCP のリプレイ評価ツール

`docs/api/endpoints/paper-results-mcp.md` §3.40 の分割章（行数上限のため別ファイル）。issue #1085。保存済みのライブの trader 判断の入力を、お試しのアーム（`jev.trial.arms`）に後から聞き直して比べる**リプレイ評価**を、MCP から見積もり・開始・停止・進み具合の確認・結果の読み取りができる。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。設計は `docs/architecture/overview/integrations-jev-replay.md`、表は `docs/architecture/er/tables-jev-replay.md`。

## 3. ツール

### 3.40 `replay_estimate` / `replay_start` / `replay_stop` / `replay_progress` / `replay_results`

| ツール | 読み書き | 内容 |
|---|---|---|
| `replay_estimate` | 読み取り専用 | 開始前の見積もり。判断の件数 × アーム数の呼び出し数・費用の見込み・`confirm_token`。何も送らず、何も書かない |
| `replay_start` | **書く**（`readOnlyHint: false`） | run を 1 件、**待ち状態（`pending`）で記録するだけ**。`replay_estimate` と同じ引数と、その `confirm_token` が要る |
| `replay_stop` | **書く**（`readOnlyHint: false`） | 動いている・待っている run の**停止フラグを立てる**だけ |
| `replay_progress` | 読み取り専用 | run の状態と進み具合（1 件、または新しい順の一覧） |
| `replay_results` | 読み取り専用 | run の結果。`jev_trial_compare` と同じ指標 |

**書き込みの範囲**: `replay_start` が書くのは `jev_replay_runs` への 1 行の追加、`replay_stop` が書くのは停止フラグだけ。**取引の設定・リスク・資金の値・`settings_set` のキー・リプレイのスイッチ `jev.replay.enabled` には触れない**。`jev.replay.enabled`（既定オフ。行が無い・壊れているときもオフ）は `jev.trial.enabled` と同じく `settings_set` のホワイトリストに**無く**、オンにできるのはアプリの Settings 画面だけ。MCP は外部（OpenRouter）へ**何も送らない**: 呼び出しはアプリ内のランナーが、スイッチがオン・キーあり・アームありのときだけ行う。API キー・応答本文（`response_json`）・エラーの抜粋・エラー本文は**返さない**。

- `replay_start` は **MCP の書き込み許可**（`settings_write_enable`）がオフなら `write_disabled`（403）。`replay_stop` は費用を減らすだけの操作なので書き込み許可のスイッチを見ない。
- 開始の条件は、(a) `jev.replay.enabled` がオン、(b) 動いている・待っている run が無い（**同時に 1 件**）、(c) 呼び出しが 1 件以上、(d) `confirm_token` が**その場で取り直した見積もり**と一致、の全部。(a)(b)(c)(d) のどれかが外れると `invalid_value`（422。`error.data.detail` に理由）で、何も書かない。見積もりの後に判断・アームの設定が変わると (d) で拒否される（**見積もりを経ない開始・上限を超える開始はできない**）。

引数（`replay_estimate` / `replay_start`）:

| 引数 | 内容 |
|---|---|
| `from` / `to` | 他のツールと同じ期間（JST の暦日、`[from, to)`、最大 366 日、既定は直近 30 日）。**ライブの判断の時刻**（`jev_decisions.timestamp`）で切る |
| `max_rows` | **必須**。対象にするライブの判断の件数の上限（1〜1000。古い順）。1 つの run の大きさを呼び出し側が決める |
| `arms` | 任意。対象のアーム id（`jev.trial.arms` の id。最大 8 個・重複不可）。省略は設定されている全アーム。設定に無い id は `invalid_argument` |
| `decision_type` | 任意。`trader` だけ（既定）。それ以外は `invalid_argument` |
| `confirm_token` | `replay_start` のみ必須。`replay_estimate` が返した `confirm_token` |

引数の誤り・未知の引数は `invalid_argument`（422。`detail` に規則）、期間の誤りは `invalid_period`（422）。

#### `replay_estimate` の応答の `data`

| キー | 内容 |
|---|---|
| `notice` | 固定の注意書き（件数の数え方・費用の見込みの意味・上限の共有・学習カットオフの扱い・送る入力） |
| `replay_enabled` | `jev.replay.enabled` の今の状態。`false` のあいだ `replay_start` は拒否される |
| `decision_type` / `from` / `to` / `max_rows` | 解決した指定（JST の日付） |
| `decisions` / `calls` | 対象の判断の件数・呼び出し数（判断 × アーム。アームごとの学習カットオフ以前の判断を除いた数） |
| `excluded_invalid_state` | 保存された入力が JSON として不正で送れない判断の数 |
| `max_cost_usd` | 費用の見込み。**予約と同じ最悪値側の単価**（入力トークンの推定 × `WorstCaseUSDPerMtok`）の合計。run はこの額を超える前に止まる |
| `list_price_usd` | 定価での費用の目安。定価が分からないアームがあれば `null`（0 ではない） |
| `arms[]` | アームごと。`arm_id`・`model`・`prompt_version`・`llm_cutoff`（公表済みの日付、無ければ `null`）・`llm_cutoff_status`（`verified` / `unverified`）・`llm_cutoff_label`（`確認済み` / `未確認`）・`reference_only`（`unverified` のとき `true`）・`calls`・`excluded_before_cutoff`（カットオフ以前で対象外にした判断の数）・`max_cost_usd`・`list_price_usd` |
| `confirm_token` | 見積もりの内容のハッシュ（指定・アームの設定・対象の判断・呼び出し数・費用）。**キーは含まない** |

**学習カットオフ**: アームに `llm_cutoff`（`YYYY-MM-DD`。ベンダーが公表した学習データの終端日。Settings のアーム設定 JSON の任意の項目）があれば、その日**より後**の判断だけを聞く。無いアームは `unverified`（**未確認**）で、判断は全部聞くが**結果は参考扱い**（`reference_only`）。日付は推測で入れない。

#### `replay_start` / `replay_stop` の応答の `data`

`RunView`（下記）。`replay_start` は `status: pending`。`replay_stop` は `stop_requested: true`（ランナーが次の呼び出しの前に `stopped` にする。記録済みの結果は残る）。終わった run の停止は `invalid_value`、存在しない `run_id` は `invalid_argument`。

#### `replay_progress`

引数: `run_id`（任意。省略は新しい順の一覧）・`limit`（任意。一覧の件数。既定 10・最大 50）。応答は `{runs: [RunView]}`。存在しない `run_id` は `invalid_argument`。

**`RunView`**: `run_id`・`status`（`pending` / `running` / `completed` / `stopped` / `failed`）・`stop_requested`・`stop_reason`（`stopped` / `failed` の理由: `requested` / `run_cost_cap` / `payment_required` / `key_not_set` / `no_arms`）・`blocked_reason`（`running` のあいだ新しい呼び出しを出していない理由: `daily_cap_overall` / `daily_cap_arm` / `stopped_*` / `throttled` / `check_failed`。日が変われば自動で続きから再開）・`spec`・`arms[]`（開始時点の設定。`llm_cutoff_status` / `llm_cutoff_label` / `reference_only`）・`decisions`・`list_price_usd`・`progress`（`calls_planned` / `calls_done` / `calls_failed` / `calls_remaining` / `counted_cost_usd` / `max_cost_usd`）・`created_by`・`created_at` / `started_at` / `finished_at` / `updated_at`。

#### `replay_results`

引数: `run_id`（必須）・`arms`（任意。比べるアームの部分集合）。応答の `data`:

| キー | 内容 |
|---|---|
| `notice` | 固定の注意書き（研究用でライブ・取引・学習・RAG に使わない・参考扱い・RAG 無しの入力） |
| `run` | `RunView` |
| `reference_only` | run のアームに `unverified`（未確認）があるとき `true` |
| `cost[]` | アームごと。`arm_id`・`llm_cutoff_status` / `llm_cutoff_label`・`rows`・`reference_only_rows`・`cost_usd`（報告値または定価の推定の合計）・`unknown_cost_rows`（費用が不明の行数。0 円ではない）・`counted_cost_usd`（上限の判定に数えた額） |
| `comparison` | `jev_trial_compare`（§3.32）と**同じ形・同じ指標**を、この run の行について: `cohort`・共通の成功数 `common_success_count`・アームごとの欠測率 `missing_rate`・方向の分布とライブとの一致・`outcomes[]`（`calibration_outcomes` での当たり率と、同じコホートのライブ自身の採点 `live_baseline`）・`counterfactual_proxy`（見送りの反実仮想ラベルのコスト控除後の `net_return_bps`。proxy）・`fairness.caveats` |

比較の相手は**同じ行のライブの判断**（`live_baseline`）。`cohort` は選んだアームすべてに聞けた判断だけなので、学習カットオフ後の判断だけを聞くアームがあるとその分だけ小さくなる（除外の件数は `replay_estimate` のアームごとの `excluded_before_cutoff`）。リプレイにはお試しの見送り記録が無いので `fairness.skipped` は空。`llm_cutoff_unverified` が `fairness.caveats` に出ているあいだは、同じ入力での公平な比較ではない。送った入力は**保存された入力そのまま**（過去の類似事例 RAG は足さない）なので、ライブのお試し（RAG あり）とは入力が違う。

## 作らないもの

多数決・合議・「採用すべきアーム」・自動昇格・結果の自動採用。リプレイの結果を、ライブの判断・取引・Policy・Risk・RAG の索引・Calibration・メタモデルの学習に混ぜる経路は無い。
