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
| `limit_exceeded` | 422 | 要求の `limit` がツール上限を超える、または期間内の集計対象件数が上限（クローズ済み 5,000 件、horizon 8 種）を超える。部分結果は返さない。`order_proposal_submit` では、JST の 1 日に受け付けた注文案が 20 件に達している（`error.data.detail` に「注文案は JST の 1 日に 20 件までです」。何も書かない）。`message` は固定文のまま | 取得量が上限を超えています。期間を狭めてください。 |
| `db_locked` | 503 | `mode=ro` の接続が 1 秒の `busy_timeout` 以内にロックを取れない。設定・注文案の書き込みの接続が 2 秒以内に書き込みロックを取れないときも同じ（何も保存しない） | データベースが使用中です。しばらくしてから再度呼んでください。 |
| `db_unavailable` | 503 | DB ファイルが無い、開けない（設定の書き込み接続を含む）、`PRAGMA query_only=ON` に失敗した、または必要な列が無くツールを実行できない | データベースを読み取れません。 |
| `query_timeout` | 504 | ツール 1 回の処理が 3 秒を超えた（待ち時間を含む）。クエリはキャンセルし、自動では再試行しない | 読み取りが時間内に終わりませんでした。 |
| `concurrency_limited` | 429 | すでに 2 件のツール呼び出しが実行中。3 件目は SQL を始めない | 同時に処理できる件数を超えています。 |
| `write_disabled` | 403 | `settings_set` を呼んだが、書き込み許可がオフ（`runtime_settings` の `mcp.settings_write_enabled` が `0`、または壊れた値）。何も保存しない。`settings_write_enable` と読み取りのツールは使える | 設定の書き込みは許可されていません。settings_write_enable か Settings の「外部連携」で許可してください。 |
| `key_not_allowed` | 403 | `settings_set` の `key`、または `experiment_create` の `kind=setting` の `name` がホワイトリスト（API 仕様 §3.10）に無い。未知のキーも、Live・ブートストラップ・秘密・書き込みスイッチ自身などの既知の別キーも同じ。書き込み許可の確認や DB への接触より前に返す | このキーは MCP から変更できません。settings_get に出るキーだけ変更できます。 |
| `invalid_value` | 422 | ホワイトリストのキーだが、値が GUI の検証（範囲・形式）に通らない。何も保存しない。`error.data.detail` に、検証の理由（呼び出し側が送った値と許容範囲だけを引く固定の文型。秘密は載らない）を付ける | 値が不正です。settings_get の constraint を確認してください。 |
| `proposals_disabled` | 403 | `order_proposal_submit` を呼んだが、注文案の受け付けがオフ（`runtime_settings` の `mcp.order_proposals_enabled` の行が無い・`0`・壊れた値。既定はオフ）。何も書かない。`order_proposal_get` と `order_proposals_disable` は使える。オンにできるのは Settings 画面だけ | 注文案の受け付けはオフです。Settings の「外部連携」でオンにしてください（MCP からはオンにできません）。 |
| `idempotency_conflict` | 409 | `order_proposal_submit` の `idempotency_key` が、内容の違う注文案ですでに使われている（内容が同じなら再送として最初の案を返し、このコードにならない）。何も書かない | この idempotency_key は内容の違う案で使われています。別のキーで出し直してください。 |
| `proposal_not_found` | 404 | `order_proposal_get` / `order_proposal_cancel` の `proposal_id` の注文案が無い | その proposal_id の注文案はありません。 |
| `not_cancellable` | 409 | `order_proposal_cancel` の注文案が、もう取り消せない状態（`filled` / `closed` / `rejected` / `expired` / `cancelled` / `failed`、またはアプリが処理中の `processing`） | この注文案はもう取り消せません（約定済み・終了済み、または処理中）。order_proposal_get で状態を確認してください。 |
| `multiple_variables` | 422 | `experiment_create` の `changes` が 2 つ以上（同じ変数の 2 回も含む）。何も書かない（issue #871） | 1 つの実験で変えられる変数は 1 つだけです。changes を 1 つにして、変数ごとに実験を分けてください。 |
| `experiment_not_found` | 404 | `experiment_compare` / `experiment_close` の `experiment_id` の実験が無い | その experiment_id の実験はありません。experiments_list で確認してください。 |
| `experiment_not_running` | 409 | `experiment_close` の実験が、もう終わっている（`closed` / `aborted`）。再び開けない。何も書かない | この実験はもう終わっています（再び開くことはできません）。 |
| `internal` | 500 | 上記以外の失敗。`message` は固定。原因は slog のみ | 読み取りに失敗しました。 |

`unauthorized` は、キーが無いときと違うときで同じ文である。オフかどうかは返さない（#347 が未認証の相手にオフを返さないのと同じ考え方）。オフそのものは待受が無いので、401 とは観測が分かれる（接続拒否か、401 か）。`access_denied` は Cloudflare の任意照合をオンにしたときだけで、`message` は `unauthorized` と同じ文にする（どちらが欠けたかは返さない）。

## 2. ツール成功時の形

認証を通過し、ツールが読み取れたときは HTTP 200。ボディは `{"tool":"<名前>","data":{...}}`。`data` のフィールドは API 仕様のホワイトリストだけで、エラー用の `code` は付けない。

未知のツール名と未知の引数は `invalid_period` にしない。HTTP 422 のまま、次の `code` と固定文で返す。

| `code` | HTTP | 条件 | `message` |
|--------|------|------|-----------|
| `invalid_argument` | 422 | ツール名が API 仕様 §3 の 28 個（読み取り 12、設定 6、注文案 6、実験 4）以外、そのツールが定義していない引数がある、`settings_set` の `key` / `value` が欠けている・型が違う、`settings_diff` の指定が不正（両方指定・存在しない ID・RFC3339 でない時刻）、`order_proposal_submit` の引数が API 仕様 §3.16 の規則に合わない（必須の欠け・`bucket` / `action` / `order_type` の範囲外・`quantity` と `budget_jpy` の両方・損切りが指値以上・期限が受け付け時点以前または 24 時間より先・文字数超過・制御文字。`error.data.detail` に規則の理由を付ける。送った値は引用しない）、`order_proposal_get` の `proposal_id` が欠けている・1 未満、実験ツールの引数（`changes` が 0 件、`kind` / 値 / 仮説 / `started_at` / `ended_at` / `audit_id` の規則違反、`experiment_id` が欠けている・1 未満、`min_trades` が範囲外。`error.data.detail` に規則の理由を付ける） | 引数が不正です。 |

期間の不正は `invalid_period` のまま分ける（テストが期間と引数を区別できるようにするため）。

**実装での載せ方（MCP の JSON-RPC 2.0 over HTTP）**: エラー応答は上表の HTTP ステータスで返し、ボディは `{"jsonrpc":"2.0","id":…,"code":"<code>","message":"<固定文>","error":{"code":-32000,"message":"<固定文>","data":{"code":"<code>"}}}`。成功は HTTP 200 の JSON-RPC `result` で、`structuredContent` が `{"tool":"<名前>","data":{…}}`、`content[0].text` が同じ JSON の文字列。JSON-RPC として不正なボディ（解析不能・バッチ・`jsonrpc` が 2.0 でない）は認証通過後に 400、未対応メソッドは `-32601`。

## 3. ログに残すもの / 残さないもの

残す: `tool`、`code`、`duration_ms`、`from`、`to`、返した件数。設定ツールの値（保存した値・変更前後）は残さない（それは監査ログの表 `mcp_settings_audit` に残る。API 仕様 §3.14）。認証失敗は `code` だけ（ツール名がまだ決まっていなければ `tool` は空）。

残さない: `Authorization`、`api_key`、SQL の全文、結果の行、`raw_data_json`、`state_json`、`secrets`。Cloudflare の任意照合を使うときも `CF-Access-Client-Id`、`CF-Access-Client-Secret`、`access_client_secret` は残さない。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-17 | 新規作成 | issue #783。#347 のエラー表の書式（code・条件・固定の案内文）に合わせた |
| 1.1 | 2026-10-17 | 必須の認証失敗を `unauthorized` だけにした。`access_denied` は Cloudflare の任意照合のときだけ | 悠太さん指示。Cloudflare Access を必須要件から外す |
| 1.2 | 2026-10-18 | 実装での JSON-RPC への載せ方を §2 に追記。`message` と HTTP の表は変更なし | issue #783 の実装 |
| 1.3 | 2026-10-20 | `invalid_argument` の条件のツール数を 9 つに更新（`health_today` を追加）。`message` と HTTP の表は変更なし | issue #808 |
| 1.4 | 2026-10-20 | 設定ツールのコード `write_disabled`（403）・`key_not_allowed`（403）・`invalid_value`（422、`error.data.detail`）を追加。`invalid_argument` の条件と `db_locked` / `db_unavailable` の条件を設定ツールに合わせて追記 | issue #806・#807 |
| 1.5 | 2026-10-21 | 注文案ツールのコード `proposals_disabled`（403）・`idempotency_conflict`（409）・`proposal_not_found`（404）を追加。`limit_exceeded`（1 日 20 件）と `invalid_argument`（注文案の引数規則。`error.data.detail`）の条件を追記し、ツール数を 18 に更新 | issue #827（親 #826） |
| 1.6 | 2026-10-21 | `order_proposal_cancel` のコード `not_cancellable`（409）を追加。`proposal_not_found` を取消にも使う。ツール数を 19 に更新 | issue #828（親 #826） |
| 1.7 | 2026-10-22 | 実験ツールのコード `multiple_variables`（422）・`experiment_not_found`（404）・`experiment_not_running`（409）を追加。`key_not_allowed` を `experiment_create` にも使い、`write_disabled` を実験の書き込みにも使う。`invalid_argument` の条件とツール数（25）を更新 | issue #871 |
