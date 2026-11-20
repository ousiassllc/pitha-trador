# API 仕様: Paper 実績 MCP の AI の役割の設定ツール

`docs/api/endpoints/paper-results-mcp.md` §3.35 の分割章（行数上限のため別ファイル）。issue #1080。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。

## 3. ツール

### 3.35 `ai_roles`

AI の役割（Jev の売買判断・Sol の改善提案・Opus の提案の検証・Luna のニュース分類）ごとの、プロバイダーとモデルの設定を読む。**読み取り専用**。役割の設定を変えるツールは無い（`settings_set`の許可リストに`ai.role.*`は無い）: ライブの Jev（売買判断）のプロバイダー・モデルは Settings 画面だけで変えられる。

引数: なし（`from`/`to`も取らない）。

応答の `data`:

| キー | 内容 |
|---|---|
| `providers` | 選べるプロバイダー id（`custom`＝独自 API・`openrouter`） |
| `roles[]` | 役割ごと。`role`（`jev`/`sol`/`opus`/`luna`）・`name`・`live`（取引に使われる役割。jev だけ true）・`configured`（`provider`・`model`。効く設定。Sol・Opus・Luna は保存後の次の呼び出しから、`jev`は再起動後）・`source`（`stored`＝Settings で保存した値、`default`＝既定。既定は今までと同じ接続先・キー・モデル）・`running`（最後の起動で実際に使い始めた`provider`・`model`（`jev`ではこれが実際に動いている組）。記録が無ければ null。モデルが空は「指定なし」）・`changeable_via_mcp`（常に false） |
| `history[]` | 新しい順に最大 20 件。`at`（UTC）・`kind`（`saved`＝Settings での保存、`started`＝起動時の記録）・`role`・`before`（`saved`のとき保存前の組、`started`は null）・`after` |

秘密は返さない: `runtime_settings`と`ai_role_history`だけを読み、`secrets`は読まない（API キー・接続先 URL・キーの有無は出ない）。モデル名は秘密ではない。ライブの Jev の`saved`の行は、`experiment_compare`の交絡（`kind=model`、`name=AI_ROLE_JEV`）にもなる。

| エラー | 条件 |
|---|---|
| `invalid_argument`（422） | 未知の引数を渡した |
