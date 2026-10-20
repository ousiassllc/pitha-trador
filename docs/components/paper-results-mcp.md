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
| Settings 画面側の設定サービス（状態表示・有効/待受アドレスの保存・API キー再生成・起動/停止/再起動・アプリ起動時の自動起動・アプリ終了時の停止） | `internal/service/paperresultsmcp/admin` |
| MCP 子プロセスの起動・停止・状態（二重起動の防止、環境変数の許可リスト、孤児の防止＝Windows の Job Object、終了理由の分類）。issue #795、設計は `architecture/paper-results-mcp-lifecycle.md` | `internal/service/paperresultsmcp/proc` |
| アプリ本体への配線（`mcpwire.New` でサービスを作り、`mcpwire.Start` で自動起動、`mcpwire.Stop` で停止、`mcpwire.RouterOptions` で Settings のカードを配線。`cmd/desktop` の `App.startup`/`App.shutdown` と `cmd/server` の起動・終了経路が呼ぶ。`internal/bootstrap` 本体には置かない） | `internal/bootstrap/mcpwire`、`cmd/desktop`、`cmd/server` |
| Settings 画面の MCP パネルのハンドラと画面部品（`GET /settings/mcp-panel`、`POST /settings/mcp`、`POST /settings/mcp/key`、`POST /settings/mcp/{start,stop,restart}`） | `internal/web/handler/mcpsettings`、`internal/web/organisms/mcp`、`static/src/components/copy-button` |
| DB を `mode=ro` + `PRAGMA query_only=ON` で開く（`sqlitedb.Open` は呼ばない） | `internal/service/paperresultsmcp/readdb` |
| SQL とホワイトリストの戻り値（列を名指しで SELECT。`SELECT *` なし） | `internal/service/paperresultsmcp/query` |
| テスト（外から見た HTTP の振る舞い）と、その固定データ・SQL 数えるドライバ | `internal/service/paperresultsmcp/mcptests`、`internal/service/paperresultsmcp/testkit` |
| 東証立会日 | 既存の `internal/service/marketcalendar` |
| 資金の分母 | `cmd/paper-results-mcp` の `paperInitialCapital`（アプリと同じ risk yaml 解決順。Paper の `initial_capital` のみ。Settings の保存値（`runtime_settings`）には追従しない: 読み取り専用 DB の許可リストに無いため）。DB は `bootstrap.DefaultDBPath` と `PITHA_DB_PATH` |

`internal/web` からは import しない（depguard の `web-no-repository` を迂回する読み取り画面を増やさない）。

## 3. テスト

受け入れ条件は `docs/api/endpoints/paper-results-mcp.md` §5。単体テストは一時ディレクトリの SQLite（マイグレーション適用済みのテスト DB）と、httptest の HTTP で行う。認証失敗が SQL 0 件・接続 0 本であることは、`database/sql` のドライバを包んで文を数えるテストで確認する。Tailscale も Cloudflare も、トンネルのアカウントは CI に置かない。

## 4. ビルド・設定・起動

ビルド: `go build -o paper-results-mcp ./cmd/paper-results-mcp`（Windows では `paper-results-mcp.exe`）。アプリ本体の実行ファイルには含まれない。Windows のインストーラはアプリ本体の実行ファイルと同じフォルダに `paper-results-mcp.exe` を置く。Settings 画面の MCP パネルは、アプリ本体の実行ファイルと同じフォルダの `paper-results-mcp(.exe)` を探し、「見つかる / 見つからない」を表示する。

### 4.1 設定（Settings 画面）

既定はオフで、何も待ち受けない。オン/オフ・待受アドレス・API キーは Settings 画面の「外部連携」→「MCP（Paper 実績・Jev 判断の読み取り専用エンドポイント）」（`/settings#modal-mcp`）で設定する。手で JSON を作る必要はない（issue #792）。画面は設定ファイルの書き込みに加え、MCP プロセスの起動・停止・再起動を行う（issue #795。§4.2。MCP は別プロセスのまま、アプリは子プロセスとして起動するだけ）。

- 画面の項目: 状態（待受中 / 起動中 / 停止中 / エラー / キー未設定）と「次回起動時も自動で起動する」表記、起動・停止・再起動のボタン、有効化のチェック、待受アドレス（`host:port`）、API キーの生成・再生成、Grok Bot に登録する値（URL と、伏せ字の `Authorization: Bearer ●●●●●●●●`）、`paper-results-mcp` の配置先（見つかる / 見つからない）、設定ファイルの場所
- 待受アドレスの検証はサーバー側で行い、プロセスと同じ規則である。許すのは loopback と、この PC の tailnet アドレス（`100.64.0.0/10`、`fd7a:115c:a1e0::/48`）で、そのアドレスが実際にインターフェースにあるときだけ。`0.0.0.0`・`::`・ホスト部が空・LAN・ホスト名・ポート 0 は 422 で拒否し、ファイルは変えない。空欄は `127.0.0.1:48110`
- API キーは `pitha-mcp-` + 32 バイトの乱数（URL 安全な base64）。生成・再生成の直後の応答に一度だけ表示し（読み取り専用の欄、`Authorization: Bearer <キー>` の全文、コピーボタン）、応答は `Cache-Control: no-store`、表示部は htmx の履歴キャッシュに入れない（`hx-history="false"`）。パネルを開き直すと常に伏せ字になる。紛失したら再生成する。再生成すると古いキーは、MCP を再起動した時点で使えなくなる（画面の「再起動」で行う）
- 状態は設定ファイル、アプリが起動した子プロセスの有無、有効かつキーがあるときだけの TCP 接続（300 ms）から決める。ファイルが無ければオフ（停止中・接続しない）。有効でもキーが空なら「キー未設定」（接続しない）。接続できれば「待受中」。アプリが起動して 10 秒以内でまだ接続できなければ「起動中」、10 秒を過ぎても接続できない・起動に失敗した・操作者の停止でなく終了した（異常終了）ときは「エラー」（理由は固定文。終了コードだけを添え、プロセスの出力は出さない）。何も起動しておらず接続もできなければ「停止中」。アプリの外で起動された MCP が待ち受けているときは「待受中」（アプリの外で起動された旨を併記。画面からは停止しない・二重には起動しない）。設定ファイルが読めない・権限が緩い・不正なときは「停止中」と警告を出す。そのまま保存または生成すると、既定値から `0600` で作り直す（既存のキーは失われる）

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

### 4.2 起動・停止・再起動（issue #795）

MCP は別プロセスのまま、アプリが子プロセスとして起動する。設計と安全条件との両立は `architecture/paper-results-mcp-lifecycle.md`。運用者が手で起動する必要はない。`paper-results-mcp` を単体で起動してもよいが、そのときは「アプリの外で起動された MCP」として「待受中」と表示し、アプリは二重に起動せず、画面からも停止しない（止めるには起動した側で止める）。

**操作**: Settings 画面の MCP パネルの「起動」「停止」「再起動」ボタン（`POST /settings/mcp/start`・`stop`・`restart`。CSRF ヘッダ必須、Setup 完了後のみ）。起動できるのは、保存された設定が有効・API キーあり・待受アドレスが許可範囲のときだけ（プロセスが自分で拒否する条件と同じ。満たさないときはプロセスを作らず、理由を 422 で返す）。待受アドレスが応答しているとき（アプリの外で起動された MCP を含む）と、アプリが起動した子がいるときは、起動しない（409）。「再起動」は停止してから保存済みの設定で起動し直す（設定の保存・API キーの再生成の反映はこれで行う）。

**アプリ起動時**: アプリの背景サービスの起動（`Services.Start`）が成功した直後（`cmd/desktop` の `OnStartup`、`cmd/server` の `Services.Start` の後）に、保存された設定が有効・API キーあり・待受アドレスが許可範囲のとき MCP も同時に起動する。無効（既定）・キー空・不正アドレス・設定ファイルが使えない場合は起動しない。画面には「次回起動時も自動で起動します」（起動しないときは理由）と出す。アプリがクラッシュして `--supervise` に再起動されたときは、前の MCP のポートが閉じるまで最大 5 秒待ってから起動する。待受アドレスが tailnet アドレスでまだ PC に割り当てられていない（ログオン直後で Tailscale が上がっていない）ときは、最大 90 秒待ってから判定し直す。それでも応答する（アプリの外で起動された）ときは起動しない。

**アプリ終了時**: アプリの終了（Wails の `OnShutdown` と `cmd/server` の終了経路の両方。`Services.Stop` より前）で MCP も止める（Unix 系は SIGTERM、5 秒で kill。Windows は終了させる）。終了を始めたあとの起動は拒否する。アプリが異常終了・kill された場合は、OS が MCP を止める: Windows は子を `CREATE_SUSPENDED` で作ってから Job Object（`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`）に入れて動かし（入る前には 1 命令も実行しない）、アプリのプロセスが消えるとハンドルが閉じて子が終了する。Linux は `Pdeathsig=SIGKILL`。Job Object に入れられなかったときは起動を失敗として子を止める。

**Windows の再起動後**: PC を再起動したあとは、インストーラーのスタートアップショートカット（`--supervise`、既定ON。`requirements/non-functional.md` §3）でアプリが立ち上がり、その起動と同時に MCP も起動する。MCP 用の自動起動（スタートアップ・タスクスケジューラへの登録）は作らない。ホスト再起動後の無人復帰は Windows の自動ログオンが前提である。

**子プロセスに渡すもの**: 引数は `-config <設定ファイルのパス>` だけ。API キーは起動引数にも環境変数にも載せず、子が `0600` の設定ファイルから読む。環境変数は許可リスト（`PATH`・`SYSTEMROOT`・`WINDIR`・`APPDATA`・`LOCALAPPDATA`・`USERPROFILE`・`HOME`・`XDG_CONFIG_HOME`・`TMP`・`TEMP`・`TMPDIR`・`TZ`・`PITHA_DB_PATH`・`PITHA_RISK_PATH`）だけで、アプリのそれ以外の環境変数は渡さない。標準入力・標準出力は null、標準エラーは終了理由の分類にだけ使い（ログにも画面にも出さない）、アプリの DB 接続・開いているファイルは渡さない。DB は子が自分で `mode=ro` + `query_only` で開く。実行ファイルはアプリ本体と同じフォルダの `paper-results-mcp(.exe)`。無いときは「エラー」（実行ファイルが見つからない）。

DB は `PITHA_DB_PATH`、無ければ `%AppData%\pitha-trador\pitha.db` を読み取り専用で開く。資金の分母は `PITHA_RISK_PATH` / `config/risk.yaml` の `paper.initial_capital`。手で止めるときは Ctrl+C（SIGINT/SIGTERM）で、処理中の呼び出しを終えてから閉じる。

Grok Bot には、選んだ経路の URL（Tailscale のホスト名、またはトンネルの HTTPS 名。画面の URL は待受アドレスそのもの）と `Authorization: Bearer <api_key>` を登録する（アプリの外の手作業）。実機での経路確認（tailnet の外から届かないことを含む）は自動テストの対象外で、キーを伏せた記録を残す（API 仕様 §5）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-17 | 新規作成 | issue #783。HALT の画面仕様とは別プロセスであると明記 |
| 1.1 | 2026-10-17 | 経路の実装をプロセスに埋め込まないことと、CI に Tailscale・Cloudflare のアカウントを置かないことを明記 | 悠太さん指示。Cloudflare を必須にしない |
| 1.2 | 2026-10-18 | 実装済みに改め、§1 に Streamable HTTP の自前ハンドラの選択、§2 に実際のパッケージ構成、§4 にビルド・設定ファイル・起動手順を追加 | issue #783 の実装 |
| 1.3 | 2026-10-19 | §2 に設定ファイルの書き込み・設定サービス・Settings 画面のハンドラを追加。§4 を、Settings 画面で有効/待受アドレス/API キーを設定する手順に書き換え（手作りの JSON は不要。キーの一度きりの表示と伏せ字、再起動が必要なこと、プロセスは画面から起動・停止しないことを明記） | issue #792 |
| 1.4 | 2026-10-19 | §2 に子プロセス管理（`proc`）とアプリ本体への配線を追加。§4.1 の状態に 起動中・エラーを足し、§4.2 を、画面からの起動・停止・再起動、アプリ起動時の自動起動、アプリ終了時の停止、異常終了時の孤児防止（Job Object）、`--supervise` 後の復帰、子プロセスに渡すもの（キーは設定ファイル経由のみ）に書き換え | issue #795 |
| 1.5 | 2026-10-19 | §4.2 に、tailnet アドレスの割り当て待ち（最大 90 秒）、Windows の `CREATE_SUSPENDED` + Job Object、アプリの外で起動された MCP の扱いを追記（レビュー指摘の反映） | issue #795 |
