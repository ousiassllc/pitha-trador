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
| DB を `mode=ro` + `PRAGMA query_only=ON` で開く（`sqlitedb.Open` は呼ばない） | `internal/service/paperresultsmcp/readdb` |
| SQL とホワイトリストの戻り値（列を名指しで SELECT。`SELECT *` なし） | `internal/service/paperresultsmcp/query` |
| テスト（外から見た HTTP の振る舞い）と、その固定データ・SQL 数えるドライバ | `internal/service/paperresultsmcp/mcptests`、`internal/service/paperresultsmcp/testkit` |
| 東証立会日 | 既存の `internal/service/marketcalendar` |
| 資金の分母 | `cmd/paper-results-mcp` の `paperInitialCapital`（アプリと同じ risk yaml 解決順。Paper の `initial_capital` のみ）。DB は `bootstrap.DefaultDBPath` と `PITHA_DB_PATH` |

`internal/web` からは import しない（depguard の `web-no-repository` を迂回する読み取り画面を増やさない）。

## 3. テスト

受け入れ条件は `docs/api/endpoints/paper-results-mcp.md` §5。単体テストは一時ディレクトリの SQLite（マイグレーション適用済みのテスト DB）と、httptest の HTTP で行う。認証失敗が SQL 0 件・接続 0 本であることは、`database/sql` のドライバを包んで文を数えるテストで確認する。Tailscale も Cloudflare も、トンネルのアカウントは CI に置かない。

## 4. ビルドと起動

ビルド: `go build -o paper-results-mcp ./cmd/paper-results-mcp`（Windows では `paper-results-mcp.exe`）。アプリ本体の実行ファイルには含まれない。

既定はオフで、何も待ち受けない。オンにするには、PC のローカル設定ファイル `%AppData%\pitha-trador\paper-results-mcp.json`（`os.UserConfigDir()` の `pitha-trador` 配下。`-config <パス>` で差し替え可）を作る。DB とは別のファイルで、権限はユーザーのみ（Unix 系では `0600` でないと読み込みを拒否する）。

```json
{
  "enabled": true,
  "listen": "127.0.0.1:48110",
  "api_key": "<長いランダム文字列。リポジトリ・ログに書かない>"
}
```

- `listen` を省略すると `127.0.0.1:48110`。許すのは loopback と、この PC の tailnet アドレス（`100.64.0.0/10`、`fd7a:115c:a1e0::/48`）で、そのアドレスが実際にインターフェースにあるときだけ。`0.0.0.0`・`::`・LAN・ホスト名は起動を拒否する
- `api_key` が空なら `paper-results-mcp: refusing to listen: no auth configured` をログに出して待受しない
- 設定ファイルが無い・読めない・JSON として不正なときも待受しない（オフと同じ）
- Cloudflare Access のオリジン照合（任意）は `verify_access_headers` / `access_client_id` / `access_client_secret`。既定はオフ（`architecture/paper-results-mcp.md` §3.5）

起動: `paper-results-mcp`（アプリとは別のプロセス。Windows のスタートアップやタスクスケジューラで同じユーザーのセッションに登録する）。DB は `PITHA_DB_PATH`、無ければ `%AppData%\pitha-trador\pitha.db` を読み取り専用で開く。資金の分母は `PITHA_RISK_PATH` / `config/risk.yaml` の `paper.initial_capital`。止めるときは Ctrl+C（SIGINT/SIGTERM）で、処理中の呼び出しを終えてから閉じる。

Grok Bot には、選んだ経路の URL（Tailscale のホスト名、またはトンネルの HTTPS 名）と `Authorization: Bearer <api_key>` を登録する。実機での経路確認（tailnet の外から届かないことを含む）は自動テストの対象外で、キーを伏せた記録を残す（API 仕様 §5）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-17 | 新規作成 | issue #783。HALT の画面仕様とは別プロセスであると明記 |
| 1.1 | 2026-10-17 | 経路の実装をプロセスに埋め込まないことと、CI に Tailscale・Cloudflare のアカウントを置かないことを明記 | 悠太さん指示。Cloudflare を必須にしない |
| 1.2 | 2026-10-18 | 実装済みに改め、§1 に Streamable HTTP の自前ハンドラの選択、§2 に実際のパッケージ構成、§4 にビルド・設定ファイル・起動手順を追加 | issue #783 の実装 |
