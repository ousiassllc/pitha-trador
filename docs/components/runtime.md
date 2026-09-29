# コンポーネント設計: Wails統合・エラーハンドリング・テスト戦略（§7〜§9）

`docs/components/overview.md` の§7〜§9を分割したファイル（節番号・内容は分割前と同一。`.linterly.yml` の300行/ファイル制限のため）。

## 7. Wails統合

- `cmd/desktop/main.go` がGin Engineを組み立て、`options.App.AssetServer.Handler` に注入してWailsを起動する。フロントエンドは通常のWailsテンプレート（`frontend/`ディレクトリ・独自バインディング）を使わず、`static/src`のビルド成果物をGinの`Static()`で配信する
- Kill Switch発動等、サーバー内部イベントをネイティブ通知として表示する処理（`runtime.EventsEmit`, OSトースト, トレイアイコン変更）は `internal/web/handler` ではなく `internal/service/risk` からWailsランタイムを直接呼び出す薄いアダプタ（`internal/service/notify`）を介して行う
- 開発ワークフロー（3種のウォッチプロセスを並行起動、`Makefile dev`ターゲット）:

```makefile
.PHONY: dev
dev:
	@bunx concurrently \
		"wails dev" \
		"templ generate --watch" \
		"bun --cwd static run dev"
```

  - `.templ`編集 → `templ generate --watch`が`_templ.go`を再生成 → `wails dev`がGoファイル変更を検知しプロセス再起動（WebViewは自動リロード）
  - `.ts`編集 → esbuildがバンドル → `static/src/dist`更新 → WebViewはHTTPキャッシュなし設定のため次回リクエストで反映（手動リロードまたは`hx-boost`遷移で反映）
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
| Lit コンポーネント | `@open-wc/testing` + `@web/test-runner` | チャート初期化・WS再接続・Kill Switch操作等の単体挙動 |
| E2E | Playwright（Wailsアプリのwebview、またはビルド前は`wails dev`のブラウザアクセスモード） | Scanner→Symbol Detail遷移、Kill Switch操作フロー全体 |
