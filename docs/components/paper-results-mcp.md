# コンポーネント: Paper 実績 MCP

`docs/architecture/paper-results-mcp.md` のプロセス境界。画面（HALT）には出さない。実装は issue #783 で追加した（`cmd/paper-results-mcp` と `internal/service/paperresultsmcp`）。

## 1. プロセス

- エントリーポイントは `cmd/paper-results-mcp`。Wails の `cmd/desktop` にも `cmd/server` の Gin にもルートを足さない。根拠: デスクトップのページはセッション Cookie と loopback の AssetServer が前提で、リモートの Grok Bot とは認証が別である（`docs/api/endpoints.md` §1）
- アプリ本体と同時に動いてよい。DB はアプリが単一ライター、本プロセスは `mode=ro` の読み手（`docs/architecture/er.md` の同時実行は WAL。本プロセスは WAL のモード変更もチェックポイントもしない）
- #347 の kabu ステータス MCP とは別バイナリ・別設定ファイル・別ポート（既定 `127.0.0.1:48110`。Tailscale で直接待つときは tailnet の IP）。あちらが main に入ったあともパッケージを共有しない。経路の実装（Tailscale も Cloudflare も含む）はこのプロセスに埋め込まない
- トランスポートは Streamable HTTP（POST のみ、JSON 応答）。MCP SDK は使わず、JSON-RPC 2.0 の小さなハンドラを `internal/service/paperresultsmcp` に置く。根拠: 認証を最初に行い SQL の前で止めること、8 つのツールだけを表で持つこと、ログに行を出さないことを、SDK の層を挟まずに保てる。選択の理由は `docs/api/endpoints/paper-results-mcp.md` 冒頭

## 2. パッケージ

| 役割 | 置き場所 |
|------|----------|
| 実行ファイル（設定の読み込み・シグナルでの停止） | `cmd/paper-results-mcp` |
| 待受・Bearer 認証・JSON-RPC・ツール表・エラー表・同時実行/時間の上限 | `internal/service/paperresultsmcp` |
| ローカル設定ファイルの読み込みと待受アドレスの検証（`0.0.0.0`・`::`・LAN の拒否） | `internal/service/paperresultsmcp/settings` |
| 設定ファイルの書き込み（原子的・`0600`）と API キーの生成（`pitha-mcp-` + 32 バイトの乱数）。Settings 画面から使う | `internal/service/paperresultsmcp/settings`（`Save`、`GenerateAPIKey`） |
| Settings 画面側の設定サービス（状態表示・有効/待受アドレスの保存・API キー再生成。MCP プロセスは起動も停止もしない） | `internal/service/paperresultsmcp/admin` |
| Settings 画面の MCP パネルのハンドラと画面部品（`GET /settings/mcp-panel`、`POST /settings/mcp`、`POST /settings/mcp/key`） | `internal/web/handler/mcpsettings`、`internal/web/organisms/mcp`、`static/src/components/copy-button` |
| DB を `mode=ro` + `PRAGMA query_only=ON` で開く（`sqlitedb.Open` は呼ばない） | `internal/service/paperresultsmcp/readdb` |
| SQL とホワイトリストの戻り値（列を名指しで SELECT。`SELECT *` なし） | `internal/service/paperresultsmcp/query` |
| テスト（外から見た HTTP の振る舞い）と、その固定データ・SQL 数えるドライバ | `internal/service/paperresultsmcp/mcptests`、`internal/service/paperresultsmcp/testkit` |
| 東証立会日 | 既存の `internal/service/marketcalendar` |
| 資金の分母 | `cmd/paper-results-mcp` の `paperInitialCapital`（アプリと同じ risk yaml 解決順。Paper の `initial_capital` のみ）。DB は `bootstrap.DefaultDBPath` と `PITHA_DB_PATH` |

`internal/web` からは import しない（depguard の `web-no-repository` を迂回する読み取り画面を増やさない）。

## 3. テスト

受け入れ条件は `docs/api/endpoints/paper-results-mcp.md` §5。単体テストは一時ディレクトリの SQLite（マイグレーション適用済みのテスト DB）と、httptest の HTTP で行う。認証失敗が SQL 0 件・接続 0 本であることは、`database/sql` のドライバを包んで文を数えるテストで確認する。Tailscale も Cloudflare も、トンネルのアカウントは CI に置かない。

## 4. ビルド・設定・起動

ビルド: `go build -o paper-results-mcp ./cmd/paper-results-mcp`（Windows では `paper-results-mcp.exe`）。アプリ本体の実行ファイルには含まれない。Windows のインストーラはアプリ本体の実行ファイルと同じフォルダに `paper-results-mcp.exe` を置く。Settings 画面の MCP パネルは、アプリ本体の実行ファイルと同じフォルダの `paper-results-mcp(.exe)` を探し、「見つかる / 見つからない」を表示する。

### 4.1 設定（Settings 画面）

既定はオフで、何も待ち受けない。オン/オフ・待受アドレス・API キーは Settings 画面の「外部連携」→「MCP（Paper 実績・Jev 判断の読み取り専用エンドポイント）」（`/settings#modal-mcp`）で設定する。手で JSON を作る必要はない（issue #792）。画面が行うのは設定ファイルの書き込みだけで、MCP プロセスの起動・停止・再起動は行わない（読み取り専用の別プロセスとして分離するため）。

- 画面の項目: 状態（待受中 / 停止中 / キー未設定）、有効化のチェック、待受アドレス（`host:port`）、API キーの生成・再生成、Grok Bot に登録する値（URL と、伏せ字の `Authorization: Bearer ●●●●●●●●`）、`paper-results-mcp` の配置先（見つかる / 見つからない）、設定ファイルの場所
- 待受アドレスの検証はサーバー側で行い、プロセスと同じ規則である。許すのは loopback と、この PC の tailnet アドレス（`100.64.0.0/10`、`fd7a:115c:a1e0::/48`）で、そのアドレスが実際にインターフェースにあるときだけ。`0.0.0.0`・`::`・ホスト部が空・LAN・ホスト名・ポート 0 は 422 で拒否し、ファイルは変えない。空欄は `127.0.0.1:48110`
- API キーは `pitha-mcp-` + 32 バイトの乱数（URL 安全な base64）。生成・再生成の直後の応答に一度だけ表示し（読み取り専用の欄、`Authorization: Bearer <キー>` の全文、コピーボタン）、応答は `Cache-Control: no-store`、表示部は htmx の履歴キャッシュに入れない（`hx-history="false"`）。パネルを開き直すと常に伏せ字になる。紛失したら再生成する。再生成すると古いキーは、MCP を再起動した時点で使えなくなる
- 状態は設定ファイルと、有効かつキーがあるときだけの TCP 接続（300 ms）から決める。ファイルが無ければオフ（停止中・接続しない）。有効でもキーが空なら「キー未設定」（接続しない）。接続できれば「待受中」、できなければ「停止中」。設定ファイルが読めない・権限が緩い・不正なときは「停止中」と警告を出す。そのまま保存または生成すると、既定値から `0600` で作り直す（既存のキーは失われる）

書き込む設定ファイルは `%AppData%\pitha-trador\paper-results-mcp.json`（`os.UserConfigDir()` の `pitha-trador` 配下）。DB とは別のファイルで、権限はユーザーのみ（`0600`。一時ファイルに書いて fsync し、rename で置き換える。ディレクトリは `0700`）。Unix 系では `0600` でないとプロセスが読み込みを拒否する。プロセスの `-config <パス>` で別のファイルを読ませた場合、画面は既定のパスしか編集しない。API キーはこのファイルにだけあり、DB（`secrets` を含む）・ログ・データエクスポート・DB バックアップには入らない。保存時のログに出すのは `enabled` / `listen` / `key_configured` だけである。

```json
{
  "enabled": true,
  "listen": "127.0.0.1:48110",
  "api_key": "pitha-mcp-…（画面が生成。リポジトリ・ログに書かない）"
}
```

- `listen` を省略すると `127.0.0.1:48110`
- `api_key` が空なら `paper-results-mcp: refusing to listen: no auth configured` をログに出して待受しない
- 設定ファイルが無い・読めない・JSON として不正なときも待受しない（オフと同じ）
- Cloudflare Access のオリジン照合（任意）は `verify_access_headers` / `access_client_id` / `access_client_secret`。画面では編集しないが、画面からの保存は既存の値を保つ。既定はオフ（`architecture/paper-results-mcp.md` §3.5）

### 4.2 起動

`paper-results-mcp`（アプリとは別のプロセス）を、運用者が起動する。自動では起動しない。Windows のスタートアップやタスクスケジューラで同じユーザーのセッションに登録する。設定ファイルはプロセスの起動時に一度だけ読むため、画面で設定を変えたあとは `paper-results-mcp` を再起動する必要がある（再起動するまで、状態は「停止中」のままか、古い設定で動き続ける）。DB は `PITHA_DB_PATH`、無ければ `%AppData%\pitha-trador\pitha.db` を読み取り専用で開く。資金の分母は `PITHA_RISK_PATH` / `config/risk.yaml` の `paper.initial_capital`。止めるときは Ctrl+C（SIGINT/SIGTERM）で、処理中の呼び出しを終えてから閉じる。

Grok Bot には、選んだ経路の URL（Tailscale のホスト名、またはトンネルの HTTPS 名。画面の URL は待受アドレスそのもの）と `Authorization: Bearer <api_key>` を登録する。実機での経路確認（tailnet の外から届かないことを含む）は自動テストの対象外で、キーを伏せた記録を残す（API 仕様 §5）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-17 | 新規作成 | issue #783。HALT の画面仕様とは別プロセスであると明記 |
| 1.1 | 2026-10-17 | 経路の実装をプロセスに埋め込まないことと、CI に Tailscale・Cloudflare のアカウントを置かないことを明記 | 悠太さん指示。Cloudflare を必須にしない |
| 1.2 | 2026-10-18 | 実装済みに改め、§1 に Streamable HTTP の自前ハンドラの選択、§2 に実際のパッケージ構成、§4 にビルド・設定ファイル・起動手順を追加 | issue #783 の実装 |
| 1.3 | 2026-10-19 | §2 に設定ファイルの書き込み・設定サービス・Settings 画面のハンドラを追加。§4 を、Settings 画面で有効/待受アドレス/API キーを設定する手順に書き換え（手作りの JSON は不要。キーの一度きりの表示と伏せ字、再起動が必要なこと、プロセスは画面から起動・停止しないことを明記） | issue #792 |
