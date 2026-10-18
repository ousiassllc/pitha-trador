# コンポーネント: Paper 実績 MCP

`docs/architecture/paper-results-mcp.md` のプロセス境界。画面（HALT）には出さない。本ファイルは実装時の置き場所を決めるだけで、この仕様変更ではコードを追加しない。

## 1. プロセス

- エントリーポイントは将来 `cmd/paper-results-mcp`。Wails の `cmd/desktop` にも `cmd/server` の Gin にもルートを足さない。根拠: デスクトップのページはセッション Cookie と loopback の AssetServer が前提で、リモートの Grok Bot とは認証が別である（`docs/api/endpoints.md` §1）
- アプリ本体と同時に動いてよい。DB はアプリが単一ライター、本プロセスは `mode=ro` の読み手（`docs/architecture/er.md` の同時実行は WAL。本プロセスは WAL のモード変更もチェックポイントもしない）
- #347 の kabu ステータス MCP とは別バイナリ・別設定ファイル・別ポート（既定 `127.0.0.1:48110`。Tailscale で直接待つときは tailnet の IP）。あちらが main に入ったあともパッケージを共有しない。経路の実装（Tailscale も Cloudflare も含む）はこのプロセスに埋め込まない

## 2. パッケージ

実装時の置き場所は次を予定する。本仕様の時点ではディレクトリを作らない。

| 役割 | 置き場所 |
|------|----------|
| 待受・認証・ツールの入出力 | `cmd/paper-results-mcp` と、読み取り専用の `internal/service/paperresultsmcp` |
| SQL | 同サービスの中で、API 仕様の列だけを SELECT する。`internal/repository/sqlitedb.Open` は呼ばない |
| 東証立会日 | 既存の `internal/service/marketcalendar` を呼ぶ |
| 資金の分母 | 既存の `internal/config` の risk yaml 解決（Paper の `initial_capital` のみ） |

`internal/web` からは import しない（depguard の `web-no-repository` を迂回する読み取り画面を増やさない）。

## 3. テスト

受け入れ条件は `docs/api/endpoints/paper-results-mcp.md` §5。単体テストは一時ディレクトリの SQLite（マイグレーション適用済みのテスト DB）と、httptest 相当の HTTP で行う。Tailscale も Cloudflare も、トンネルのアカウントは CI に置かない。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-17 | 新規作成 | issue #783。HALT の画面仕様とは別プロセスであると明記 |
| 1.1 | 2026-10-17 | 経路の実装をプロセスに埋め込まないことと、CI に Tailscale・Cloudflare のアカウントを置かないことを明記 | 悠太さん指示。Cloudflare を必須にしない |
