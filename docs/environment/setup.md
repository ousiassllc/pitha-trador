# 環境構築

## 概要

- 言語: Go 1.25+（`go.mod`の`go 1.25.14`に準拠。バックエンド・Scheduler・アダプタ全般）
- デスクトップシェル: Wails v2（WebView2、ネイティブウィンドウ/通知。OSトレイは未対応）
- サーバー: Gin + Huma（`/api/v1/...`）、Templ + HTMX（SSR）
- フロントエンド（リッチアイランドのみ）: Lit + TypeScript、ビルドは esbuild、パッケージマネージャは **bun** に固定
- スタイリング: Tailwind CSS
- DB: SQLite（`modernc.org/sqlite`、アプリ内蔵）+ golang-migrate + sqlite-vec（ベクトル検索）
- 外部API: ブローカーAPI（kabuステーションAPI＝三菱UFJ eスマート証券・旧auカブコム証券、または立花証券・e支店API。1プロセスで1つを選択）、Jev / Sol / Opus / Luna API

技術スタックの詳細は `docs/architecture/overview.md` §2 技術スタック、レイヤー構造は同§3 を参照。本ドキュメントは開発環境・Lint/Format/Linterly/Git Hooks/Swagger の構築方針を扱う。CI/CD（GitHub Actions）の詳細は `environment/ci.md` に分割している。

アプリケーション本体・CI（`.github/workflows/ci.yml`）・`Makefile`・`lefthook.yml`・`.golangci.yml`・`.linterly.yml`・`static/package.json`等の環境構築用ファイルはいずれも実装済みである。本ドキュメントはその現状の構成と方針を記述する。

## ディレクトリ構造

アプリケーション本体のディレクトリ構造は `docs/architecture/overview.md` §3 ディレクトリ構成・レイヤー構造 を参照。本ドキュメントに関連する環境構築用ファイルの配置は以下の通り。

```text
pitha-trador/
├── .github/
│   ├── dependabot.yml        # Dependabot（gomod / github-actions / bun）
│   ├── actions/setup/
│   │   └── action.yml        # 複合Action: setup-go / setup-bun / フロントエンドビルド / templ生成（lint・test・buildで共用）
│   └── workflows/
│       └── ci.yml            # push/PR/タグ: lint（govulncheck含む）→ test・test-windows（並列）→ build（wails build -platform windows/amd64 -nsis -installscope user 含む）→ release（main push/タグpush時のみ）
├── .env.example               # 環境変数の一覧と説明の雛形（`.env`は自動読込されない。値はプロセス環境変数として設定する）
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
        ├── setup.md             # 本ドキュメント
        └── ci.md                # CI/CD（GitHub Actions）の詳細
```

- Goモジュールのルートは`pitha-trador/`直下（`go.mod`）
- `.github/workflows/`には現状`ci.yml`のみが存在する。共通セットアップは`.github/actions/setup/action.yml`（複合Action）。`e2e.yml`（実機E2E用）は未作成であり、必要になった時点で追加する（後述「CI/CD」節の注意を参照）。依存の自動更新設定は`.github/dependabot.yml`
- フロントエンド（Lit/TypeScript）の依存管理は`static/`配下に閉じ、bunで管理する（Goモジュールとは独立）

## 開発環境セットアップ

### 必要ツール

| ツール | バージョン目安 | 用途 |
|---|---|---|
| Go | 1.25+（`go.mod`準拠） | バックエンド全般 |
| Wails CLI | v2.16.0（`go.mod`の`wails/v2`・`ci.yml`と同版。`go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`） | デスクトップアプリのビルド・`wails dev` |
| WebView2 Runtime | 最新（Windows 10/11は通常プリインストール済み） | Wailsのネイティブウィンドウ描画（Windows実機/`wails dev`時に必要） |
| bun | `.bun-version`記載のバージョン（CIと同一） | フロントエンド（Lit/TypeScript）の依存管理・ビルド |
| golang-migrate CLI | v4（`go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest`） | マイグレーションファイルの手動生成・確認用（アプリ起動時は自動適用） |
| templ CLI | `go.mod`の`a-h/templ`と同版（現在v0.3.1020。CIは`ci.yml`の`TEMPL_VERSION`。`go install github.com/a-h/templ/cmd/templ@v0.3.1020`） | `templ generate`。`make generate`・`make dev`が実行するため、`make lint`/`make test`/`make build`・lefthookのpre-commit/pre-pushに必須 |
| golangci-lint | v2.14.0（`ci.yml`の`GOLANGCI_LINT_VERSION`と同版。`GOTOOLCHAIN=auto go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`。`go >= 1.26`を要求するため`GOTOOLCHAIN=auto`が要る） | Go lint（`make lint`・pre-commit） |
| linterly | v0.3.3（`ci.yml`の`LINTERLY_VERSION`と同版。`GOTOOLCHAIN=auto go install github.com/ousiassllc/linterly/cmd/linterly@v0.3.3`） | 行数制限の検査（`make lint`・pre-commit。下記「Linterly」節） |
| govulncheck | v1.7.0（`ci.yml`の`GOVULNCHECK_VERSION`。`go install golang.org/x/vuln/cmd/govulncheck@v1.7.0`） | 既知脆弱性の検査。CIの`lint`ジョブ専用（`make lint`には含まれない。ローカル実行は任意） |
| Lefthook | 最新（`go install github.com/evilmartians/lefthook@latest` または `bun add -D lefthook`） | Git Hooks |
| kabuステーションAPI | 三菱UFJ eスマート証券（旧auカブコム証券）提供 | kabuをブローカーに選ぶ場合（既定）のWindows実機での市場データ・発注検証（開発時はモックサーバーで代替可） |

### 立花証券（e支店API）の事前準備（ブローカーに立花証券を選ぶ場合。issue #721）

ブローカーは1プロセスで1つを選ぶ（`broker.provider`。既定kabu。切替はSettings＋再起動。`architecture/overview/integrations.md` §5）。立花証券を選ぶ場合は、アプリを動かす前に次を**操作者が標準Webで行う**（一次資料: [API専用ページ「３．ご利用方法」](https://www.e-shiten.jp/e_api/mfds_json_api_menu.html)、[v4r9スケジュール告知](https://www.e-shiten.jp/api/20260513.html)）。立花証券を選ばない（kabuのまま）場合は不要。

1. **口座**: 立花証券・e支店の口座を開設する（API利用に口座が必要）。
2. **パスキー登録**: 標準Web（本番）のログインでパスキーを登録する。本番のAPI（v4r9以降）の利用にはパスキー認証が必要で、登録後は標準Webの電話番号認証は使えない。
3. **API利用設定**: 標準Webの「お客様情報」→「e支店・API利用設定」で、既定の「利用しない」を**「利用する」**に変更する。
4. **認証IDの取得**: 同じ画面で認証ID（`sAuthId`）を取得する。
5. **秘密鍵・公開鍵の作成と公開鍵の登録**: 同じ画面で鍵を作成（利用者自身でも作成可）し、公開鍵を登録する。**秘密鍵は自分で管理する**（ファイルに保存し、OS権限で本人のみ読み取り可にする。Gitやクラウド共有に置かない。DBには本文を保存しない。`requirements/non-functional.md` §4）。
6. **書面の確認**: 各種書面（金商法交付書面等）を標準Webで既読にする。未読だと再認証が正常でも仮想URLが発行されずAPIが使えない。書面は追加・変更のたびに再確認が要る。
7. **デモ環境**: デモは本番とは**別の**デモ標準Web（`https://demo.e-shiten.jp`。[デモ環境の案内](https://www.e-shiten.jp/Service/demo.html)）で、上記3〜5（API利用設定・認証ID・鍵）を**デモ専用に**設定する。デモの認証ID・秘密鍵・公開鍵は本番と別セットで、デモはパスキー認証が不要。まずデモで検証し（#724・#725）、本番は読み取り（市況データ）のみで検証してから既定の切替を判断する。
8. **PC環境**: インターネットに直結（IPv4。IPv6のみの回線では`10005`で失敗する）し、PC時計をNTPで正確に合わせる（APIが要求の時刻`p_sd_date`を30秒の範囲で検査する）。API側の固定IP登録は任意で、動的IP回線では使わない。

取得した認証ID・秘密鍵のパスはSettings画面で入力する（#723・#733。立花アダプタの実装は#724）。発注は#55まで行わないため、第二暗証番号は本番では登録・保持しない。

### 初回セットアップ手順

```bash
# Go依存関係
go mod download

# フロントエンド依存関係（bun固定）
bun --cwd static install

# 環境変数は`.env`を経由せず、必要に応じてシェル/OSのプロセス環境変数として設定する
# （既定値のままなら設定不要。変数一覧は下記「環境変数」節と`.env.example`）

# Git Hooks
lefthook install

# 開発起動（templ generate --watch / esbuild watch / wails dev を並行起動）
# PITHA_STRATEGY_PATH/PITHA_RISK_PATH をリポジトリ内の config/*.yaml へ
# 設定するため（Makefileが自動設定）、それらを編集して再起動すればすぐ反映される
make dev
```

`JEV_API_KEY`/`JEV_BASE_URL`/`JEV_MODEL`/`KABU_API_PASSWORD`/`SLACK_WEBHOOK_URL`/`LUNA_API_KEY`/`LUNA_BASE_URL`/`SOL_API_KEY`/`SOL_BASE_URL`/`OPUS_API_KEY`/`OPUS_BASE_URL`/`NEWS_FEED_URL`/`NEWS_FEED_API_KEY`/`NEWS_FEED_ENABLED`/`TACHIBANA_DEMO_AUTH_ID`/`TACHIBANA_PROD_AUTH_ID`/`TACHIBANA_DEMO_SECOND_PASSWORD`は`.env`では設定しない（issue #57、Luna/Sol/Opus/News Ingest分は`architecture/overview.md` §8・§13）。アプリ起動後、Settings画面（`/settings`）から入力する。Luna/Sol/Opusは既定でJev（`JEV_API_KEY`のみ）、ニュースフィードは既定でやのしんTDnet WebAPI（キー不要）で動くため、`LUNA_*`/`SOL_*`/`OPUS_*`/`NEWS_FEED_*`は役ごと・フィードの任意の差し替え（`NEWS_FEED_ENABLED`に`off`でニュース取り込みを停止）であり、未入力でも起動は失敗しない（issue #273）。必須2キー（`JEV_API_KEY`/`KABU_API_PASSWORD`）が未設定（`JEV_BASE_URL`は既定値`https://api.typesafe.ai`があり、`JEV_MODEL`（既定`jev-latest`）とともにSettings画面のJev接続先モーダル内の任意項目から上書きする。issue #271・#272・#274）の間は、初回起動時にどのページを開いても専用のSetup画面（`/setup`）へリダイレクトされ、そこで入力を完了すると通常画面へ進める（issue #80）。詳細は`docs/architecture/overview.md` §5・§6・§8・§10.5・§13を参照。

### 環境変数

アプリ本体は`.env`を自動では読み込まない。以下はプロセス環境変数として設定する（一覧の雛形は`.env.example`）。API/Secret系（上記のJev/kabu/Slack/Luna/Sol/Opus/News）とバックアップ先・ログディレクトリ・Policy/Fast Screenerしきい値（下記「Settings画面で設定する運用項目」）はSettings画面で入力するため対象外。

| 変数 | 参照元 | 既定値・挙動 |
|---|---|---|
| `PITHA_SERVER_ADDR` | `cmd/server` | HTTPサーバー（ヘッドレス起動）の待受アドレス。既定`127.0.0.1:48080`。loopback以外（`:48080`・`0.0.0.0`・LAN IP等）は起動を拒否する。`cmd/desktop`（Wails）のページ・APIはAssetServer経由で配信され、ネットワークポートを待ち受けない。例外としてWindows（WebView2）のみ、`/ws/...`のUpgradeだけを受ける専用のループバックリスナーをランダムポートで起動する（`127.0.0.1`と`[::1]`の同一ポート。`[::1]`が使えない環境では`127.0.0.1`のみ。本変数の対象外、`cmd/desktop/ws_listener.go`。`api/endpoints.md` §6） |
| `PITHA_SERVER_ALLOW_NON_LOOPBACK` | `cmd/server` | `1`のときのみ`PITHA_SERVER_ADDR`にloopback以外を許可する（意図的な公開用） |
| `PITHA_SERVER_ALLOWED_HOSTS` | `cmd/server` | `PITHA_SERVER_ALLOW_NON_LOOPBACK=1`のときのみ有効。Hostヘッダとして受け付ける追加ホスト名（カンマ区切り、DNS rebinding対策のHost検証。loopback名（`localhost`/`127.0.0.1`/`::1`）とワイルドカード以外の`PITHA_SERVER_ADDR`のホストは常に許可） |
| `SWAGGER_ENABLED` | `internal/router` | `true`のときのみ`/swagger`を有効化。未設定・それ以外は無効＝オプトイン（後述「Swagger / OpenAPI」） |
| `PITHA_DB_PATH` | `internal/bootstrap` | SQLite DBファイルのパス。未設定（または空）は`os.UserConfigDir()`配下の`pitha-trador/pitha.db`（Windowsは`%AppData%\pitha-trador\pitha.db`）。`secrets`テーブルを含むため、DBファイルは`0600`で作成/絞り込み（`-wal`/`-shm`も同モード）、新規作成する親ディレクトリ・`logs/`・インスタンスロックのディレクトリは`0700`、ログ/ロックファイルは`0600`（非Windows。既存の親ディレクトリのモードは変更しない） |
| `PITHA_UNIVERSE_PATH` | `internal/bootstrap` | 銘柄マスタCSVの場所（「銘柄マスタの投入」節）。優先順位は本環境変数 > 実行ファイルと同じディレクトリの`config/universe.csv`。どちらも無ければCSV同期をスキップする |
| `PITHA_STRATEGY_PATH` / `PITHA_RISK_PATH` | `internal/bootstrap` | `config/strategy.yaml`・`config/risk.yaml`の場所。優先順位は明示指定 > 本環境変数 > 実行ファイルと同じディレクトリの`config/*.yaml` > 埋め込み既定値（`architecture/overview.md` §9） |
| `PITHA_STATIC_DIR` | `internal/router` | 設定すると`/static/...`を`go:embed`ではなく指定ディレクトリ（存在するディレクトリのみ有効。`make dev`は`static/src`）から配信する。未設定・不正パスは埋め込みにフォールバック |

### Settings画面で設定する運用項目

プロセス起動・配布・開発インフラ以外の運用ノブは環境変数ではなくSettings画面（`/settings`の「運用設定」）から編集する（issue #708）。値は`runtime_settings`テーブル（`architecture/er/tables-system.md`）に保存され、項目ごとに「保存」と「既定に戻す」（保存値の削除）ができる。保存値はAPIキーと違い画面に表示される。

| 項目（Settings） | `runtime_settings`キー | 未設定時 | 反映 |
|---|---|---|---|
| バックアップ先ディレクトリ | `system.backup_dir` | 日次バックアップ無効（起動ログに警告。`/settings`で設定すれば解消） | **再起動不要**。保存後、Schedulerの次回メンテナンスチェック（10分以内）から動く。未設定の間はスキップされ失敗扱いにならず、設定後にその日のバックアップが実行される |
| ログディレクトリ | `system.log_dir` | DBファイルの親ディレクトリ配下の`logs/` | **再起動が必要**（ログは起動時に開くため。DBは起動前に読み取り専用で参照する）。不正値（相対パス等）は既定にフォールバック |
| Policy Engine（ロング/ショート）のしきい値 | `policy.{long,short}.*` | `config/strategy.yaml`の`policy.*` | **再起動不要**（評価のたびに再読込）。自己改善ループ（Sol/Opus）も同じキーを更新するため、後から書き込んだ方が有効 |
| Fast Screenerのフィルター・重み | `screener.*` | `config/strategy.yaml`の`fast_screener.*` | **再起動不要**（次の候補更新から） |
| 使用するブローカー | `broker.provider`（`kabu`\|`tachibana`） | `kabu`（従来どおり） | **再起動が必要**（起動時に1回読む）。自動フェイルオーバーはなく、切替は手動＋再起動 |
| 立花証券 e支店 接続設定 | `broker.tachibana.environment`（`demo`\|`production`）・`broker.tachibana.{demo,production}.base_url`・`broker.tachibana.{demo,production}.private_key_path`・`broker.tachibana.request_max_per_second`（1〜10）・`broker.tachibana.reauth_time`（05:30〜08:00） | デモ環境・`https://demo-kabuka.e-shiten.jp/e_api_v4r10/`（本番は`https://kabuka.e-shiten.jp/e_api_v4r10/`）・毎秒1（日中の負荷を抑える既定。設計上限は10）・05:35。秘密鍵パスは未設定（=立花は使えない） | **再起動が必要**。認証ID（`TACHIBANA_DEMO_AUTH_ID`/`TACHIBANA_PROD_AUTH_ID`）と任意のデモ第二暗証番号（`TACHIBANA_DEMO_SECOND_PASSWORD`）は「接続先」の「立花証券 e支店」で入力する（`secrets`、再起動で反映） |
| 立花の監視銘柄ソース | `broker.tachibana.candidate_source`（`daily_screen`\|`fixed`）・`broker.tachibana.{manual_symbols,fixed_symbols}`（銘柄コードのカンマ区切り、最大120件）・`broker.tachibana.nightly.{run_time,max_per_second,markets,min_price_jpy,exclude_symbols}`（夜間日足バッチ）・`broker.tachibana.screen.<指標>.{weight,top_n}`（`gain_rate`/`loss_rate`/`volume`/`turnover`/`volume_surge`/`turnover_surge`/`range_rate`）・`broker.tachibana.event.max_connects_per_day`・`broker.tachibana.rest_quote.{min_interval_seconds,requests_per_round}` | `daily_screen`・手動/固定リストは空・夜間バッチは18:00以降（翌01:00以降も可。08:00〜15:30は保存時に拒否）・0.1〜3件/秒（既定1）・市場区分`prime,standard,growth`・株価下限0（除外なし）・除外銘柄なし・各指標は重み1/上位10件・EVENT接続・切断は1日10回（1〜20）・REST時価補助は60秒に1要求以下（間隔10〜3600秒・1回1要求、1〜3） | **再起動不要**（次の夜間バッチ・次の監視リスト確定・次回のEVENT接続から。環境変数は使わない）。値は保存時に検証され、不正値は400で保存しない。重み>0かつ上位件数>=1の指標が1つも残らない保存も拒否する。kabu選択時の監視（FR-SCHED-9）には影響しない |

- 優先順位は`config/strategy.yaml` < `runtime_settings`（Settings画面）。以前の環境変数による上書き（`PITHA_POLICY_LONG_*`/`PITHA_POLICY_SHORT_*`/`PITHA_FAST_SCREENER_*`/`PITHA_LOG_DIR`/`PITHA_BACKUP_DIR`）は廃止した（設定されていても無視される）。
- バックアップ先はローカルディスク外（外部ドライブ・クラウド同期フォルダ等）の**既存ディレクトリの絶対パス**を指定する。ディレクトリ自体は作成しない。存在しない間（未マウントなど）はローカルへ退避せずバックアップが失敗し、連続失敗時にSlack/ログで通知される（Settingsの該当項目にも警告を表示する）。
- バックアップの内容: Schedulerの日次ジョブ（起動直後・10分ごとの未実行検出と毎日16:00）が`PRAGMA wal_checkpoint(TRUNCATE)`後の整合コピーを`daily/pitha-YYYY-MM-DD.db`へ保存し（`secrets`テーブルは空にし、`0700`/`0600`で作成）、90日超の日次分は削除、各ISO週の最初のバックアップ分を`weekly/pitha-YYYY-MM-DD.db.gz`（日付はその週の月曜）として52週保持する。復元はアプリ停止後にバックアップファイルを`PITHA_DB_PATH`（既定パス）へ置き換え、Setup画面でAPIキー・パスワードを再入力する。
- ログディレクトリは日次JSONログ（全ログ`<日付>.log`とERRORのみの`<日付>-error.log`）・`.gz`アーカイブ・エラーログのダウンロードが共有する。作成できない場合は標準エラー出力へフォールバックして起動を継続する。起動失敗などの致命エラーはERRORレベルで記録する。
- 立花証券 e支店APIを使う場合は、事前準備（標準Webでのパスキー登録 → API利用設定「利用する」→ 認証ID取得 → 鍵作成・公開鍵登録。デモ/本番で別々に行う）を上記「立花証券（e支店API）の事前準備」で済ませ、取得した**認証ID**を「接続先」の「立花証券 e支店」へ、**秘密鍵ファイルの絶対パス**を運用設定の「立花証券 e支店（接続設定）」へ入力する。秘密鍵（RSA 2048/4096のPEM、例: `openssl genrsa -out tachibana-demo.pem 2048`）はDB・ログ・画面に保存・表示せず、パスのみ保存する。保存時にPEM・RSA 2048/4096・秘密鍵であること（公開鍵・証明書は400）を検証し、OneDrive/Dropbox/Googleドライブ等の同期フォルダ配下は警告する。ファイルは同期されないフォルダに置き、OS権限（`chmod 600`等）でユーザーのみ読み取り可にする。「PEM本文を`secrets`に保存」は、暗号鍵がアプリ埋め込みシード由来で実質的な保護にならないため採らない。
- 立花の「固定IP登録」（利用設定画面の任意設定）はアプリでは扱わない。動的IP回線（家庭用回線など）では**登録しない**こと。登録すると回線のIPが変わったときに立花から締め出され、再登録するまで接続できない。本番の第二暗証番号はアプリに保存せず（保存先はない。issue #55で扱う）、デモの第二暗証番号はデモ発注スモーク（任意）専用で本番環境では使わない。
- 値は保存時に検証される（確率系しきい値は`(0, 1]`、`min_entry_quality`は`poor`/`fair`/`good`/`strong`/`exceptional`、`top_n >= 1`・価格/しきい値は正・`max_price >= min_price`・重みは0以上で合計が正、パスは絶対パス）。不正値は400で拒否し保存しない（FR-POLICY-2a / FR-FS-4）。

### 銘柄マスタの投入

スキャン対象ユニバース（`instruments`テーブル）は、DBが空のままだと全体スキャン・PUSH購読・候補更新・Jev Scout/Trader・Paper発注のすべてが何もしない。kabuステーションAPIには上場銘柄一覧を取得するエンドポイントが無いため、運用者が銘柄マスタCSV（東証上場銘柄一覧＝JPX公開のExcelをCSV化したもの等）を用意し、アプリ起動時に`internal/bootstrap`が取り込む。

1. 下記形式のCSV（UTF-8、BOM可）を作成し、`PITHA_UNIVERSE_PATH`で指すか、実行ファイルと同じディレクトリの`config/universe.csv`に置く。開発時のサンプルは`config/universe.sample.csv`（`make dev`は`PITHA_UNIVERSE_PATH`でこれを指す）。
2. アプリを（再）起動する。起動ログに`bootstrap: universe synced`（`instruments`/`changed`件数）が出れば投入済み。

```csv
symbol,name,market,sector,kind
7203,トヨタ自動車,TSE Prime,輸送用機器,stock
101,TOPIX,INDEX,,market_index
1050,輸送用機器,INDEX,輸送用機器,sector_index
```

- 列は見出し行で識別する（順序自由・大文字小文字無視）。必須は`symbol`（英数字`[0-9A-Za-z]`のみ・≤10文字・ファイル内で一意。`7203.T`・`^N225`・全角文字などは行番号つきのエラーでファイル全体を拒否する。銘柄詳細画面/APIが`^[0-9A-Za-z]+$`のsymbolしか受け付けないため。`130A`のような英数字混在コードと`101`（TOPIX）は可）/`name`/`market`。`sector`は任意（`kind=sector_index`では必須。株式の`sector`と一致するとsector_return_5m算出に使われる）。`kind`は`stock`（既定）/`market_index`/`sector_index`。`market_index`/`sector_index`も`stock`と同じ`market-data`ジョブでREST板を取得・スナップショット保存され（FR-SCHED-2）、市場コンテキスト特徴量（FR-FE-4）の入力になるが、取得されるのは`config/strategy.yaml`の`scan.full_scan_enabled: true`のときの60秒フルスキャンだけである。既定（オフ。FR-SCHED-7）ではランキング監視（FR-SCHED-9）が決めた`stock`の監視銘柄に加え、有効な`market_index`全件と監視銘柄の`sector`に一致する`sector_index`にも毎サイクル`market-data`が走るため、`market_return_*`・`sector_return_5m`は算出される（指数行がCSVに無い・更新が3分以上止まると欠損になる）。`market_breadth`は監視銘柄（最大45）だけの集計になる。PUSH購読と候補更新（Fast Screener→Jev Scout）の対象は`stock`のみ。
- 同期は冪等。新規`symbol`は`is_active=1`で追加し、既存`symbol`は`name`/`market`/`sector`/`kind`のみ更新して`is_active`は変更しない（運用者が除外した銘柄は再起動しても復活しない）。CSVから消した銘柄は削除も無効化もされない。
- 不正な行が1つでもあればファイル全体を適用せず、行番号つきのエラーをログに出してDBは変更しない。CSVが無くDBも空の場合はスキャン対象が0件になる旨をエラーログに出す。
- CSVはUTF-8（BOM可）のみ対応。UTF-8として不正なバイト列を含むファイル（Excelの既定「CSV」保存形式であるShift_JIS/CP932等）は、`line N, byte M: file is not valid UTF-8`のエラーでファイル全体を拒否する（文字化けした`name`/`sector`を保存しない）。Shift_JISのCSVはUTF-8で保存し直す（Excelでは「CSV UTF-8（コンマ区切り）」を選ぶ）。
- **CSVが無い場合の画面案内とJPXからの自動取得**（issue #508）: 有効な`stock`が1件も無い間、Scanner Dashboardのスキャン状況パネルは「まだスキャンサイクルが実行されていません」の代わりに「銘柄マスタが未投入です」の案内（`data-testid="scan-universe-empty"`。CSVの置き場所`PITHA_UNIVERSE_PATH`／`config/universe.csv`を併記）と、確認付きの取得ボタン「JPXから取得して投入する」（`scan-universe-import`）を表示する。押すまでJPXへは接続せず、起動時・定期の自動取得もしない。押下（`POST /scanner/universe/import`）で東証上場銘柄一覧（`https://www.jpx.co.jp/markets/statistics-equities/misc/tvdivq0000001vg2-att/data_j.xlsx`、16 MiB超は中止、全体で60秒）を1回だけ取得し、株式（`プライム/スタンダード/グロース`の内国・外国。`market`は`TSE Prime`/`TSE Standard`/`TSE Growth`、`sector`は33業種区分、`-`は未設定）だけをCSVと同じ検証・同じupsert（1トランザクション）で投入する。ETF・ETN／REIT等／PRO Market／出資証券は対象外で、指数（`market_index`/`sector_index`）は一覧に無いためCSVで追加する。未知の区分・不正な`symbol`・重複コード・1,000件未満・列の欠落は全体を拒否し、ネットワーク失敗やHTTPエラーと合わせてDBを変更せず、失敗の種別ごとの固定文言（接続できない／JPX側の形式・URL変更／マスタへの保存失敗）をパネルに表示してCSV投入へ誘導する。下位エラー（URL・DNS・SQLite・`ParseJPX`の英語メッセージ等）は画面に出さず`slog`にだけ記録する（issue #700）。成功すると再起動なしで次のスキャンサイクルから対象になる（PUSH購読の登録は次回の（再）接続時で、それまではREST取得）。有効な`stock`が既にあるときは取得を提供せず、`POST`は409（CSVで投入済みのマスタは上書きしない）。JPXのデータの権利はJPXに帰属し、取得・利用は[JPXの利用上の注意](https://www.jpx.co.jp/term-of-use/)（高頻度・高負荷な自動取得の自粛を含む）に従って運用者が責任を負う（パネルにも同旨とリンクを表示する）。

### Makefileターゲット

`make`（引数なし）または`make help`でコマンド一覧を表示する（既定ターゲットは`help`）。

| ターゲット | 内容 |
|---|---|
| `make dev` | `wails dev`・`templ generate --watch`・`bun --cwd=static run dev`を並行起動 |
| `make generate` | `templ generate`と`bun run --cwd static build`。`lint`/`test`/`build`の前提 |
| `make lint` | `generate`後に`golangci-lint run`と`bun run --cwd static lint`（`static/`で`biome check .`を実行。ルートで`bunx biome`を実行すると`@biomejs/biome`ではなく無関係なnpmパッケージ`biome`を解決して何も検査しないため、`static/`から実行する）。続けて`GOOS=windows go vet ./...`（Windows専用ファイルの検査）・`linterly check --no-update-check`・`bun run --cwd static typecheck`（`tsc --noEmit`）を実行し、CIの`lint`ジョブ（`govulncheck`を除く）と同じ検査をローカルで再現する |
| `make test` | `generate`後に`go test ./...`と`bun --cwd=static test` |
| `make test-race` | `generate`後に`go test -race ./...`（CIの`test`ジョブと同じ検証。race detectorはcgoを必要とするためgccが必要で、`CGO_ENABLED=0`の環境では使えない） |
| `make build` | `generate`後に`wails build -platform windows/amd64` |
| `make openapi-export` | 起動中サーバー（`127.0.0.1:48080`）から`docs/api/openapi.json`を書き出す（任意タスク。ファイルは未コミット） |

- **`make dev`の環境変数**: `wails dev`は`cmd/desktop`をカレントとして動くため、`Makefile`は`PITHA_UNIVERSE_PATH`も`config/universe.sample.csv`に設定し（銘柄マスタの投入、上記）、`PITHA_STRATEGY_PATH`/`PITHA_RISK_PATH`を`$(CURDIR)/config/*.yaml`（絶対パス）に設定する。`internal/bootstrap.Run`は環境変数を埋め込み既定値より優先するため、`config/risk.yaml`等を編集して`make dev`を再起動すれば再ビルドなしで反映される（埋め込み既定値はビルド時のスナップショット）。`PITHA_STATIC_DIR`は`static/src`に設定し、`/static/...`をディスクから配信する。`wails dev`のファイル監視は既定で`.go`変更時のみGoバイナリを再ビルドするため、この上書きが無いと`bun run dev`（esbuild/Tailwind watch）の出力がgo:embedのスナップショットに阻まれ`make dev`再起動まで反映されない
- **`generate`が前提となる理由**: `templ generate`が`*_templ.go`を、`bun run --cwd static build`が`static/src/dist/{css,js}`を生成する。どちらも`.gitignore`対象であり、`static/src/embed.go`の`//go:embed dist img vendor`は`dist/`が空だとコンパイル自体が失敗する。そのためクリーンなチェックアウトでは、生成前に`go vet`/golangci-lint/`go test`/`wails build`のいずれも実行できない（古い生成物が残っていると陳腐化した出力に対して実行してしまう）。CIの`lint`/`test`/`build`各ジョブも同じ2ステップを先に実行し、`lefthook`のpre-commit/pre-pushも`make generate`を呼ぶ
- **CSPの`lightweight-charts` style hash**: `lightweight-charts`は帰属ロゴ用のインライン`<style>`を挿入し、CSPは`internal/web/middleware/security_headers.go`の`lightweightChartsAttributionStyleHash`（SHA-256）でこれだけを許可している。`bun test`の`static/src/csp/lightweight-charts-style-hash.test.ts`がインストール済みライブラリの実際のstyleテキストのハッシュをこの定数と照合するため、`lightweight-charts`の更新でstyleが変わるとテストが失敗する。失敗したら期待値として表示されたハッシュで定数を更新する。
- **`-race`の適用範囲**: データ競合の検出はCIの`test`ジョブ（`go test -race ./...`）と`make test-race`で行う。race detectorはcgo（gcc）を必要とし、Windows開発機などgccが無い環境ではビルドできないため、`make test`とlefthookのpre-pushは`-race`なしの`go test ./...`のままにしている（pre-pushを高速に保つ目的も兼ねる）

## CI/CD

GitHub Actions（`.github/workflows/ci.yml`）。ジョブ構成（`lint` → `test`・`test-windows`（並列）→ `build` → `release`）・トリガー・バージョン固定・最小権限・外部ActionのSHA固定・複合Action・脆弱性スキャン・リリース署名（issue #376）などの詳細は`environment/ci.md`を参照する。

## Lint

| 対象 | ツール | 設定ファイル |
|---|---|---|
| Go | golangci-lint | `.golangci.yml` |
| フロントエンド（Lit/TypeScript） | Biome | `static/biome.json` |

- `.golangci.yml`は`default: none`とし、`govet`・`staticcheck`・`errcheck`・`ineffassign`・`unused`・`depguard`・`gosec`・`bodyclose`・`noctx`・`rowserrcheck`のみを有効化する（`gofmt`はlinterではなく`formatters:`で有効化）。`unused`は`staticcheck`のU1000と同じ未使用コード検出で、テストコード内の未使用ヘルパーも対象にする（#671）
- `gosec`・`noctx`・`bodyclose`は`*_test.go`を対象外とする（`t.TempDir()`配下のパーミッション、テスト用`httptest.NewRequest`・WebSocketハンドシェイクは攻撃面ではないため）。`gosec`の`G304`（変数パスのファイルオープン）は、パスがすべてアプリ自身の設定/データディレクトリやサーバー生成名から作られリクエスト由来ではないため設定で除外する（パーミッション系`G301/G302/G306`は有効のまま）。`internal/config/secret*.go`の`G101`（シークレット行キー名への誤検知）は`exclusions`で除外する。上記以外の指摘は修正するか、理由付きの`//nolint:<linter> // <理由>`で個別に抑止する
- `depguard`は8ルールでレイヤー規約（`architecture/overview.md` §3、`components/overview.md` §3）を強制する。`web-no-repository`は`internal/web/**`から`internal/repository`**およびその全サブパッケージ**（`pkg`はプレフィックス一致）へのimportを拒否し、`web-no-bootstrap`は`internal/web/**`から`internal/bootstrap`**およびその全サブパッケージ**へのimportを拒否し（`bootstrap` → `web`の一方向のみ。#702）、`templ-no-service`は`internal/web/{atoms,molecules,organisms,pages,layout}/**`（Templ層）から`internal/service/**`へのimportを拒否する（#380）。Atomic Designの依存方向（atoms → molecules → organisms → layout → pages の下向きのみ）は`atoms-direction`・`molecules-direction`・`organisms-direction`・`layout-direction`が上位層のimportを拒否して強制し、`templ-parts-no-middleware`が`atoms`/`molecules`/`organisms`/`pages`から`internal/web/middleware`へのimportを拒否する（`web/layout`と`web/handler`のみ利用可。#521）。上記以外のimport規約（`service` → `web`の禁止、`bootstrap`の子 → 親の禁止、兄弟サブパッケージ間など）はレビューで担保する。`repository`のサブパッケージ分割（#244）でルールの書き換えは不要
- Biomeはlintとformatを1ツールで兼ねるため、`static/`配下は追加のESLint/Prettier設定を持たない

## Format

| 対象 | ツール | コマンド |
|---|---|---|
| Go | gofmt（標準、`.golangci.yml`の`formatters:`で有効化されCI検証も兼ねる） | `go fmt ./...` |
| フロントエンド | Biome | `bun run --cwd static format`（`static/`で`biome format --write src`を実行。ルートで`bunx biome`は実行しない） |

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

`.linterlyignore`の方針（`architecture/overview.md` §3「サブパッケージ単位の責務規約」）:

```text
# logs/（.gitignoreの `logs/` パターンでリポジトリルート・cmd/desktop/両方が
# 対象）はアプリ実行時に生成されるランタイムログで、ソースコードではない。
# linterly は .gitignore を参照しないため明示的に除外する（issue-sweep実行中、
# ローカルでの動作確認で溜まった数十万行のログが pre-commit の golangci-lint /
# linterly をブロックした事例より）。
**/logs/**

# 自動生成コード（Templが生成するGoコード。手書きソースコードの除外は基本追加しない）
*_templ.go

# wails dev/build 生成の JS バインディング・runtime 型定義（.gitignore 済みの生成物）
static/wailsjs/**

# ライセンス全文（法的な定型文でありソースコードではない。分割・短縮できない）
LICENSE
```

- 許容する除外は上記の`*_templ.go`・`**/logs/**`・`static/wailsjs/**`（`wails dev`が生成する`.gitignore`済みのバインディング・`runtime.d.ts`）・ライセンス全文`LICENSE`（手書きソースではない定型文）のみ。**手書きソース（テスト含む）の除外は置かない**。ディレクトリ2000行・ファイル300行の上限は、責務別サブパッケージへの分割（`architecture/overview.md` §3）で満たす
- サブパッケージ分割前の暫定除外（`internal/repository/`は#244、`internal/web/handler/`は#245、`internal/bootstrap/`は#246、`internal/service/risk/`は#247）は全て削除済みで、#248で全廃を確認した。手書きソースの除外を新規に追加してはならない（必要になった時点でサブパッケージ分割を先に行う）

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
      glob: "**/*.{ts,css,json,mjs}"
      run: bunx biome check {staged_files}
    typecheck:
      root: static/
      glob: "**/*.ts"
      run: bun run typecheck
    linterly:
      run: linterly check

pre-push:
  commands:
    go-test:
      run: make generate && go test ./...
```

`biome`は`root: static/`で`static/`をカレントにして実行する（`@biomejs/biome`は`static/package.json`のdevDependencyであり、リポジトリルートの`bunx biome`は無関係なnpmパッケージ`biome`を解決してしまうため）。`root`指定時、`{staged_files}`は`static/`配下のステージ済みファイルのみが`static/`相対パスで渡され、`glob`もその相対パスに対して評価される。

`typecheck`はCIの`lint`ジョブの`tsc --noEmit`と同じ検査で、`biome`では検出できない型エラー（#197/#198でCIのみ失敗した経緯）をコミット時に検出する。`tsc`はプロジェクト全体を検査するため`{staged_files}`は渡さない（`static/node_modules`が必要で、`bun install --cwd static`で導入する）。`golangci-lint`と`go-test`の前に`make generate`を実行するのは、`*_templ.go`と`static/src/dist/`が未生成だと`go:embed`でコンパイルできない（または古い生成物に対して実行してしまう）ため。`linterly check`は`{staged_files}`を渡さずリポジトリ全体を検査する（ディレクトリ単位の行数上限のため）。

## Swagger / OpenAPI

APIサーバー（Huma）を含むプロジェクトのため対象。`docs/api/endpoints.md`のAPIルート（`/api/v1/...`）に対応する。

- **Spec生成ツール**: Huma組み込みの自動生成（`swag`等のアノテーション方式は不要）。Go構造体のリフレクションからリクエスト起動時に都度OpenAPI 3.1スペックを生成するため、実装とspecがずれることが構造的にない
- **エンドポイント**: `/api/v1/openapi.json`（Huma生成のspec本体）
- **UI**: Stoplight Elements（`@stoplight/elements`、bunで導入）。`static/package.json`の依存として`bun.lock`でバージョンを固定し、`static/esbuild.config.mjs`が`web-components.min.js`/`styles.min.css`を`static/src/dist/vendor/stoplight-elements/`へコピーして`go:embed`でバイナリに同梱、`/static/dist/vendor/stoplight-elements/`から同一オリジン配信する（CDN読み込みはしない。取引操作APIを持つオリジンでサードパーティスクリプトを実行しないため）。`/swagger`固定エンドポイントで、`<elements-api apiDescriptionUrl="/api/v1/openapi.json">` を埋め込んだ静的HTML 1枚を返す。Swagger UI用の追加ミドルウェアは不要
- **環境変数**: `SWAGGER_ENABLED`（`true`のときのみ`/swagger`ルートを有効化。未設定・`true`以外は無効＝オプトイン）。`/api/v1/openapi.json`は本変数に関わらず常に公開される。`make dev`は`true`を設定し、本番（Phase 7実売買時）を含むそれ以外は未設定（無効）とする
- **CI連携**: Huma生成spec方式のため「生成し忘れによるdrift」が構造的に発生しない。よってswag方式で一般的な`swag init && git diff --exit-code`のようなdrift検知CIステップは不要。外部ツール（Postman等）向けにspecファイルをエクスポートしたい場合のみ、任意タスクとして`make openapi-export`（`docs/api/openapi.json`へ書き出し）を用意する

## 改訂履歴

改訂履歴は`docs/environment/setup/history.md`に分割している（1.0〜）。
