# API 仕様: Paper 実績 MCP — 開示・ニュースの追加の情報源（§3.38）

`docs/api/endpoints/paper-results-mcp.md` §3.38 の分割章（行数上限のため別ファイル）。issue #1096。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。読み取り専用（設定も記録も書かない）。表は `docs/architecture/er/tables-disclosure-sources.md`、設計は `docs/architecture/overview/integrations-disclosure-sources.md`。

## §3.38 `disclosure_sources_status`

EDINET の書類一覧の記録と、立花ニュースの記録の、状況を返す。件数・最終取得時刻・失敗件数だけで、取引の判断・Policy・Risk・発注・既存の開示見送りルールには使われない。

### 引数

| 名前 | 型 | 必須 | 説明 |
|------|-----|------|------|
| from / to | string | いいえ | 期間（JST の日付。`paper-results-mcp.md` §2 と同じ。省略は直近 30 日）。`recorded_in_period` の数え方にだけ使う（記録した書類・ニュースの時刻が期間に入る行数） |

未知の引数は `invalid_argument`。**オン・オフ・上限・保持期間を変える引数は無い**（`disclosure_src.*` は Settings 画面だけで変えられ、`settings_set` の許可リストにも無い）。

### 戻り値（`data`）

```json
{
  "rows_from": "2026-10-19T00:00:00Z",
  "rows_to": "2026-11-18T00:00:00Z",
  "sources": [
    {
      "source": "edinet",
      "enabled": false,
      "recorded_total": 0,
      "recorded_in_period": 0,
      "oldest_recorded_at": null,
      "newest_recorded_at": null,
      "request_count": 0,
      "failure_count": 0,
      "last_fetched_at": null,
      "last_success_at": null,
      "last_failure_at": null,
      "last_failure_code": "",
      "requests_today": 0,
      "daily_request_cap": 30
    },
    { "source": "tachibana_news", "enabled": false, "…": "同じ形。daily_request_cap の既定は 24" }
  ]
}
```

- `sources` は常に `edinet`、`tachibana_news` の順。
- `enabled`: その情報源のスイッチ（`disclosure_src.edinet.enabled` / `disclosure_src.tachibana_news.enabled`。行が無い・壊れている間は `false`）。
- `recorded_total` / `recorded_in_period` / `oldest_recorded_at` / `newest_recorded_at`: 記録した行数（全期間・期間内）と最古・最新の行の時刻（書類の提出日時・ニュースの時刻。UTC の RFC3339）。
- `request_count` / `failure_count`: 累計の要求数・失敗数。`last_fetched_at` / `last_success_at` / `last_failure_at`: 最後に要求を出した・成功した・失敗した時刻（まだなければ `null`）。`last_failure_code`: 最後の失敗の固定コード（`http_401` / `http_403` / `http_429` / `http_4xx` / `http_5xx` / `timeout` / `transport` / `invalid_response` / `api_error`。なければ空）。
- `requests_today` / `daily_request_cap`: 今日（JST）に出した要求数と、1 日の要求数の上限（`disclosure_src.*.daily_request_cap`。既定は EDINET 30、立花 24）。保存された日が今日でないときは `requests_today` は 0。

立花ニュースは、立花の実機計測（#725）が終わるまで本番に繋がらないため、行・要求数は 0 のままになる。

### 返さないもの

API キー（`EDINET_API_KEY`）・認証情報・URL・エラーの本文・書類の表題・見出し・銘柄コード・書類管理番号。`secrets` 表は読まない。読むのは `research_edinet_documents` / `research_tachibana_news` / `research_disclosure_source_status` の件数と時刻・コード、および `disclosure_src.*` の `runtime_settings` だけ（固定キーの許可リスト）。

### テスト

`internal/service/paperresultsmcp/disclosuresrctests`（件数・最終取得時刻・失敗件数・既定オフ・秘密と本文を返さないこと・引数の厳格さと書き込めないこと）。ツールの登録は `internal/service/paperresultsmcp/disclosure_sources_tool.go`、SQL は `internal/service/paperresultsmcp/disclosuresrcdiag`。
