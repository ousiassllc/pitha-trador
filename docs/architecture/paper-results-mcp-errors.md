# Paper 実績 MCP のエラー

`docs/architecture/paper-results-mcp.md` と `docs/api/endpoints/paper-results-mcp.md` が参照するエラー表。クライアントへ返す `message` は次の固定文だけとし、SQL・ファイルパス・ヘッダの値・スタックは含めない。サーバー側の slog に残す項目は §3 だけである（行の中身と認証ヘッダは残さない）。

認証失敗とオフは、DB に触れる前に返す（FR-MCP-2、FR-MCP-1）。

## 1. コード

| `code` | HTTP | 条件 | `message` |
|--------|------|------|-----------|
| `not_listening` | なし（TCP の接続拒否） | `enabled` が false、設定ファイルが読めない、または認証因子が一つも無い。bind していない | （ボディ無し。クライアントは接続拒否だけを観測する） |
| `access_denied` | 401 | `verify_access_headers` が true で、`CF-Access-Client-Id` または `CF-Access-Client-Secret` が無い、またはローカル設定と一致しない。SQL は発行しない | 認証に失敗しました。 |
| `unauthorized` | 401 | `api_key` が設定されていて、`Authorization: Bearer` が無い、または一致しない。SQL は発行しない | 認証に失敗しました。 |
| `invalid_period` | 422 | `from` / `to` の形式が `YYYY-MM-DD` でない、片方だけ、`to <= from`、期間が 366 日を超える、`to` が JST の翌日より未来 | 期間の指定が不正です。 |
| `limit_exceeded` | 422 | 要求の `limit` がツール上限を超える、または期間内の集計対象件数が上限（クローズ済み 5,000 件、horizon 8 種）を超える。部分結果は返さない | 取得量が上限を超えています。期間を狭めてください。 |
| `db_locked` | 503 | `mode=ro` の接続が 1 秒の `busy_timeout` 以内にロックを取れない | データベースが使用中です。しばらくしてから再度呼んでください。 |
| `db_unavailable` | 503 | DB ファイルが無い、開けない、`PRAGMA query_only=ON` に失敗した、または必要な列が無くツールを実行できない | データベースを読み取れません。 |
| `query_timeout` | 504 | ツール 1 回の処理が 3 秒を超えた（待ち時間を含む）。クエリはキャンセルし、自動では再試行しない | 読み取りが時間内に終わりませんでした。 |
| `concurrency_limited` | 429 | すでに 2 件のツール呼び出しが実行中。3 件目は SQL を始めない | 同時に処理できる件数を超えています。 |
| `internal` | 500 | 上記以外の失敗。`message` は固定。原因は slog のみ | 読み取りに失敗しました。 |

`access_denied` と `unauthorized` の `message` は同じ文である。どちらが欠けていたか、オフかどうかは返さない（#347 が未認証の相手にオフを返さないのと同じ考え方）。オフそのものは待受が無いので、この 2 コードとは観測が分かれる（接続拒否か、401 か）。

## 2. ツール成功時の形

認証を通過し、ツールが読み取れたときは HTTP 200。ボディは `{"tool":"<名前>","data":{...}}`。`data` のフィールドは API 仕様のホワイトリストだけで、エラー用の `code` は付けない。

未知のツール名と未知の引数は `invalid_period` にしない。HTTP 422 のまま、次の `code` と固定文で返す。

| `code` | HTTP | 条件 | `message` |
|--------|------|------|-----------|
| `invalid_argument` | 422 | ツール名が API 仕様 §3 の 8 つ以外、またはそのツールが定義していない引数がある | 引数が不正です。 |

期間の不正は `invalid_period` のまま分ける（テストが期間と引数を区別できるようにするため）。

## 3. ログに残すもの / 残さないもの

残す: `tool`、`code`、`duration_ms`、`from`、`to`、返した件数。認証失敗は `code` だけ（ツール名がまだ決まっていなければ `tool` は空）。

残さない: `Authorization`、`CF-Access-Client-Id`、`CF-Access-Client-Secret`、`api_key`、`access_client_secret`、SQL の全文、結果の行、`raw_data_json`、`state_json`、`secrets`。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-17 | 新規作成 | issue #783。#347 のエラー表の書式（code・条件・固定の案内文）に合わせた |
