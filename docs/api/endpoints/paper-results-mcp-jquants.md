# API 仕様: Paper 実績 MCP — J-Quants の取り込み状況（§3.39）

`docs/api/endpoints/paper-results-mcp.md` §3.39 の分割章（行数上限のため別ファイル）。issue #1093。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md`。設計は `docs/architecture/overview/integrations-jquants.md`、運用は `docs/environment/setup/jquants.md`。

## §3.39 `jquants_status`

J-Quants 読み取りアダプタの記録状況を返す。件数・日数・日付・固定コードだけで、取引の判断・Policy・Risk・発注・Jev には使われない。J-Quants の利用条件（私的利用・外部へ送らない）のため、価格・銘柄・キーは返さない。

### 引数

| 名前 | 型 | 必須 | 説明 |
|------|-----|------|------|
| from / to | string | いいえ | 期間（JST の日付。`paper-results-mcp.md` §2 と同じ。省略は直近 30 日）。`daily_days_in_period` / `minute_days_in_period` の数え方にだけ使う（半開区間 `[from, to)` の取引日の数） |

未知の引数は `invalid_argument`。**オン・オフ・契約のフラグ・上限・埋め戻す日数・削除の依頼を変える引数は無い**（`jquants.*` は Settings 画面だけで変えられ、`settings_set` の許可リストにも無い。テストで固定）。したがってこの MCP からは、取り込みをオンにすることも、データを削除することもできない。

### 戻り値（`data`）

```json
{
  "enabled": false,
  "plan_daily": false,
  "plan_minute": false,
  "daily_request_cap": 200,
  "requests_today": 0,
  "rows": { "symbols": 0, "daily_bars": 0, "minute_bars": 0, "coverage": 0 },
  "days_from": "2026-10-19",
  "days_to": "2026-11-18",
  "daily_days_in_period": 0,
  "minute_days_in_period": 0,
  "failures_recent": 0,
  "last_failure_code": "",
  "last_success_at": null
}
```

- `enabled` / `plan_daily` / `plan_minute` / `daily_request_cap`: 運用設定の実効値（`jquants.enabled` / `jquants.plan_daily` / `jquants.plan_minute` / `jquants.daily_request_cap`。行が無い・壊れている間は、オフ・未契約・200）。
- `requests_today`: 今日（JST）に J-Quants へ送った要求数（ページ送りを含む。`daily_request_cap` に数えられる分）。
- `rows`: 研究用の表の全期間の行数（`research_jquants_symbols` / `_daily_bars` / `_minute_bars` / `_coverage`）。
- `days_from` / `days_to`: 期間の JST の日付（`days_to` は含まない）。`daily_days_in_period` / `minute_days_in_period`: 期間内に日足・分足が 1 行以上ある日の数。
- `failures_recent`: 直近 7 日（今日まで）の失敗した要求の数。`last_failure_code`: 最後の失敗の固定コード（`http_401` / `http_403` / `http_429` / `http_4xx` / `http_5xx` / `transport` / `decode`。失敗が無ければ空文字）。`last_success_at`: 最後に取得に成功した時刻（UTC の RFC3339。まだなければ `null`）。

### 返さないもの

API キー（`JQUANTS_API_KEY`）・URL・エラーの本文・価格・出来高・銘柄コード・銘柄名。`secrets` 表は読まない。読むのは `research_jquants_*` の件数と日付、`runtime_settings` の`jquants.*`だけ。

### テスト

`internal/service/paperresultsmcp/jquantstests`（件数・要求数・最終成功・失敗の固定コード・既定オフ・秘密と市場データを返さないこと・引数の厳格さ・書き込めないこと。`jquants.*`が`settings_set`の許可リストに無いことは`internal/config/jquantssettings`のテストで固定）。ツールの登録は `internal/service/paperresultsmcp/jquants_tool.go`、SQL は `internal/service/paperresultsmcp/jquantsdiag`。
