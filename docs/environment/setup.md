# 環境構築

## 概要

- 言語: Go 1.23+（バックエンド・Scheduler・アダプタ全般）
- デスクトップシェル: Wails v2（WebView2、ネイティブウィンドウ/トレイ/通知）
- サーバー: Gin + Huma（`/api/v1/...`）、Templ + HTMX（SSR）
- フロントエンド（リッチアイランドのみ）: Lit + TypeScript、ビルドは esbuild、パッケージマネージャは **bun** に固定
- スタイリング: Tailwind CSS
- DB: SQLite（`modernc.org/sqlite`、アプリ内蔵）+ golang-migrate + sqlite-vec（ベクトル検索）
- 外部API: kabuステーションAPI（三菱UFJ eスマート証券、旧auカブコム証券）、Jev / Sol / Opus / Luna API

技術スタックの詳細は `docs/architecture/overview.md` §2 技術スタック、レイヤー構造は同§3 を参照。本ドキュメントは開発環境・CI/CD・Lint/Format/Linterly/Git Hooks/Swagger の構築方針のみを扱う。

現時点でこのリポジトリに既存のコード・設定ファイルは無い（仕様書のみのグリーンフィールド状態）。以下は新規導入する構成である。

## ディレクトリ構造

アプリケーション本体のディレクトリ構造は `docs/architecture/overview.md` §3 ディレクトリ構成・レイヤー構造 を参照。本ドキュメントに関連する環境構築用ファイルの配置は以下の通り。

```text
pitha-trador/
├── .github/
│   └── workflows/
│       ├── ci.yml            # push/PR: lint → test → build（wails build -platform windows/amd64 含む）
│       └── e2e.yml            # タグpush時: windows-latestで.exeを実起動しPlaywright E2E（任意）
├── .golangci.yml              # Go lint設定
├── .linterly.yml              # 行数リンター設定
├── .linterlyignore
├── lefthook.yml                # Git Hooks
├── Makefile                    # 開発タスク（build/dev/lint/test/openapi-export等）
├── static/
│   ├── package.json            # フロントエンド（Lit/TS）依存。bunで管理
│   ├── bun.lock
│   └── biome.json               # フロントエンドLint+Format設定
└── docs/
    └── environment/
        └── setup.md             # 本ドキュメント
```

- Goモジュールのルートは`pitha-trador/`直下（`go.mod`）
- フロントエンド（Lit/TypeScript）の依存管理は`static/`配下に閉じ、bunで管理する（Goモジュールとは独立）

## 開発環境セットアップ

### 必要ツール

| ツール | バージョン目安 | 用途 |
|---|---|---|
| Go | 1.23+ | バックエンド全般 |
| Wails CLI | v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`) | デスクトップアプリのビルド・`wails dev` |
| WebView2 Runtime | 最新（Windows 10/11は通常プリインストール済み） | Wailsのネイティブウィンドウ描画（Windows実機/`wails dev`時に必要） |
| bun | 最新 | フロントエンド（Lit/TypeScript）の依存管理・ビルド |
| golang-migrate CLI | v4（`go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest`） | マイグレーションファイルの手動生成・確認用（アプリ起動時は自動適用） |
| golangci-lint | 最新 | Go lint |
| Lefthook | 最新（`go install github.com/evilmartians/lefthook@latest` または `bun add -D lefthook`） | Git Hooks |
| kabuステーションAPI | 三菱UFJ eスマート証券（旧auカブコム証券）提供 | Windows実機での市場データ・発注検証（開発時はモックサーバーで代替可） |

### 初回セットアップ手順

```bash
# Go依存関係
go mod download

# フロントエンド依存関係（bun固定）
bun --cwd static install

# 環境変数
cp .env.example .env

# Git Hooks
lefthook install

# 開発起動（templ generate --watch / esbuild watch / wails dev を並行起動）
make dev
```

## CI/CD

GitHub Actions（`.github/workflows/ci.yml`）。

- **トリガー**: `push`（main, feat/**）、`pull_request`
- **ジョブ構成**: `lint` → `test` → `build` の順に実行（前段が失敗したら後段はスキップ）
  - `lint`: `golangci-lint run` ＋ `bunx biome check static/`
  - `test`: `go test ./...` ＋（フロントエンドの単体テストがある場合）`bun --cwd static test`
  - `build`: `wails build -platform windows/amd64` で実際の`.exe`をビルドしCI Artifactとしてアップロードする
- **実行環境**: `ubuntu-latest`のみで完結する。Wails v2のWindowsターゲットはpure Go実装であり、DBドライバも`modernc.org/sqlite`（+`modernc.org/sqlite/vec`）でCGO不要のため、`GOOS=windows`へのクロスコンパイルがLinux上でそのまま成立する（mingw等のクロスコンパイラも不要）。よってWindowsランナーを毎PRで使う必要はない
- **注意**: WebView2はWindows専用のランタイムのため、`.exe`を実際に起動してUIを操作するE2Eテスト（`components/overview.md` §9）は`ubuntu-latest`では実行できない。そのようなテストが必要になった場合のみ、`.github/workflows/e2e.yml`をタグpush等の低頻度トリガーで`windows-latest`ランナーにより別途実行する（通常のlint/test/buildフローには含めない）

## Lint

| 対象 | ツール | 設定ファイル |
|---|---|---|
| Go | golangci-lint | `.golangci.yml` |
| フロントエンド（Lit/TypeScript） | Biome | `static/biome.json` |

- `.golangci.yml`には標準的なlinter（`govet`, `staticcheck`, `errcheck`, `ineffassign`, `gosimple`, `gofmt`）を有効化する
- Biomeはlintとformatを1ツールで兼ねるため、`static/`配下は追加のESLint/Prettier設定を持たない

## Format

| 対象 | ツール | コマンド |
|---|---|---|
| Go | gofmt（標準、golangci-lintの`gofmt`linterでCI検証も兼ねる） | `go fmt ./...` |
| フロントエンド | Biome | `bunx biome format --write static/src` |

## Linterly

`.linterly.yml`（デフォルト値を使用、特別な理由がない限り変更しない）:

```yaml
rules:
  # max_lines_per_file / max_lines_per_directory はデフォルト値（300/2000）を推奨。
  # 特別な理由がない限り変更しない。
  # max_lines_per_file: 300
  # max_lines_per_directory: 2000
  warning_threshold: 10       # 早めに警告を出す

count_mode: all               # 変更しない
default_excludes: true        # ビルド成果物等のデフォルト除外を有効化
language: ja

# ignore でパス別に除外する場合も慎重に。
# 生成コードなど明確な理由がある場合のみ追加する。
# ignore:
#   - "**/*_templ.go"
```

`.linterlyignore`:

```text
# 自動生成コード（Templが生成するGoコード。手書きソースコードの除外は基本追加しない）
*_templ.go
```

`static/dist/`（esbuildビルド成果物）は`default_excludes: true`により自動除外される想定。手書きソースコードの除外パターンは基本追加しない。

## Git Hooks

Lefthook（`lefthook.yml`）:

```yaml
pre-commit:
  commands:
    golangci-lint:
      glob: "*.go"
      run: golangci-lint run
    biome:
      glob: "static/src/**/*.{ts,css}"
      run: bunx biome check {staged_files}
    linterly:
      run: linterly check {staged_files}

pre-push:
  commands:
    go-test:
      run: go test ./...
```

## Swagger / OpenAPI

APIサーバー（Huma）を含むプロジェクトのため対象。`docs/api/endpoints.md`のAPIルート（`/api/v1/...`）に対応する。

- **Spec生成ツール**: Huma組み込みの自動生成（`swag`等のアノテーション方式は不要）。Go構造体のリフレクションからリクエスト起動時に都度OpenAPI 3.1スペックを生成するため、実装とspecがずれることが構造的にない
- **エンドポイント**: `/api/v1/openapi.json`（Huma生成のspec本体）
- **UI**: Stoplight Elements（`@stoplight/elements`、bunで導入）。`/swagger`固定エンドポイントで、`<elements-api apiDescriptionUrl="/api/v1/openapi.json">` を埋め込んだ静的HTML 1枚を返す。Swagger UI用の追加ミドルウェアは不要
- **環境変数**: `SWAGGER_ENABLED`（`true`/`false`）で`/swagger`ルートの有効/無効を切り替える。開発・ステージングは`true`（デフォルト）、本番（Phase 7実売買時）は`false`
- **CI連携**: Huma生成spec方式のため「生成し忘れによるdrift」が構造的に発生しない。よってswag方式で一般的な`swag init && git diff --exit-code`のようなdrift検知CIステップは不要。外部ツール（Postman等）向けにspecファイルをエクスポートしたい場合のみ、任意タスクとして`make openapi-export`（`docs/api/openapi.json`へ書き出し）を用意する

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-26 | 新規作成 | 初版 |
| 1.1 | 2026-09-26 | CI構成を修正: `wails build -platform windows/amd64`は`ubuntu-latest`上でクロスビルド可能（Wails Windowsターゲット・`modernc.org/sqlite`系ドライバがいずれもpure GoでCGO不要なため）と判明したため、`build`ジョブに実ビルドを含め、Windowsランナーは実機E2Eテストのみに限定 | ユーザー指摘によるファクトチェック・設計修正 |
