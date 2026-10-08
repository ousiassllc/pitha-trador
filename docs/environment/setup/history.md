# 環境構築: 改訂履歴

`docs/environment/setup.md`から分割した改訂履歴（linterlyの1ファイル300行上限のため）。`setup.md`の変更はここに追記する。

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.1 | 2026-09-26 | CI構成を修正: `wails build -platform windows/amd64`は`ubuntu-latest`上でクロスビルド可能（Wails Windowsターゲット・`modernc.org/sqlite`系ドライバがいずれもpure GoでCGO不要なため）と判明したため、`build`ジョブに実ビルドを含め、Windowsランナーは実機E2Eテストのみに限定 | ユーザー指摘によるファクトチェック・設計修正 |
| 1.2 | 2026-09-28 | `JEV_API_KEY`/`JEV_BASE_URL`/`KABU_API_PASSWORD`/`SLACK_WEBHOOK_URL`の入力経路をSettings画面（`/settings`）へ変更（issue #57）。`.env`からの入力を廃止したのに合わせ、初回セットアップ手順に案内を追記 | issue #57実装 |
| 1.3 | 2026-09-28 | CI `lint`/`test`/`build`各ジョブにフロントエンドビルドステップ（`bun install --cwd static --frozen-lockfile` + `bun run --cwd static build`）を最初のGoコンパイル系ステップより前に追加。`config`/静的アセットの`go:embed`化（issue #59）により未ビルド状態では`go build`自体が失敗するようになったための対応。`make dev`の`PITHA_STRATEGY_PATH`/`PITHA_RISK_PATH`設定についても追記 | issue #59実装（配布可能な.exeへの対応） |
| 1.4 | 2026-09-28 | 配布形式をポータブルexeからNSISインストーラーへ変更（issue #64）。`build`ジョブに`sudo apt-get install nsis`を追加し、`wails build -platform windows/amd64 -nsis -installscope user`でインストーラーをビルド（`-installscope user`はUAC不要の完全自動更新の前提）。SHA256チェックサムを生成し、`release`ジョブでインストーラー・チェックサムをGitHub Releaseに添付するよう変更 | issue #64実装（自動更新の前提となるインストーラー配布への移行） |
| 1.5 | 2026-09-28 | `main`へのpush（＝PRマージ）ごとに自動でバージョンを1つ繰り上げてGitHub Releaseを公開するよう`release`ジョブを拡張。従来の手動`git tag vX.Y.Z && git push`によるリリースも引き続き可能（両方とも同じ`release`ジョブを通る） | ユーザー要望（mainマージのたびに自動リリース） |
| 1.6 | 2026-09-29 | `LUNA_API_KEY`/`LUNA_BASE_URL`/`SOL_API_KEY`/`SOL_BASE_URL`/`OPUS_API_KEY`/`OPUS_BASE_URL`/`NEWS_FEED_URL`/`NEWS_FEED_API_KEY`をSettings画面経由の入力対象に追加 | 現状Jevのみが実AI呼び出しであった状態の是正（AI機能実装フェーズ） |
| 1.7 | 2026-09-29 | 必須3キー（`JEV_API_KEY`/`JEV_BASE_URL`/`KABU_API_PASSWORD`）は未設定だと全画面が`/setup`へリダイレクトされる旨を追記 | issue #80実装 |
| 1.8 | 2026-09-29 | CI/CD節のジョブ構成・トリガーに`release`ジョブとタグ`v*`トリガーを追記（issue #115）。bunを`.bun-version`で固定、`lint`ジョブに`linterly check`を追加、`concurrency`で版番号採番の競合を防止（issue #132） | code-review・doc-driftレビュー指摘 |
| 1.9 | 2026-09-29 | グリーンフィールド記述を削除し実装済みの現状に更新、Goを1.25+（`go.mod`準拠）に修正、環境変数一覧（`PITHA_SERVER_ADDR`/`PITHA_DB_PATH`/`PITHA_STATIC_DIR`/`PITHA_POLICY_*`等）とMakefileターゲット節（`make`既定は`help`）を追加、ビルド成果物パスを`static/src/dist`に統一、`e2e.yml`は未作成と明記 | issue #113/#114/#116/#118/#131 doc-drift・code-reviewレビュー指摘 |
| 1.10 | 2026-09-29 | Swagger UIをunpkg CDN読み込みから、bunで導入した`@stoplight/elements`のvendor同梱・同一オリジン配信へ変更し、`SWAGGER_ENABLED`を`true`のみ有効のオプトインに変更（`make dev`が設定）。`.env.example`の説明を`/swagger`のみの切替に是正（issue #112, #117） | セキュリティ指摘（未固定・SRIなしCDNスクリプト）・doc-drift指摘 |
| 1.11–1.12 | 2026-09-29 | 環境変数表に`PITHA_BACKUP_DIR`（SQLite日次バックアップの退避先）・`PITHA_SERVER_ALLOWED_HOSTS`（Host検証の追加許可ホスト）を追加 | issue #97（DB日次バックアップ未実装の解消）, #136 |
| 1.13 | 2026-09-29 | `PITHA_BACKUP_DIR`の説明を更新（`secrets`除外・パーミッション・週次52週保持・退避先必須・catch-up実行） | issue #137/#152/#159 |
| 1.14 | 2026-09-29 | Lint/Format/Linterly/Git Hooks節を実ファイル（`.golangci.yml`の有効linterとdepguard、`lefthook.yml`、`.linterlyignore`）に合わせて是正。`make lint`とCI `lint`ジョブの差分を明記。`.env.example`に`PITHA_SERVER_ALLOW_NON_LOOPBACK`/`PITHA_SERVER_ALLOWED_HOSTS`/`PITHA_STATIC_DIR`/`PITHA_POLICY_*`の雛形を追加 | issue #154 |
| 1.15–1.19 | 2026-09-30 | `depguard`がサブパッケージも拒否対象であることを明記。`.linterlyignore`の方針を「手書きソースの除外全廃（許容は`*_templ.go`と`**/logs/**`のみ）」へ改め、暫定除外（`internal/web/handler/`・`internal/bootstrap/`・`internal/service/risk/`）をサブパッケージ分割の完了に伴い順次削除し、手書きソースの暫定除外が残っていないことを確認済みの記述へ更新 | issue #243, #245, #246, #247, #248 |
| 1.20 | 2026-09-30 | depguardの強制範囲（`web` → `repository/**`のみ）を明記。lefthook/CIコメントの`go:embed`対象を`dist img vendor`へ更新 | 分割後レビュー指摘 |
| 1.21–1.22 | 2026-10-02 | Settings画面経由の入力対象に`UPDATE_GITHUB_TOKEN`（非公開リポジトリのリリース取得用、任意）を追加（1.21）したが、リポジトリのpublic化に伴い更新確認用トークン機能を廃止して削除（1.22） | issue #265、更新確認用トークン機能の廃止 |
| 1.23 | 2026-10-02 | Settings画面経由の入力対象に`JEV_MODEL`を追加し、必須キーを`JEV_API_KEY`/`KABU_API_PASSWORD`の2つへ更新（`JEV_BASE_URL`は既定値`https://api.typesafe.ai`付きの任意上書き）。履歴1.7の「必須3キー」は当時の記録 | issue #271/#291 |
| 1.24 | 2026-10-02 | 環境変数表`PITHA_SERVER_ADDR`の「`cmd/desktop`はネットワークポートを待ち受けない」を、Windowsのみ`/ws/...`専用のループバックリスナー（`127.0.0.1`と`[::1]`、ランダムポート）を起動する実態に合わせて修正 | issue #300（#266/#285の実装との乖離解消） |
| 1.25–1.26 | 2026-10-03 | CI/CD節に`GOTOOLCHAIN: auto`の設定理由を追記（`actions/setup-go` v7が`GOTOOLCHAIN=local`を設定し、`golangci-lint`の`go install`が失敗していた）。設定箇所はワークフロー全体ではなく`Install golangci-lint`/`Install linterly`のステップレベルが正（`actions/setup-go`の`$GITHUB_ENV`エクスポートがワークフローレベル`env`を上書きするため） | PR #304 CI `lint`ジョブ失敗の修正・再修正 |
| 1.27 | 2026-10-03 | `.linterlyignore`の内容ブロックを実ファイル（コメント文面を含む）に合わせ、`LICENSE`（ライセンス全文・手書きソースではない定型文）を許容する除外に追記 | issue #316（実ファイルとの乖離解消） |
| 1.28 | 2026-10-03 | 初回セットアップ手順の`JEV_BASE_URL`/`JEV_MODEL`の上書き先を、廃止済みの「詳細設定（任意）」からSettings画面のJev接続先モーダル内の任意項目へ訂正 | issue #315（#302 とのdoc-drift解消） |
| 1.29 | 2026-10-04 | 初回セットアップ手順から`cp .env.example .env`を削除し、`.env`は自動読込されずプロセス環境変数として設定する旨に統一（ファイル構成図の`.env.example`の説明も同趣旨に修正）。`.env.example`冒頭コメントも是正 | issue #386（環境変数節との矛盾解消） |
| 1.30 | 2026-10-04 | 「銘柄マスタの投入」節と`PITHA_UNIVERSE_PATH`を追加（`instruments`の起動時CSV投入手順） | issue #389 |
| 1.31 | 2026-10-04 | CI/CD節に最小権限（トップレベル`permissions: contents: read`、`release`のみ`contents: write`）・外部ActionのコミットSHA固定・`govulncheck`・Dependabot（gomod/github-actions/bun）を追記。Lint節に`gosec`/`bodyclose`/`noctx`/`rowserrcheck`と除外方針を追記。`go.mod`のGoを1.25.14へ更新（1.25.11は標準ライブラリの既知脆弱性6件に該当し`govulncheck`が失敗するため） | issue #375 |
| 1.32 | 2026-10-04 | 自動更新の署名検証を追加: CI `build`ジョブが`RELEASE_SIGNING_PUBLIC_KEY`/`RELEASE_SIGNING_KEY`設定時に`checksums.txt.sig`（ed25519分離署名）を生成・公開し、公開鍵を`-ldflags`で埋め込む。鍵の発行手順を追記 | issue #376 |
| 1.33 | 2026-10-05 | CIの署名発行・欠落時失敗の条件を`ci.yml`の`Sign checksums`の`if`（`main`へのpushとタグpushのみ。PR・`feat/**`は公開鍵の埋め込みのみ）に合わせて訂正 | issue #414 |
| 1.34 | 2026-10-05 | 「銘柄マスタの投入」節にUTF-8以外（Shift_JIS/CP932）のCSVは全体拒否される旨を追記、`make test`の`bun test`がCSPの`lightweight-charts`style hashとインストール版の一致を検証する旨を追記 | issue #393, #407 |
| 1.35 | 2026-10-05 | CIの`lint`/`test`/`build`で重複していたGo・bunセットアップ・フロントエンドビルド・templ生成を複合Action`.github/actions/setup`へ集約（`ci.yml`の300行上限超過を解消）。ジョブ名・ステップ内容は不変。Dependabotの`github-actions`に`/.github/actions/setup`を追加 | issue #402 |
| 1.36 | 2026-10-05 | 「銘柄マスタの投入」節に`symbol`の文字種（英数字のみ。違反行は全体拒否）を追記し、`market_index`/`sector_index`も`market-data`ジョブで板取得される旨（PUSH購読・候補更新は`stock`のみ）に訂正。CI/CD節の詳細を`environment/ci.md`へ分割（`setup.md`の行数上限超過を解消。内容は不変） | issue #418, #422, #423 |
| 1.37 | 2026-10-05 | Lint節のdepguard記述を`.golangci.yml`の2ルール（`web-no-repository`・`templ-no-service`）に訂正（「lintで強制するのは`web` → `repository/**`のみ」を削除） | issue #431 |
| 1.38 | 2026-10-05 | 環境変数表の`PITHA_POLICY_*`/`PITHA_FAST_SCREENER_*`に、上書き後の値も起動時検証される旨を追記 | issue #459 |
| 1.39 | 2026-10-05 | 「銘柄マスタの投入」節にCSV未投入時の画面案内とJPX東証上場銘柄一覧の確認付き自動取得（`POST /scanner/universe/import`）を追記 | issue #508 |
| 1.40 | 2026-10-05 | `make lint`をCIの`lint`ジョブ相当（`GOOS=windows go vet`・`linterly check`・`tsc --noEmit`を追加）に、lefthookのpre-commitに`typecheck`を追加し`biome`のglobへ`json`/`mjs`を追加。`.linterlyignore`に生成物`static/wailsjs/**`を追加 | issue #553/#555 |
| 1.41 | 2026-10-05 | 必要ツール表に`templ`CLI・`linterly`・`govulncheck`を追加し、Wails CLI・`golangci-lint`をCI固定版（`ci.yml`・`go.mod`）に合わせた。`environment/ci.md`の`linterly check --no-update-check`・`bun test`常時実行・初回タグ`v0.1.0`を`ci.yml`に合わせた | issue #519, #567, #578 |
| 1.42 | 2026-10-06 | depguard記述を`.golangci.yml`の7ルール（`atoms/molecules/organisms/layout-direction`・`templ-parts-no-middleware`を追加、Atomic Design依存方向とweb/middleware制限はlintで強制）に更新。CIジョブ構成（ファイル構成図・CI/CD節）に`test-windows`を追記。環境変数表に`PITHA_LOG_DIR`の独立行を追加（`.env.example`にも雛形を追加） | issue #521（再乖離）, #607, #608, #609 |
| 1.43 | 2026-10-06 | `LUNA_*`/`SOL_*`/`OPUS_*`/`NEWS_FEED_*`を既定（Jev/やのしん）の任意の差し替えとして位置づけ直し、`NEWS_FEED_ENABLED`を追加 | issue #273 |
| 1.44 | 2026-10-07 | 銘柄マスタCSVの`market_index`/`sector_index`の取得を、既定オフの60秒フルスキャン（`scan.full_scan_enabled: true`のときのみ）前提に訂正し、既定では指数行が更新されず市場コンテキスト特徴量が欠損になりうる旨を追記 | issue #654 |
| 1.45 | 2026-10-07 | 銘柄マスタの`market_index`/`sector_index`について、既定のランキング監視でも市場コンテキスト算出用に毎サイクル取得され（`market_breadth`は監視銘柄の集計）、指数行が無い・停止時のみ欠損になる旨に訂正 | issue #670 |
| 1.46 | 2026-10-07 | Lint節の有効linter一覧に`unused`を追加（`staticcheck`のU1000相当の未使用コードを`make lint`/CIで検出し、テストコードの未使用ヘルパーの再発を防ぐ） | issue #671 |
| 1.47 | 2026-10-07 | 「銘柄マスタの投入」節のJPX取得失敗時の表示を、下位エラーの生文字列ではなく失敗種別ごとの固定文言とし、下位エラーは`slog`のみに記録する旨に訂正 | issue #700 |
| 1.48 | 2026-10-07 | Lint節のdepguardを「8ルール」に訂正し、#702で追加した`web-no-bootstrap`（`internal/web/**`から`internal/bootstrap/**`のimportを拒否。`bootstrap` → `web`の一方向のみ）を追記 | issue #704 |
| 1.49 | 2026-10-08 | 環境変数表から`PITHA_LOG_DIR`/`PITHA_BACKUP_DIR`/`PITHA_POLICY_*`/`PITHA_FAST_SCREENER_*`を削除し、Settings画面で設定する運用項目の節（保存先キー・未設定時の挙動・再起動の要否）を追加 | issue #708 |
| 1.50 | 2026-10-08 | 「立花証券（e支店API）の事前準備」節を追加（e支店口座・標準Webのパスキー登録・API利用設定「利用する」・認証ID取得・鍵作成と公開鍵登録・書面確認・デモは別途デモ標準Web・NTP同期・IPv4）。外部APIの記述をブローカー選択式へ更新 | issue #721 |
