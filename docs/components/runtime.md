# コンポーネント設計: Wails統合・エラーハンドリング・テスト戦略（§7〜§9）

`docs/components/overview.md` の§7〜§9を分割したファイル（節番号・内容は分割前と同一。`.linterly.yml` の300行/ファイル制限のため）。

## 7. Wails統合

- `cmd/desktop/main.go` がGin Engineを組み立て、`options.App.AssetServer.Handler` に注入してWailsを起動する。フロントエンドは通常のWailsテンプレート（`frontend/`ディレクトリ・独自バインディング）を使わず、`static/src`のビルド成果物を `internal/router` の `engine.StaticFS("/static", staticFS())` で配信する。`staticFS()`（`internal/router/static.go`）は通常`go:embed`された`static/src`（`staticassets.FS`）を返し、環境変数`PITHA_STATIC_DIR`が既存ディレクトリを指す場合のみディスク上のそのディレクトリを配信する（`make dev`が使用）
- Kill Switch発動等、サーバー内部イベントをネイティブ通知として表示する処理（`runtime.SendNotification`によるOSトースト、`runtime.EventsEmit`）は `internal/web/handler` ではなく、`cmd/desktop/notify.go` の `App` が `risk.Notifier` を実装して行う（`internal/service/risk` はインターフェース越しに呼び出し、`bootstrap.BuildServices` がSlack・構造化ログと並べて束ねる）。`internal/service/notify` はSlack Webhook・構造化ログ・メンテナンス通知のみでWailsに依存しない
- OSのシステムトレイ（トレイアイコン変更）は未対応: Wails v2の`runtime`パッケージにトレイAPIが無く、Wails v3または外部systrayライブラリが必要となるため。現状はネイティブトーストと`EventsEmit`（`kill-switch:triggered`等）のみを提供する。`EventsEmit`のフロントエンド購読者は未実装で、Kill Switchパネルは`/ws/system`と`GET /api/v1/system/status`の再同期で状態を更新する
- 開発ワークフロー（3種のウォッチプロセスを並行起動、`Makefile dev`ターゲット）:

```makefile
dev:
	@PITHA_STRATEGY_PATH=$(CURDIR)/config/strategy.yaml \
	PITHA_RISK_PATH=$(CURDIR)/config/risk.yaml \
	PITHA_STATIC_DIR=$(CURDIR)/static/src \
	SWAGGER_ENABLED=true \
	bunx concurrently \
		"cd cmd/desktop && wails dev" \
		"templ generate --watch" \
		"bun --cwd=static run dev"
```

  - 環境変数（設定ファイルパス・静的ファイルディレクトリ・Swagger有効化）の設定理由は `docs/environment/setup.md` の「Makefileターゲット」節を参照
  - `.templ`編集 → `templ generate --watch`が`_templ.go`を再生成 → `wails dev`がGoファイル変更を検知しプロセス再起動（WebViewは自動リロード）
  - `.ts`編集 → esbuildがバンドル → `static/src/dist`更新 → `make dev`では`PITHA_STATIC_DIR`によりディスクから直接配信されるため、Goの再ビルド無しに手動リロードで再取得して反映される（キャッシュ制御ヘッダーの付与や自動遷移による再取得は行わない）
  - esbuildのエントリは`static/src/components/*/pitha-*.ts`をglobで自動列挙し（`lib/*.ts`は各コンポーネントからimportされるためエントリにしない）、本番ビルドはsourcemapを出さず（`go:embed`されて`/static`で配信されるため。`--watch`のみ出力、issue #146）、`splitting: true`（ESM）でLit等の共有コードを`dist/js/chunks/`へ切り出す。全ページ共通の`pitha-kill-switch-panel`と各ページのコンポーネントでLitが二重にロードされることはない
  - `.css`編集 → TailwindがビルドしてSPAリロード不要で反映

- ビルド・配布: `wails build` で単一のWindows実行ファイル（`.exe`）を生成する。DBはSQLite（アプリ内蔵、`modernc.org/sqlite`）のため、Postgres等の外部DBサービスを事前にインストール・起動しておく必要はない。初回起動時に`db/migrations`を自動適用しDBファイルを生成する。Wails v2のWindowsターゲットとDBドライバ（`modernc.org/sqlite`, `modernc.org/sqlite/vec`）はいずれもpure Go実装のためCGO不要であり、`wails build -platform windows/amd64`はLinux CIランナー上でもそのままクロスビルドできる（`environment/setup.md` §CI/CD参照）

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
