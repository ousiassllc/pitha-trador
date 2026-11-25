# API 仕様: Paper 実績 MCP のデスクトップアプリ自動更新ツール

`docs/api/endpoints/paper-results-mcp.md` §3.52 の分割章（行数上限のため別ファイル）。issue #1222。要件は FR-MCP-52（`docs/requirements/functional/components-platform.md`）、設計は `docs/architecture/update-tools/paper-results-mcp-update.md`。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。

デスクトップアプリ（`cmd/desktop`）の自動更新（`internal/service/updater`、issue #65・#76）の状態を見る・今すぐ確認する・導入を依頼する 3 つのツール。**アップデーター本体は再実装しない**: GitHub の取得・署名とチェックサムの検証・URL とサイズの制限・SafeGate・インストーラーの起動は、すべてアプリ側の既存の `Checker.CheckForUpdate` と `QuitForUpdate` が行う。

## 3. ツール

### 3.52 `update_status` / `update_check_now` / `update_apply`

3 つとも**引数を取らない**（`inputSchema` は `{"type":"object","properties":{},"additionalProperties":false}`）。版・URL・アセットの指定、SafeGate や署名検証を飛ばす引数、ダウングレードは、スキーマにも実装にも無い（何か渡すと `invalid_argument`・422 で何もしない）。

| ツール | 読み書き | 内容 |
|---|---|---|
| `update_status` | 読み取り専用 | アプリが公開している更新の状態。`Checker.Status()` と同じ内容（Settings の更新パネルと同じ文言） |
| `update_check_now` | 書き込み（依頼の記録だけ） | GitHub Releases への即時確認。スケジューラーの周期確認と同じ `Checker.CheckForUpdate`（`checkMu` で周期確認・Settings の手動確認と直列）。**アプリの終了も導入もしない**。書き込み許可（`settings_write_enable`）には依存しない |
| `update_apply` | 書き込み（導入の依頼） | `update_check_now` で署名とチェックサムを検証したインストーラーを、既存の `QuitForUpdate` で導入する依頼。**書き込み許可がオンのときだけ**動く |

`update_status` の `data`（`update_check_now` はこれに `check` を足す）:

| キー | 内容 |
|---|---|
| `unavailable` | `true` は、アップデーターが無いビルド（`cmd/server`）か、アプリが動いていない（公開された状態が 1 分以上更新されていない）。エラーにはならない。このとき以降のキーは `current_version`（この MCP の版）と `false` のフラグだけ |
| `unavailable_reason` | `unavailable` のときの固定文（Settings の更新パネルと同じ） |
| `current_version` | 動いているアプリの版（`internal/version.Version`。リリースでないビルドは `dev`）。コミットは埋め込まれていないので出さない |
| `latest_version` | 最後の確認で見た最新リリースのタグ。新しい版があるときだけ |
| `available` / `blocked` / `ready` / `dev_build` / `no_release` | `updater.Status` の同名のフラグ。`ready` は検証済みのインストーラーがあること |
| `blocked_kind` / `blocked_reason` | 導入を止めている条件の種別（SafeGate の `open_positions` / `kill_switch` / `recent_order` / `check_failed`、と、取引日の立会時間帯 JST 9:00〜15:30 の `market_hours`）と、Settings と同じ文言。`blocked` のときだけ。SafeGate の種別が先で、SafeGate が通っているのに立会時間帯のときだけ `market_hours`（`blocked_reason` は「取引時間中（9:00〜15:30）のため」）。立会時間帯の `market_hours` は MCP の `update_status` にだけ出る表示で、`Checker.Status`・Settings の更新パネル・周期確認の保留（`blocked`）は変わらない |
| `checked_at` | 最後の確認が終わった時刻（RFC3339 UTC）。未確認なら省略 |
| `error_kind` / `error_reason` | 最後の確認が失敗したときの種別（`network` / `rate_limit` / `access` / `verification` / `release` / `other`）と、Settings と同じ文言 |
| `last_apply` | 直近の `update_apply` の依頼（監査。`requested_at` / `actor`（`mcp`）/ `client` / `result`（`accepted` / `deferred` / `expired` / `pending`）/ `reason`）。依頼が無ければ省略 |

**出さないもの**: `LastError` の生の文、インストーラーのローカルパス、ダウンロード URL、公開鍵、署名。種別と上の固定文だけを返す（issue #241 と同じ方針）。

`update_check_now` の `check`: `completed`（確認が終わった。結果は `update_status` と同じ形の状態）／`failed`（確認が失敗した。`error_kind` が理由）／`running`（アプリが確認中で、この呼び出しの待ち時間 2 秒では終わらなかった。あとで `update_status` を見る）／`expired`（アプリが一定時間内に着手せず取り下げた）。`dev` ビルドは確認をスキップし `dev_build=true`（`check=completed`）。アップデーターが無いときは依頼を記録せず `unavailable=true` を返す。

`update_apply` の `data`:

| キー | 内容 |
|---|---|
| `result` | `accepted`（導入を受け付けた。応答のあとアプリは通常の終了手順に入り、MCP は先に止まり、インストーラーが起動する）／`deferred`（導入しなかった。アプリは終了しない）／`pending`（アプリが着手したが、この呼び出しの間に結果が出なかった。`update_status` の `last_apply` で見る） |
| `reason` / `reason_text` | `deferred` の理由: SafeGate の種別（`open_positions` / `kill_switch` / `recent_order` / `check_failed`。導入の直前に評価し直す）、`market_hours`（取引日の JST 9:00〜15:30。昼休みを含む。SafeGate が通っていても導入しない。立会・取引日の判定は `internal/service/marketcalendar` の `TSE`。土日・祝日・年末年始の休場日は時間帯に関係なく SafeGate だけで判定する）、`not_ready`（検証済みのインストーラーが無い。署名・チェックサムの検証に失敗した場合も含む。先に `update_check_now`）、`unavailable`（アップデーターが無い）、`timeout`（アプリが 2 秒以内に着手せず、依頼を取り下げた。あとから導入されることはない） |
| `request_id` | 記録した依頼の ID（`unavailable` のときは無い） |

エラー: 書き込み許可がオフの `update_apply` は `write_disabled`（403）。確認も導入も依頼の記録（監査行）もしない。ほかは `docs/architecture/paper-results-mcp-errors.md` の共通のコード。

挙動の要点:

- 導入されるのは、**この MCP の `update_check_now` が（アプリの `Checker` を通して）ダウンロードし検証した**インストーラーだけ。署名検証・チェックサム・URL とサイズの制限に通らなかったリリースは今どおり導入されず、`error_kind=verification` などとして状態に残る（署名を飛ばす手段は無い）。
- `update_check_now` のあとに `ready` が残った場合でも、アプリ自身の自動更新の契約は変わらない（スケジューラーの周期確認は、Ready なら今までどおり導入しうる）。立会時間帯の保留（`market_hours`）は MCP の `update_apply` だけに掛かり、周期確認と Settings の「今すぐ確認」の自動導入は変えない。`settings_write_enable` が効くのは MCP 起点の `update_apply` だけ。
- 通った・保留になった `update_apply` は、`actor=mcp`・`client`（接続元ホスト）・時刻・結果を `update_control_requests` に残す（`er/update-tools/tables-update-control.md`）。書き込みオフの拒否は何も残さない。

#### 例（実際の応答。テストの偽の GitHub・偽の QuitForUpdate での出力）

`update_status`（確認前）:

リクエスト:

```json
{"id":1,"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{},"name":"update_status"}}
```

レスポンス（HTTP 200）:

```json
{"id":1,"jsonrpc":"2.0","result":{"structuredContent":{"data":{"available":false,"blocked":false,"current_version":"v0.1.0","dev_build":false,"no_release":false,"ready":false,"unavailable":false},"tool":"update_status"},"isError":false,"content":"（structuredContent の JSON を text にしたもの）"}}
```

`update_check_now`（新しい版を検証済み。終了も導入もしていない）:

リクエスト:

```json
{"id":1,"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{},"name":"update_check_now"}}
```

レスポンス（HTTP 200）:

```json
{"id":1,"jsonrpc":"2.0","result":{"structuredContent":{"data":{"available":true,"blocked":false,"check":"completed","checked_at":"2026-11-25T07:45:07Z","current_version":"v0.1.0","dev_build":false,"latest_version":"v0.2.0","no_release":false,"ready":true,"unavailable":false},"tool":"update_check_now"},"isError":false,"content":"（structuredContent の JSON を text にしたもの）"}}
```

`update_apply`（通った）:

リクエスト:

```json
{"id":1,"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{},"name":"update_apply"}}
```

レスポンス（HTTP 200）:

```json
{"id":1,"jsonrpc":"2.0","result":{"structuredContent":{"data":{"request_id":2,"result":"accepted"},"tool":"update_apply"},"isError":false,"content":"（structuredContent の JSON を text にしたもの）"}}
```

`update_apply`（SafeGate が保留。アプリは終了しない）:

リクエスト:

```json
{"id":1,"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{},"name":"update_apply"}}
```

レスポンス（HTTP 200）:

```json
{"id":1,"jsonrpc":"2.0","result":{"structuredContent":{"data":{"reason":"open_positions","reason_text":"ポジションを保有しているため","request_id":2,"result":"deferred"},"tool":"update_apply"},"isError":false,"content":"（structuredContent の JSON を text にしたもの）"}}
```

`update_apply`（取引日の立会時間帯 JST 9:00〜15:30。SafeGate が通っていても導入しない。アプリは終了しない）:

リクエスト:

```json
{"id":1,"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{},"name":"update_apply"}}
```

レスポンス（HTTP 200）:

```json
{"id":1,"jsonrpc":"2.0","result":{"structuredContent":{"data":{"reason":"market_hours","reason_text":"取引時間中（9:00〜15:30）のため","request_id":2,"result":"deferred"},"tool":"update_apply"},"isError":false,"content":"（structuredContent の JSON を text にしたもの）"}}
```

`update_status`（同じ立会時間帯。確認済みの新しい版があり、`market_hours` で保留される。上の `update_apply` のあと）:

リクエスト:

```json
{"id":1,"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{},"name":"update_status"}}
```

レスポンス（HTTP 200）:

```json
{"id":1,"jsonrpc":"2.0","result":{"structuredContent":{"data":{"available":true,"blocked":true,"blocked_kind":"market_hours","blocked_reason":"取引時間中（9:00〜15:30）のため","checked_at":"2026-11-25T09:55:30Z","current_version":"v0.1.0","dev_build":false,"last_apply":{"actor":"mcp","client":"127.0.0.1","reason":"market_hours","requested_at":"2026-11-25T01:00:00Z","result":"deferred"},"latest_version":"v0.2.0","no_release":false,"ready":true,"unavailable":false},"tool":"update_status"},"isError":false,"content":"（structuredContent の JSON を text にしたもの）"}}
```

`update_apply`（書き込み許可がオフ。何も実行・記録しない）:

リクエスト:

```json
{"id":1,"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{},"name":"update_apply"}}
```

レスポンス（HTTP 403）:

```json
{"code":"write_disabled","error":{"code":-32000,"data":{"code":"write_disabled"},"message":"設定の書き込みは許可されていません。settings_write_enable か Settings の「外部連携」で許可してください。"},"id":1,"jsonrpc":"2.0","message":"設定の書き込みは許可されていません。settings_write_enable か Settings の「外部連携」で許可してください。"}
```

`update_check_now`（署名の検証に失敗したリリース。導入されない）:

リクエスト:

```json
{"id":1,"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{},"name":"update_check_now"}}
```

レスポンス（HTTP 200）:

```json
{"id":1,"jsonrpc":"2.0","result":{"structuredContent":{"data":{"available":true,"blocked":false,"check":"failed","checked_at":"2026-11-25T07:45:07Z","current_version":"v0.1.0","dev_build":false,"error_kind":"verification","error_reason":"ダウンロードしたインストーラーの検証に失敗しました（更新は中止されました）","latest_version":"v0.2.0","no_release":false,"ready":false,"unavailable":false},"tool":"update_check_now"},"isError":false,"content":"（structuredContent の JSON を text にしたもの）"}}
```

`update_status`（アップデーターが無いビルド）:

リクエスト:

```json
{"id":1,"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{},"name":"update_status"}}
```

レスポンス（HTTP 200）:

```json
{"id":1,"jsonrpc":"2.0","result":{"structuredContent":{"data":{"available":false,"blocked":false,"current_version":"v0.1.0","dev_build":false,"no_release":false,"ready":false,"unavailable":true,"unavailable_reason":"このビルドはアップデートの確認に対応していません。"},"tool":"update_status"},"isError":false,"content":"（structuredContent の JSON を text にしたもの）"}}
```
