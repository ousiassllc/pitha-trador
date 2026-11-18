# API 仕様: Paper 実績 MCP の立花の日足の研究用収集の状況ツール

`docs/api/endpoints/paper-results-mcp.md` §3.36 の分割章（行数上限のため別ファイル）。issue #1082。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。読み取り専用（設定も記録も書かない）。設計は `docs/architecture/paper-results-mcp-research-bars.md`、表は `docs/architecture/er/tables-research.md`。

## 3. ツール

### 3.36 `research_daily_bars_status`

ブローカーが kabu のままでも、夜間に立花証券 e支店の日足を `daily_bars` に記録し、過去分を埋め戻す**研究用の収集**（取引に使わない。Settings の `research.tachibana_bars.enabled`、既定オフ）の状況を返す。

引数: なし（期間の `from` / `to` も取らない）。何か渡すと `invalid_argument`。

応答の `data`:

| キー | 内容 |
|---|---|
| `status` | `state`（`disabled` / `not_configured` / `broker_is_tachibana` / `waiting` / `running` / `ok` / `error`。状態の行がまだ無いときは `unknown`）、`detail`（状態の説明。`: ` より後ろ（ブローカーや DB 自身の文言）は返さず、100 文字で切る）、`updated_at`（RFC3339 UTC。行が無ければ `null`）。認証情報・URL・秘密鍵のパスは持たない |
| `daily_bars` | `symbols`（`daily_bars` にある銘柄数）、`first_trade_date` / `last_trade_date`（無ければ `null`）、`research_bars_saved`（研究用の収集が書いた日足の累計。`research_daily_bar_progress.bars_saved` の和）。行数は `daily_bars` が大きく 3 秒の上限に収まらないので返さない。銘柄数と最初・最後の日は主キーの索引の飛び読みで求める。**夜間バッチ（立花がブローカーのとき）が書いた分も含む** |
| `backfill` | `universe`（最新の研究用の夜の対象銘柄数。夜が無ければ 0）、`backfilled`（一度でも取得に成功した銘柄数 = `first_ok_night` が NULL でない行）、`percent`（`backfilled / universe` の百分率、小数 1 桁、100 を超えない。`universe` が 0 なら `null`）、`failed_symbols`（直近の試みが失敗した銘柄数 = `last_ok = 0`） |
| `runs[]` | 直近 10 夜（`run_date` の新しい順）。`run_date` / `status`（`running` / `succeeded` / `failed`）/ `started_at` / `finished_at`（実行中は `null`）/ `symbols` / `requests` / `saved_bars` / `failed` / `duration_ms` / `reason`（`detail` と同じ切り方） |
| `bars_per_day[]` | 直近 10 立会日（`daily_bars` の最後の日から 30 暦日の範囲。新しい順）の `trade_date` と `bars`（その日の日足の本数。銘柄数に近いほど欠けが無い） |

`data` に出す列は上の表のものだけ（結果の構造体に列挙。SQL は名前付きの列の SELECT のみ）。読むのは `research_tachibana_status` / `research_daily_bar_runs` / `research_daily_bar_progress` / `daily_bars`。`research_daily_bars_status` は設定の値も `runtime_settings` も返さない（スイッチの値は Settings 画面で見る）。
