# 環境構築

## 概要

- 言語: Go 1.25+（`go.mod`の`go 1.25.11`に準拠。バックエンド・Scheduler・アダプタ全般）
- デスクトップシェル: Wails v2（WebView2、ネイティブウィンドウ/通知。OSトレイは未対応）
- サーバー: Gin + Huma（`/api/v1/...`）、Templ + HTMX（SSR）
- フロントエンド（リッチアイランドのみ）: Lit + TypeScript、ビルドは esbuild、パッケージマネージャは **bun** に固定
- スタイリング: Tailwind CSS
- DB: SQLite（`modernc.org/sqlite`、アプリ内蔵）+ golang-migrate + sqlite-vec（ベクトル検索）
- 外部API: kabuステーションAPI（三菱UFJ eスマート証券、旧auカブコム証券）、Jev / Sol / Opus / Luna API

技術スタックの詳細は `docs/architecture/overview.md` §2 技術スタック、レイヤー構造は同§3 を参照。本ドキュメントは開発環境・CI/CD・Lint/Format/Linterly/Git Hooks/Swagger の構築方針のみを扱う。

アプリケーション本体・CI（`.github/workflows/ci.yml`）・`Makefile`・`lefthook.yml`・`.golangci.yml`・`.linterly.yml`・`static/package.json`等の環境構築用ファイルはいずれも実装済みである。本ドキュメントはその現状の構成と方針を記述する。

## ディレクトリ構造

アプリケーション本体のディレクトリ構造は `docs/architecture/overview.md` §3 ディレクトリ構成・レイヤー構造 を参照。本ドキュメントに関連する環境構築用ファイルの配置は以下の通り。

```text
pitha-trador/
├── .github/
│   └── workflows/
│       └── ci.yml            # push/PR/タグ: lint → test → build（wails build -platform windows/amd64 -nsis -installscope user 含む）→ release（main push/タグpush時のみ）
├── .env.example               # 環境変数の一覧と説明（`cp .env.example .env`）
├── .bun-version               # CIで使うbunバージョン固定（setup-bunの`bun-version-file`）
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
- `.github/workflows/`には現状`ci.yml`のみが存在する。`e2e.yml`（実機E2E用）は未作成であり、必要になった時点で追加する（後述「CI/CD」節の注意を参照）
- フロントエンド（Lit/TypeScript）の依存管理は`static/`配下に閉じ、bunで管理する（Goモジュールとは独立）

## 開発環境セットアップ

### 必要ツール

| ツール | バージョン目安 | 用途 |
|---|---|---|
| Go | 1.25+（`go.mod`準拠） | バックエンド全般 |
| Wails CLI | v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`) | デスクトップアプリのビルド・`wails dev` |
| WebView2 Runtime | 最新（Windows 10/11は通常プリインストール済み） | Wailsのネイティブウィンドウ描画（Windows実機/`wails dev`時に必要） |
| bun | `.bun-version`記載のバージョン（CIと同一） | フロントエンド（Lit/TypeScript）の依存管理・ビルド |
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
# PITHA_STRATEGY_PATH/PITHA_RISK_PATH をリポジトリ内の config/*.yaml へ
# 設定するため（Makefileが自動設定）、それらを編集して再起動すればすぐ反映される
make dev
```

`JEV_API_KEY`/`JEV_BASE_URL`/`KABU_API_PASSWORD`/`SLACK_WEBHOOK_URL`/`LUNA_API_KEY`/`LUNA_BASE_URL`/`SOL_API_KEY`/`SOL_BASE_URL`/`OPUS_API_KEY`/`OPUS_BASE_URL`/`NEWS_FEED_URL`/`NEWS_FEED_API_KEY`は`.env`では設定しない（issue #57、Luna/Sol/Opus/News Ingest分は`architecture/overview.md` §8・§13）。アプリ起動後、Settings画面（`/settings`）から入力する。必須3キー（`JEV_API_KEY`/`JEV_BASE_URL`/`KABU_API_PASSWORD`）が未設定の間は、初回起動時にどのページを開いても専用のSetup画面（`/setup`）へリダイレクトされ、そこで入力を完了すると通常画面へ進める（issue #80）。詳細は`docs/architecture/overview.md` §5・§6・§8・§10.5・§13を参照。

### 環境変数

アプリ本体は`.env`を自動では読み込まない。以下はプロセス環境変数として設定する（一覧の雛形は`.env.example`）。API/Secret系（上記のJev/kabu/Slack/Luna/Sol/Opus/News）はSettings画面で入力するため対象外。

| 変数 | 参照元 | 既定値・挙動 |
|---|---|---|
| `PITHA_SERVER_ADDR` | `cmd/server` | HTTPサーバー（ヘッドレス起動）の待受アドレス。既定`127.0.0.1:48080`。loopback以外（`:48080`・`0.0.0.0`・LAN IP等）は起動を拒否する。`cmd/desktop`（Wails）はネットワークポートを待ち受けない |
| `PITHA_SERVER_ALLOW_NON_LOOPBACK` | `cmd/server` | `1`のときのみ`PITHA_SERVER_ADDR`にloopback以外を許可する（意図的な公開用） |
| `PITHA_SERVER_ALLOWED_HOSTS` | `cmd/server` | `PITHA_SERVER_ALLOW_NON_LOOPBACK=1`のときのみ有効。Hostヘッダとして受け付ける追加ホスト名（カンマ区切り、DNS rebinding対策のHost検証。loopback名（`localhost`/`127.0.0.1`/`::1`）とワイルドカード以外の`PITHA_SERVER_ADDR`のホストは常に許可） |
| `SWAGGER_ENABLED` | `internal/router` | `true`のときのみ`/swagger`を有効化。未設定・それ以外は無効＝オプトイン（後述「Swagger / OpenAPI」） |
| `PITHA_DB_PATH` | `internal/bootstrap` | SQLite DBファイルのパス。未設定（または空）は`os.UserConfigDir()`配下の`pitha-trador/pitha.db`（Windowsは`%AppData%\pitha-trador\pitha.db`） |
| `PITHA_BACKUP_DIR` | `internal/bootstrap` | SQLite DBの日次バックアップ（`requirements/non-functional.md` §3）の退避先ディレクトリ。ローカルディスク外（外部ドライブ・クラウド同期フォルダ等）を指定する。ディレクトリ自体は事前に存在している必要がある（作成しない。未マウントの場合はバックアップが失敗しSlack/ログで通知される）。Schedulerの日次ジョブ（起動直後・10分ごとの未実行検出と毎日16:00）が`PRAGMA wal_checkpoint(TRUNCATE)`後の整合コピーを`daily/pitha-YYYY-MM-DD.db`へ保存し（`secrets`テーブルは空にし、`0700`/`0600`で作成）、90日超の日次分は削除、各ISO週の最初のバックアップ分を`weekly/pitha-YYYY-MM-DD.db.gz`（日付はその週の月曜）として52週保持する。未設定・空のときはバックアップ無効（起動ログに警告）。復元はアプリ停止後にバックアップファイルを`PITHA_DB_PATH`（既定パス）へ置き換え、Setup画面でAPIキー・パスワードを再入力する |
| `PITHA_STRATEGY_PATH` / `PITHA_RISK_PATH` | `internal/bootstrap` | `config/strategy.yaml`・`config/risk.yaml`の場所。優先順位は明示指定 > 本環境変数 > 実行ファイルと同じディレクトリの`config/*.yaml` > 埋め込み既定値（`architecture/overview.md` §9） |
| `PITHA_STATIC_DIR` | `internal/router` | 設定すると`/static/...`を`go:embed`ではなく指定ディレクトリ（存在するディレクトリのみ有効。`make dev`は`static/src`）から配信する。未設定・不正パスは埋め込みにフォールバック |
| `PITHA_POLICY_LONG_*` / `PITHA_POLICY_SHORT_*` | `internal/config` | `config/strategy.yaml`の`policy.long`/`policy.short`のしきい値を起動時に上書きする（FR-POLICY-4）。サフィックスは`MIN_PROBABILITY`・`MIN_ENTRY_QUALITY`・`MIN_CONTINUATION_PROBABILITY`・`MAX_TOXIC_FLOW`・`MAX_LIQUIDITY_STRESSED`。数値は不正値だと起動エラー |
| `PITHA_FAST_SCREENER_*` | `internal/config` | `fast_screener`のフィルター・重みを起動時に上書きする（FR-FS-1/FR-FS-3、名前は`.env.example`と`requirements/functional.md`参照）。DB `runtime_settings`の`screener.*`が最優先 |

### Makefileターゲット

`make`（引数なし）または`make help`でコマンド一覧を表示する（既定ターゲットは`help`）。

| ターゲット | 内容 |
|---|---|
| `make dev` | `wails dev`・`templ generate --watch`・`bun --cwd=static run dev`を並行起動 |
| `make generate` | `templ generate`と`bun run --cwd static build`。`lint`/`test`/`build`の前提 |
| `make lint` | `generate`後に`golangci-lint run`と`bun run --cwd static lint`（`static/`で`biome check .`を実行。ルートで`bunx biome`を実行すると`@biomejs/biome`ではなく無関係なnpmパッケージ`biome`を解決して何も検査しないため、`static/`から実行する）。CIの`lint`ジョブが実行する`linterly check`と`bunx tsc --noEmit`は含まない（`linterly check`はlefthookのpre-commitで、`tsc --noEmit`はCIのみで実行される） |
| `make test` | `generate`後に`go test ./...`と`bun --cwd=static test` |
| `make build` | `generate`後に`wails build -platform windows/amd64` |
| `make openapi-export` | 起動中サーバー（`127.0.0.1:48080`）から`docs/api/openapi.json`を書き出す（任意タスク。ファイルは未コミット） |

- **`make dev`の環境変数**: `wails dev`は`cmd/desktop`をカレントとして動くため、`Makefile`は`PITHA_STRATEGY_PATH`/`PITHA_RISK_PATH`を`$(CURDIR)/config/*.yaml`（絶対パス）に設定する。`internal/bootstrap.Run`は環境変数を埋め込み既定値より優先するため、`config/risk.yaml`等を編集して`make dev`を再起動すれば再ビルドなしで反映される（埋め込み既定値はビルド時のスナップショット）。`PITHA_STATIC_DIR`は`static/src`に設定し、`/static/...`をディスクから配信する。`wails dev`のファイル監視は既定で`.go`変更時のみGoバイナリを再ビルドするため、この上書きが無いと`bun run dev`（esbuild/Tailwind watch）の出力がgo:embedのスナップショットに阻まれ`make dev`再起動まで反映されない
- **`generate`が前提となる理由**: `templ generate`が`*_templ.go`を、`bun run --cwd static build`が`static/src/dist/{css,js}`を生成する。どちらも`.gitignore`対象であり、`static/src/embed.go`の`//go:embed dist vendor`は`dist/`が空だとコンパイル自体が失敗する。そのためクリーンなチェックアウトでは、生成前に`go vet`/golangci-lint/`go test`/`wails build`のいずれも実行できない（古い生成物が残っていると陳腐化した出力に対して実行してしまう）。CIの`lint`/`test`/`build`各ジョブも同じ2ステップを先に実行し、`lefthook`のpre-commit/pre-pushも`make generate`を呼ぶ

## CI/CD

GitHub Actions（`.github/workflows/ci.yml`）。

- **トリガー**: `push`（main, feat/**）、タグ`v*`のpush、`pull_request`
- **ジョブ構成**: `lint` → `test` → `build` → `release` の順に実行（前段が失敗したら後段はスキップ。`release`は下記の条件を満たす場合のみ実行）
  - `lint`: フロントエンドビルド（`bun install --cwd static --frozen-lockfile` + `bun run --cwd static build`）→ `golangci-lint run` ＋ `linterly check`（行数制限。lefthookの`--no-verify`回避対策）＋ `bunx biome check .` ＋ `bunx tsc --noEmit`
  - `test`: フロントエンドビルド → `go test ./...` ＋（フロントエンドの単体テストがある場合）`bun --cwd static test`
  - `build`: フロントエンドビルド → `wails build -platform windows/amd64 -nsis -installscope user` でNSISインストーラー（`.exe`、ユーザースコープインストール）をビルドしCI Artifactとしてアップロードする。SHA256チェックサムも同時に生成する。バージョンは`main`へのpushでは既存の最新`vX.Y.Z`タグのパッチ+1、タグpushではタグ名、それ以外（PR・`feat/**`）は`dev`を`-ldflags`で埋め込む
  - `release`: `build`の成果物（インストーラー・`checksums.txt`）を`softprops/action-gh-release@v2`でGitHub Releaseとして公開する。`main`へのpush（＝PRマージ、次パッチ版を自動採番）またはタグ`v*`のpush（手動リリース）でのみ実行され、`tag_name`は`build`ジョブが算出した版番号を使う
  - `config/strategy.yaml`・`config/risk.yaml`・静的アセット（`static/src/dist`・`static/src/vendor`）は`go:embed`でバイナリに埋め込む（`architecture/overview.md` §9）。`static/src/embed.go`は空/未ビルドの`dist`を埋め込もうとすると`go build`自体がコンパイルエラーになるため、`lint`/`test`/`build`いずれのジョブも上記フロントエンドビルドを最初のGoコンパイル系ステップより前に実行する必要がある
- **バージョン固定**: bunは`.bun-version`（`oven-sh/setup-bun`の`bun-version-file`）、templ・wails・golangci-lint・linterlyはワークフロー内でバージョンを固定する。`latest`は使わない
- **同時実行制御**: ワークフロー全体に`concurrency: { group: ci-${{ github.ref }}, cancel-in-progress: false }`を設定する。版番号の採番（`build`）とタグ作成（`release`）が別ジョブのため、`main`への連続pushで並行実行されると同じ版番号を算出してタグが衝突しうる。同一refの実行を直列化して防ぐ
- **実行環境**: `ubuntu-latest`のみで完結する。Wails v2のWindowsターゲットはpure Go実装であり、DBドライバも`modernc.org/sqlite`（+`modernc.org/sqlite/vec`）でCGO不要のため、`GOOS=windows`へのクロスコンパイルがLinux上でそのまま成立する（mingw等のクロスコンパイラも不要）。よってWindowsランナーを毎PRで使う必要はない
- **注意**: WebView2はWindows専用のランタイムのため、`.exe`を実際に起動してUIを操作するE2Eテスト（`components/runtime.md` §9）は`ubuntu-latest`では実行できない。そのようなテストが必要になった場合のみ、`.github/workflows/e2e.yml`（**現状は未作成**）をタグpush等の低頻度トリガーで`windows-latest`ランナーにより別途追加して実行する（通常のlint/test/buildフローには含めない）

## Lint

| 対象 | ツール | 設定ファイル |
|---|---|---|
| Go | golangci-lint | `.golangci.yml` |
| フロントエンド（Lit/TypeScript） | Biome | `static/biome.json` |

- `.golangci.yml`は`default: none`とし、`govet`・`staticcheck`・`errcheck`・`ineffassign`・`depguard`のみを有効化する（`gofmt`はlinterではなく`formatters:`で有効化）
- `depguard`の`web-no-repository`ルールが、`internal/web/**`から`internal/repository`へのimportを拒否してレイヤー規約（`architecture/overview.md` §3）をlintで強制する
- Biomeはlintとformatを1ツールで兼ねるため、`static/`配下は追加のESLint/Prettier設定を持たない

## Format

| 対象 | ツール | コマンド |
|---|---|---|
| Go | gofmt（標準、`.golangci.yml`の`formatters:`で有効化されCI検証も兼ねる） | `go fmt ./...` |
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

`.linterlyignore`（抜粋。実ファイルの全文が正）:

```text
# 実行時ログ（ソースコードではない）
**/logs/**

# 自動生成コード（Templが生成するGoコード）
*_templ.go

# 既知債務（手書きソース）: ディレクトリ2000行上限の暫定除外（issue #134 で追跡）。
# internal/repository/・internal/web/handler/・internal/bootstrap/・
# internal/service/risk/ の手書きソース（テスト含む）を個別ファイル単位で列挙している。
# 新規追加は禁止（必要になった時点でサブパッケージ分割を先に行う）
```

- 個別ファイル列挙の除外はすべて上記の既知債務で、issue #134でサブパッケージ分割により解消するまでの暫定措置である。生成物・ログ以外の除外パターンを新たに追加してはならない
- 除外中のファイルも1ファイル300行以内に保つ（現時点の唯一の例外は`internal/web/handler/settings_test.go`）

`static/src/dist/`（esbuildビルド成果物。`static/esbuild.config.mjs`の`outdir: src/dist/js`、Tailwind出力は`static/src/dist/css`。`.gitignore`対象）は`default_excludes: true`により自動除外される想定。手書きソースコードの除外パターンは基本追加しない。

## Git Hooks

Lefthook（`lefthook.yml`）:

```yaml
pre-commit:
  commands:
    golangci-lint:
      glob: "*.go"
      run: make generate && golangci-lint run
    biome:
      root: static/
      glob: "**/*.{ts,css}"
      run: bunx biome check {staged_files}
    linterly:
      run: linterly check

pre-push:
  commands:
    go-test:
      run: make generate && go test ./...
```

`biome`は`root: static/`で`static/`をカレントにして実行する（`@biomejs/biome`は`static/package.json`のdevDependencyであり、リポジトリルートの`bunx biome`は無関係なnpmパッケージ`biome`を解決してしまうため）。`root`指定時、`{staged_files}`は`static/`配下のステージ済みファイルのみが`static/`相対パスで渡され、`glob`もその相対パスに対して評価される。

`golangci-lint`と`go-test`の前に`make generate`を実行するのは、`*_templ.go`と`static/src/dist/`が未生成だと`go:embed`でコンパイルできない（または古い生成物に対して実行してしまう）ため。`linterly check`は`{staged_files}`を渡さずリポジトリ全体を検査する（ディレクトリ単位の行数上限のため）。

## Swagger / OpenAPI

APIサーバー（Huma）を含むプロジェクトのため対象。`docs/api/endpoints.md`のAPIルート（`/api/v1/...`）に対応する。

- **Spec生成ツール**: Huma組み込みの自動生成（`swag`等のアノテーション方式は不要）。Go構造体のリフレクションからリクエスト起動時に都度OpenAPI 3.1スペックを生成するため、実装とspecがずれることが構造的にない
- **エンドポイント**: `/api/v1/openapi.json`（Huma生成のspec本体）
- **UI**: Stoplight Elements（`@stoplight/elements`、bunで導入）。`static/package.json`の依存として`bun.lock`でバージョンを固定し、`static/esbuild.config.mjs`が`web-components.min.js`/`styles.min.css`を`static/src/dist/vendor/stoplight-elements/`へコピーして`go:embed`でバイナリに同梱、`/static/dist/vendor/stoplight-elements/`から同一オリジン配信する（CDN読み込みはしない。取引操作APIを持つオリジンでサードパーティスクリプトを実行しないため）。`/swagger`固定エンドポイントで、`<elements-api apiDescriptionUrl="/api/v1/openapi.json">` を埋め込んだ静的HTML 1枚を返す。Swagger UI用の追加ミドルウェアは不要
- **環境変数**: `SWAGGER_ENABLED`（`true`のときのみ`/swagger`ルートを有効化。未設定・`true`以外は無効＝オプトイン）。`/api/v1/openapi.json`は本変数に関わらず常に公開される。`make dev`は`true`を設定し、本番（Phase 7実売買時）を含むそれ以外は未設定（無効）とする
- **CI連携**: Huma生成spec方式のため「生成し忘れによるdrift」が構造的に発生しない。よってswag方式で一般的な`swag init && git diff --exit-code`のようなdrift検知CIステップは不要。外部ツール（Postman等）向けにspecファイルをエクスポートしたい場合のみ、任意タスクとして`make openapi-export`（`docs/api/openapi.json`へ書き出し）を用意する

## 改訂履歴

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
| 1.11 | 2026-09-29 | 環境変数表に`PITHA_BACKUP_DIR`（SQLite日次バックアップの退避先）を追加 | issue #97（DB日次バックアップ未実装の解消） |
| 1.12 | 2026-09-29 | 環境変数表に`PITHA_SERVER_ALLOWED_HOSTS`（Host検証の追加許可ホスト）を追加 | issue #136 |
| 1.13 | 2026-09-29 | `PITHA_BACKUP_DIR`の説明を更新（`secrets`除外・パーミッション・週次52週保持・退避先必須・catch-up実行） | issue #137/#152/#159 |
| 1.14 | 2026-09-29 | Lint/Format/Linterly/Git Hooks節を実ファイル（`.golangci.yml`の有効linterとdepguard、`lefthook.yml`、`.linterlyignore`）に合わせて是正。`make lint`とCI `lint`ジョブの差分を明記。`.env.example`に`PITHA_SERVER_ALLOW_NON_LOOPBACK`/`PITHA_SERVER_ALLOWED_HOSTS`/`PITHA_STATIC_DIR`/`PITHA_POLICY_*`の雛形を追加 | issue #154 |
