# Paper 実績 MCP のエラー

`docs/architecture/paper-results-mcp.md` と `docs/api/endpoints/paper-results-mcp.md` が参照するエラー表。クライアントへ返す `message` は次の固定文だけとし、SQL・ファイルパス・ヘッダの値・スタックは含めない。サーバー側の slog に残す項目は §3 だけである（行の中身と認証ヘッダは残さない）。

認証失敗とオフは、DB に触れる前に返す（FR-MCP-2、FR-MCP-1）。

## 1. コード

| `code` | HTTP | 条件 | `message` |
|--------|------|------|-----------|
| `not_listening` | なし（TCP の接続拒否） | `enabled` が false、設定ファイルが読めない、`api_key` が空、`listen` がアーキテクチャ §3.1 の許すアドレスでない、または（任意の）Access 照合を有効にしたのに id か secret が空。bind していない | （ボディ無し。クライアントは接続拒否だけを観測する） |
| `unauthorized` | 401 | `Authorization: Bearer` が無い、または `api_key` と一致しない。SQL は発行しない。必須の認証失敗はこれだけ | 認証に失敗しました。 |
| `access_denied` | 401 | 任意。アーキテクチャ §3.5 で `verify_access_headers` が true のときだけ。`CF-Access-Client-Id` または `CF-Access-Client-Secret` が無い、またはローカル設定と一致しない。SQL は発行しない。必須要件の失敗コードではない | 認証に失敗しました。 |
| `invalid_period` | 422 | `from` / `to` の形式が `YYYY-MM-DD` でない、片方だけ、`to <= from`、期間が 366 日を超える、`to` が JST の翌日より未来 | 期間の指定が不正です。 |
| `limit_exceeded` | 422 | 要求の `limit` がツール上限を超える、または期間内の集計対象件数が上限（クローズ済み 5,000 件、horizon 8 種）を超える。部分結果は返さない | 取得量が上限を超えています。期間を狭めてください。 |
| `db_locked` | 503 | `mode=ro` の接続が 1 秒の `busy_timeout` 以内にロックを取れない | データベースが使用中です。しばらくしてから再度呼んでください。 |
| `db_unavailable` | 503 | DB ファイルが無い、開けない、`PRAGMA query_only=ON` に失敗した、または必要な列が無くツールを実行できない | データベースを読み取れません。 |
| `query_timeout` | 504 | ツール 1 回の処理が 3 秒を超えた（待ち時間を含む）。クエリはキャンセルし、自動では再試行しない | 読み取りが時間内に終わりませんでした。 |
| `concurrency_limited` | 429 | すでに 2 件のツール呼び出しが実行中。3 件目は SQL を始めない | 同時に処理できる件数を超えています。 |
| `internal` | 500 | 上記以外の失敗。`message` は固定。原因は slog のみ | 読み取りに失敗しました。 |

`unauthorized` は、キーが無いときと違うときで同じ文である。オフかどうかは返さない（#347 が未認証の相手にオフを返さないのと同じ考え方）。オフそのものは待受が無いので、401 とは観測が分かれる（接続拒否か、401 か）。`access_denied` は Cloudflare の任意照合をオンにしたときだけで、`message` は `unauthorized` と同じ文にする（どちらが欠けたかは返さない）。

## 2. ツール成功時の形

認証を通過し、ツールが読み取れたときは HTTP 200。ボディは `{"tool":"<名前>","data":{...}}`。`data` のフィールドは API 仕様のホワイトリストだけで、エラー用の `code` は付けない。

未知のツール名と未知の引数は `invalid_period` にしない。HTTP 422 のまま、次の `code` と固定文で返す。

| `code` | HTTP | 条件 | `message` |
|--------|------|------|-----------|
| `invalid_argument` | 422 | ツール名が API 仕様 §3 の 8 つ以外、またはそのツールが定義していない引数がある | 引数が不正です。 |

期間の不正は `invalid_period` のまま分ける（テストが期間と引数を区別できるようにするため）。

**実装での載せ方（MCP の JSON-RPC 2.0 over HTTP）**: エラー応答は上表の HTTP ステータスで返し、ボディは `{"jsonrpc":"2.0","id":…,"code":"<code>","message":"<固定文>","error":{"code":-32000,"message":"<固定文>","data":{"code":"<code>"}}}`。成功は HTTP 200 の JSON-RPC `result` で、`structuredContent` が `{"tool":"<名前>","data":{…}}`、`content[0].text` が同じ JSON の文字列。JSON-RPC として不正なボディ（解析不能・バッチ・`jsonrpc` が 2.0 でない）は認証通過後に 400、未対応メソッドは `-32601`。

## 3. ログに残すもの / 残さないもの

残す: `tool`、`code`、`duration_ms`、`from`、`to`、返した件数。認証失敗は `code` だけ（ツール名がまだ決まっていなければ `tool` は空）。

残さない: `Authorization`、`api_key`、SQL の全文、結果の行、`raw_data_json`、`state_json`、`secrets`。Cloudflare の任意照合を使うときも `CF-Access-Client-Id`、`CF-Access-Client-Secret`、`access_client_secret` は残さない。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-17 | 新規作成 | issue #783。#347 のエラー表の書式（code・条件・固定の案内文）に合わせた |
| 1.1 | 2026-10-17 | 必須の認証失敗を `unauthorized` だけにした。`access_denied` は Cloudflare の任意照合のときだけ | 悠太さん指示。Cloudflare Access を必須要件から外す |
| 1.2 | 2026-10-18 | 実装での JSON-RPC への載せ方を §2 に追記。`message` と HTTP の表は変更なし | issue #783 の実装 |
