# API 仕様: Paper 実績 MCP の調整の実験ツール

`docs/api/endpoints/paper-results-mcp.md` §3.23〜§3.26 の分割章（行数上限のため別ファイル）。issue #871。設計・判定・最低取引件数の案は `docs/architecture/paper-results-mcp-experiments.md`。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。4 ツールとも `from` / `to` は取らず（`noPeriod`）、期間は実験が決める。

## 3. ツール

### 3.23 `experiment_create`

調整の実験を 1 件記録する。**変更そのものはしない**（設定は先に `settings_set`、モデル・プロンプトの版の切り替えは本体側で行ってから記録する）。書き込み許可（`paper-results-mcp.md` §7.2）がオフなら `write_disabled`（403）。

| 引数 | 必須 | 内容 |
|------|------|------|
| `hypothesis` | ○ | 仮説（自由文。1〜500 文字。改行可、そのほかの制御文字は不可） |
| `changes` | ○ | 変えた変数の配列。**ちょうど 1 要素**。0 件は `invalid_argument`、2 件以上は `multiple_variables`（422。何も保存しない） |
| `changes[].kind` | ○ | `setting` / `model` / `prompt` |
| `changes[].name` | ○ | 設定キー（`setting` はホワイトリストのキーだけ。外は `key_not_allowed`）/ モデルの呼び名 / プロンプトの版の呼び名。1〜200 文字 |
| `changes[].before` / `after` | ○ | 変更前・変更後の値（文字列か数値。1〜200 文字。互いに異なる） |
| `started_at` | | 実験の開始時刻（RFC3339）。変更が効き始めた時刻。省略は今。未来は不可 |
| `audit_id` | | `kind` が `setting` のとき、その変更の監査 ID（`settings_set` の応答の `audit_id`。その設定キーの `set` / `reset` の行）。交絡の検出で、その変更自体を除くために使う |

応答の `data`: 作った実験 `{id, created_at, actor("mcp"), client, hypothesis, variable:{kind, name, before, after, audit_id}, started_at, ended_at(null), status("running")}`。実行文は `tuning_experiments` への `INSERT` 1 件だけ。

### 3.24 `experiment_close`

実行中の実験を終える。引数: `experiment_id`（○）、`status`（`closed`＝終了・判定の対象〔既定〕/ `aborted`＝中止・判定を出さない）、`ended_at`（RFC3339。開始時刻以後・現在以前。省略は今）。ない ID は `experiment_not_found`（404）、終わった実験は `experiment_not_running`（409。再び開けない）、書き込み許可がオフなら `write_disabled`。応答は更新後の実験（3.23 と同じ形）。実行文は `UPDATE tuning_experiments SET status, ended_at WHERE id AND status = 'running'` の 1 件だけ。

### 3.25 `experiments_list`

実験の一覧（新しい順。読み取り専用）。引数: `status`（`running` / `closed` / `aborted`。省略は全部）、`limit`（1〜50、既定 20。超過は `limit_exceeded`）、`min_trades`（64〜10000。**下限 64（暫定・あいり未承認）は上げるのは可、下げるのは不可**。64 未満・10000 超は `invalid_value`（422。理由に指定値と下限を書く）で、黙って 64 に直さない。省略は 64）。

`data.experiments[]` の各要素: `experiment`（3.23 の形）、`min_trades`、`min_trades_source`（`floor_provisional_unapproved`＝下限の暫定値でまだ承認されていない / `argument_raised`＝呼び出しで下限より上げた）、`before_trades` / `after_trades`（前後のクローズ済み取引件数）、`confounded`、`verdict`、`judged`。1 行ごとに前後の集計 2 回を行う（`limit_exceeded`＝期間内の取引が 5,000 件を超える実験があるとき、一覧全体が失敗する）。

### 3.26 `experiment_compare`

実験 1 つの前後比較（読み取り専用）。引数: `experiment_id`（○）、`min_trades`（3.25 と同じ。下限 64、上げるのみ可）。**後** = 実験の開始から終了まで（実行中は今まで）、**前** = 開始の直前の同じ長さ。両方を半開区間 `[from, to)` として、`paper_performance_summary` と `risk_rejections` と**同じ関数**（`query.PerformanceSummary` / `query.Rejections`）で計算する。

`data`:

| フィールド | 内容 |
|------------|------|
| `experiment` | 3.23 の形 |
| `min_trades` / `min_trades_source` | 判定に要る前後それぞれの件数と、その出どころ（3.25 と同じ） |
| `before` / `after` | `{from, to, performance, risk_rejections}`。`performance` は `paper_performance_summary` の `data` と同じフィールド（`closed_trade_count`・`win_count`・`loss_count`・`win_rate`・`total_pnl_jpy`・`avg_win_jpy`・`avg_loss_jpy`・`max_drawdown_jpy`・`profit_factor` ほか。`by_origin[]` を含む）。`risk_rejections` は `risk_rejections` の `data` と同じフィールド（`risk_passed_count`・`buckets[]`・`other_reject_count`・`other_reject_breakdown[]` ほか） |
| `diff` | 後 − 前: `total_pnl_jpy`・`max_drawdown_jpy`（参考。`max_drawdown_jpy` は判定に使う） |
| `per_trade_pnl` | 判定の基礎になる検定（Welch の t 検定）。`before` / `after`: `{n, mean_jpy, sd_jpy}`（1 取引あたりの純損益（コスト控除後）の件数・平均・標本標準偏差。`paper_performance_summary` の `closed_trade_count` / `expectancy_jpy` と一致）、`diff`（平均の差、後 − 前）、`ci_low` / `ci_high`（差の信頼区間）、`ci_level`（0.95）、`t`（t 値）、`df`（Welch–Satterthwaite の自由度）。計算できない値（n<2、または前後とも全取引が同じ損益）は `null`。`n` が 0 のとき `mean_jpy` も `null` |
| `confounded` / `confounders[]` | 交絡の有無と一覧。要素: `kind`（`setting` / `model` / `prompt`）、`name`（設定キー、または判断種別 `scout` / `trader`）、`audit_id`・`at`（設定の変更のとき）、`values[]`（その期間に記録されたモデル ID / プロンプトの版） |
| `verdict` / `judged` | `aborted`（中止）/ `confounded`（交絡）/ `insufficient_trades`（前後どちらかが `min_trades` 未満）/ `not_computable`（件数は足りるが全取引が同じ損益で区間が出ない）→ 判定保留（`judged:false`）。`improved`（区間の下限 > 0 かつ最大ドローダウンが前以下。唯一の勝ち）/ `mixed`（下限 > 0 だがドローダウン悪化。勝ちと言わない）/ `worsened`（区間の上限 < 0）/ `inconclusive`（区間が 0 をまたぐ）→ `judged:true` |

判定の順と式・前提は `docs/architecture/paper-results-mcp-experiments.md` §4。**判定の基準は 1 取引あたりの純損益（コスト控除後）の平均の前後差の 95% 信頼区間と、最大ドローダウン**で、合計の損益の大小・勝率は使わない。取引は同日・同相場に偏りうるので区間は楽観的になりうる。前後は別の期間なので因果の証明にはならない。`min_trades` に届かない・交絡あり・中止のときも数値はそのまま返す（判定だけ出さない）。

### 受け入れ条件

- `experiment_create` で作った実験が `experiments_list` に出る。書き込み許可がオフなら作れない（`write_disabled`）
- 変数 2 つ以上の `experiment_create` は `multiple_variables` で、何も保存されない
- `experiment_compare` の前後の数値が、同じ期間の `paper_performance_summary` / `risk_rejections` と一致する
- 前後どちらかの取引件数が `min_trades`（下限 64）に届かない実験（63 件を含む）は判定を出さない（`insufficient_trades`、`judged:false`）。下限未満の `min_trades` は `invalid_value`
- 区間が 0 をまたぐ実験を `improved` と言わない。区間が 0 より上でも最大ドローダウンが悪化していれば `improved` ではなく `mixed`
- 実験期間中に対象以外の設定・モデル・プロンプトの版が変わると `confounded`
- 書き込みは `tuning_experiments` の `INSERT` と終了の `UPDATE` だけ（`TestSettingsWritePathIsNarrow`）
