# コンポーネント設計: Wails統合・エラーハンドリング・テスト戦略（§7〜§9）

`docs/components/overview.md` の§7〜§9を分割したファイル（節番号・内容は分割前と同一。`.linterly.yml` の300行/ファイル制限のため）。

## 7. Wails統合

- `cmd/desktop/main.go` がGin Engineを組み立て、`options.App.AssetServer.Handler` に注入してWailsを起動する。フロントエンドは通常のWailsテンプレート（`frontend/`ディレクトリ・独自バインディング）を使わず、`static/src`のビルド成果物を `internal/router` の `engine.StaticFS("/static", staticFS())` で配信する。`staticFS()`（`internal/router/static.go`）は通常`go:embed`された`static/src`（`staticassets.FS`）を返し、環境変数`PITHA_STATIC_DIR`が既存ディレクトリを指す場合のみディスク上のそのディレクトリを配信する（`make dev`が使用）
- WebSocket（`/ws/...`）はAssetServer経由では扱えない（Wails AssetServerはWebSocketを扱えず、WebView2は`ws://`をAssetServerへ回さずネットワークへ直接送る）。そのためWindows版の`cmd/desktop`は、`/ws/...`のUpgradeだけを受けるループバック専用リスナー（`cmd/desktop/ws_listener.go`。`router.WebSocketOnly`でGin Engineを包み、ランダムポートを`127.0.0.1`と`[::1]`の両方で待ち受ける）を別に起動し、`router.WithWebSocketBase`で渡したそのアドレス（`ws://wails.localhost:<port>`）を全画面の`<meta name="ws-base">`に出力する。Litの`lib/ws.ts`の`resolveWsUrl`がこのmetaで相対パス（`/ws/...`）を絶対URL化する。Windows以外（`wails://wails/`）ではリスナーも`ws-base`も無く、ページは自身のoriginへ接続する。HTML/HTMX/静的アセットは従来どおりAssetServerのみを経由する（`api/endpoints.md` §6）
- Kill Switch発動等、サーバー内部イベントをネイティブ通知として表示する処理（`runtime.SendNotification`によるOSトースト、`runtime.EventsEmit`）は `internal/web/handler` ではなく、`cmd/desktop/notify.go` の `App` が `risk.Notifier` を実装して行う（`internal/service/risk` はインターフェース越しに呼び出し、`bootstrap.BuildServices` がSlack・構造化ログと並べて束ねる）。`internal/service/notify` はSlack Webhook・構造化ログ・メンテナンス通知のみでWailsに依存しない
- OSのシステムトレイ（トレイアイコン変更）は未対応: Wails v2の`runtime`パッケージにトレイAPIが無く、Wails v3または外部systrayライブラリが必要となるため。現状はネイティブトーストと`EventsEmit`（`kill-switch:triggered`等）のみを提供する。`EventsEmit`のフロントエンド購読者は未実装で、Kill Switchパネルは`/ws/system`と`GET /api/v1/system/status`の再同期で状態を更新する
- 開発ワークフロー（3種のウォッチプロセスを並行起動、`Makefile dev`ターゲット）:

```makefile
dev: ## 開発起動（wails dev + templ watch + bun watch）
	@PITHA_STRATEGY_PATH=$(CURDIR)/config/strategy.yaml \
	PITHA_RISK_PATH=$(CURDIR)/config/risk.yaml \
	PITHA_UNIVERSE_PATH=$(CURDIR)/config/universe.sample.csv \
	PITHA_STATIC_DIR=$(CURDIR)/static/src \
	SWAGGER_ENABLED=true \
	bunx concurrently \
		"cd cmd/desktop && wails dev" \
		"templ generate --watch" \
		"bun --cwd=static run dev"
```

  - 環境変数（設定ファイルパス・銘柄マスタCSV・静的ファイルディレクトリ・Swagger有効化）の設定理由は `docs/environment/setup.md` の「Makefileターゲット」節を参照
  - `.templ`編集 → `templ generate --watch`が`_templ.go`を再生成 → `wails dev`がGoファイル変更を検知しプロセス再起動（WebViewは自動リロード）
  - `.ts`編集 → esbuildがバンドル → `static/src/dist`更新 → `make dev`では`PITHA_STATIC_DIR`によりディスクから直接配信されるため、Goの再ビルド無しに手動リロードで再取得して反映される（キャッシュ制御ヘッダーの付与や自動遷移による再取得は行わない）
  - esbuildのエントリは`static/src/components/*/pitha-*.ts`をglobで自動列挙し（`lib/*.ts`は各コンポーネントからimportされるためエントリにしない）、本番ビルドはsourcemapを出さず（`go:embed`されて`/static`で配信されるため。`--watch`のみ出力、issue #146）、`splitting: true`（ESM）でLit等の共有コードを`dist/js/chunks/`へ切り出す。全ページ共通の`pitha-kill-switch-panel`と各ページのコンポーネントでLitが二重にロードされることはない
  - `.css`編集 → Tailwindが`static/src/dist/css/app.css`を再生成 → `make dev`では`PITHA_STATIC_DIR`によりディスクから配信されるため、`.ts`と同様に手動リロードで再取得して反映される（ライブリロード・HMRの仕組みは無い）

- ビルド・配布: 配布物はNSISインストーラー（`build/bin/*-installer.exe`。単一の`.exe`ではない）。CIの`build`ジョブが`wails build -platform windows/amd64 -nsis -installscope user -ldflags "-X …version.Version=… -X …version.ReleasePublicKey=…"`で生成し（`makensis`のインストールが必要。ユーザースコープ＝UAC不要で自動更新の前提）、`checksums.txt`（署名有効時は`checksums.txt.sig`）とともにGitHub Releaseへ公開する（詳細は`environment/ci.md`、`environment/setup.md` §CI/CD）。ローカルの`make build`は`-nsis`なしの`wails build -platform windows/amd64`で実行ファイルのみを生成する。`cmd/desktop/wails.json`の`frontend:build`は空のため、`wails build`の前に`make generate`相当（`templ generate`と`bun run --cwd static build`）が必須で、`static/src/embed.go`の`//go:embed dist img vendor`が`dist/`未生成だとコンパイルできない（`make build`・CIは`generate`/共通セットアップで実施）。DBはSQLite（アプリ内蔵、`modernc.org/sqlite`）のため、Postgres等の外部DBサービスを事前にインストール・起動しておく必要はない。初回起動時に`db/migrations`を自動適用しDBファイルを生成する。Wails v2のWindowsターゲットとDBドライバ（`modernc.org/sqlite`, `modernc.org/sqlite/vec`）はいずれもpure Go実装のためCGO不要であり、`wails build -platform windows/amd64`はLinux CIランナー上でクロスビルドできる（`-nsis`にはLinux上に`makensis`が要る）

## 8. エラーハンドリング（要約）

- HTMX: サーバーがエラーUIもHTMLで返す（4xx/5xxは`atoms.Toast`フラグメントを`#toast-region`へswap。フラグメントの無い失敗・応答なしはグローバル`htmx:responseError`/`htmx:sendError`リスナーが汎用トースト。§4「エラー表示」参照）
- Lit: JSON APIを使うため自前でtry/catchしコンポーネント内にエラー状態をレンダリングする
- Huma API: バリデーションエラーはRFC 7807 Problem Details形式で自動生成される

## 9. テスト戦略

| レイヤー | テスト手法 | 検証内容 |
|---------|----------|---------|
| Go ハンドラ | `httptest` + HTMLアサーション | 正しいHTMLフラグメント/フルページ・ステータスコード |
| Templ テンプレート | `Render()` → HTML文字列アサーション | atoms/molecules/organisms/pagesの出力 |
| Lit コンポーネント | `bun test`（`bun:test`）+ happy-dom（`@happy-dom/global-registrator`、`static/bunfig.toml`が`test/setup.ts`をpreload） | チャート初期化・WS再接続・Kill Switch操作等の単体挙動 |
| E2E | **未整備**（`tests/`は雛形でREADMEのみ。CIにもE2Eジョブは無い。導入時はPlaywrightを想定: Wailsアプリのwebview、またはビルド前は`wails dev`のブラウザアクセスモード） | （導入時）Scanner→Symbol Detail遷移、Kill Switch操作フロー全体 |
